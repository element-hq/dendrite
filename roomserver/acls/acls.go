// Copyright 2024 New Vector Ltd.
// Copyright 2020 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package acls

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/element-hq/dendrite/roomserver/storage/tables"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/sirupsen/logrus"
)

const MRoomServerACL = "m.room.server_acl"

type ServerACLDatabase interface {
	// RoomsWithACLs returns all room IDs for rooms with ACLs
	RoomsWithACLs(ctx context.Context) ([]string, error)

	// GetBulkStateACLs returns all server ACLs for the given rooms.
	GetBulkStateACLs(ctx context.Context, roomIDs []string) ([]tables.StrippedEvent, error)
}

type ServerACLs struct {
	acls               map[string]*serverACL      // room ID -> ACL
	aclsMutex          sync.RWMutex               // protects the above
	aclRegexCache      map[string]**regexp.Regexp // Cache from "serverName" -> pointer to a regex
	aclRegexCacheMutex sync.RWMutex               // protects the above
}

func NewServerACLs(db ServerACLDatabase) *ServerACLs {
	// Add some logging, as this can take a while on larger instances.
	logrus.Infof("Loading server ACLs...")
	start := time.Now()
	aclCount := 0
	defer func() {
		logrus.WithFields(logrus.Fields{
			"duration": time.Since(start),
			"acls":     aclCount,
		}).Info("Finished loading server ACLs")
	}()
	ctx := context.TODO()
	acls := &ServerACLs{
		acls: make(map[string]*serverACL),
		// Be generous when creating the cache, as in reality
		// there are hundreds of servers in an ACL.
		aclRegexCache: make(map[string]**regexp.Regexp, 100),
	}

	// Look up all rooms with ACLs.
	rooms, err := db.RoomsWithACLs(ctx)
	if err != nil {
		logrus.WithError(err).Fatalf("Failed to get known rooms")
	}

	// No rooms with ACLs, don't bother hitting the DB again.
	if len(rooms) == 0 {
		return acls
	}

	// Get ACLs for the required rooms, bail if we are unable to get them.
	events, err := db.GetBulkStateACLs(ctx, rooms)
	if err != nil {
		logrus.WithError(err).Fatal("Failed to get server ACLs for all rooms")
	}

	aclCount = len(events)

	for _, event := range events {
		acls.OnServerACLUpdate(event)
	}

	return acls
}

type ServerACL struct {
	Allowed         []string `json:"allow"`
	Denied          []string `json:"deny"`
	AllowIPLiterals bool     `json:"allow_ip_literals"`
}

type serverACL struct {
	ServerACL
	allowedRegexes []**regexp.Regexp
	deniedRegexes  []**regexp.Regexp
}

func compileACLRegex(orig string) (*regexp.Regexp, error) {
	escaped := regexp.QuoteMeta(orig)
	escaped = strings.Replace(escaped, "\\?", ".", -1)
	escaped = strings.Replace(escaped, "\\*", ".*", -1)
	return regexp.Compile(escaped)
}

// cachedCompileACLRegex is a wrapper around compileACLRegex with added caching
func (s *ServerACLs) cachedCompileACLRegex(orig string) (**regexp.Regexp, error) {
	s.aclRegexCacheMutex.RLock()
	re, ok := s.aclRegexCache[orig]
	if ok {
		s.aclRegexCacheMutex.RUnlock()
		return re, nil
	}
	s.aclRegexCacheMutex.RUnlock()
	compiled, err := compileACLRegex(orig)
	if err != nil {
		return nil, err
	}
	s.aclRegexCacheMutex.Lock()
	defer s.aclRegexCacheMutex.Unlock()
	s.aclRegexCache[orig] = &compiled
	return &compiled, nil
}

func (s *ServerACLs) OnServerACLUpdate(strippedEvent tables.StrippedEvent) {
	acls := &serverACL{}
	if err := json.Unmarshal([]byte(strippedEvent.ContentValue), &acls.ServerACL); err != nil {
		logrus.WithError(err).Errorf("Failed to unmarshal state content for server ACLs")
		return
	}
	// The spec calls only for * (zero or more chars) and ? (exactly one char)
	// to be supported as wildcard components, so we will escape all of the regex
	// special characters and then replace * and ? with their regex counterparts.
	// https://matrix.org/docs/spec/client_server/r0.6.1#m-room-server-acl
	for _, orig := range acls.Allowed {
		if expr, err := s.cachedCompileACLRegex(orig); err != nil {
			logrus.WithError(err).Errorf("Failed to compile allowed regex")
		} else {
			acls.allowedRegexes = append(acls.allowedRegexes, expr)
		}
	}
	for _, orig := range acls.Denied {
		if expr, err := s.cachedCompileACLRegex(orig); err != nil {
			logrus.WithError(err).Errorf("Failed to compile denied regex")
		} else {
			acls.deniedRegexes = append(acls.deniedRegexes, expr)
		}
	}
	logrus.WithFields(logrus.Fields{
		"allow_ip_literals": acls.AllowIPLiterals,
		"num_allowed":       len(acls.allowedRegexes),
		"num_denied":        len(acls.deniedRegexes),
	}).Debugf("Updating server ACLs for %q", strippedEvent.RoomID)

	// Clear out Denied and Allowed, now that we have the compiled regexes.
	// They are not needed anymore from this point on.
	acls.Denied = nil
	acls.Allowed = nil
	s.aclsMutex.Lock()
	defer s.aclsMutex.Unlock()
	s.acls[strippedEvent.RoomID] = acls
}

func (s *ServerACLs) IsServerBannedFromRoom(serverName spec.ServerName, roomID string) bool {
	s.aclsMutex.RLock()
	// First of all check if we have an ACL for this room. If we don't then
	// no servers are banned from the room.
	acls, ok := s.acls[roomID]
	if !ok {
		s.aclsMutex.RUnlock()
		return false
	}
	s.aclsMutex.RUnlock()
	// Split the host and port apart. This is because the spec calls on us to
	// validate the hostname only in cases where the port is also present.
	if serverNameOnly, _, err := net.SplitHostPort(string(serverName)); err == nil {
		serverName = spec.ServerName(serverNameOnly)
	}
	// Check if the hostname is an IPv4 or IPv6 literal. We cheat here by adding
	// a /0 prefix length just to trick ParseCIDR into working. If we find that
	// the server is an IP literal and we don't allow those then stop straight
	// away.
	if _, _, err := net.ParseCIDR(fmt.Sprintf("%s/0", serverName)); err == nil {
		if !acls.AllowIPLiterals {
			return true
		}
	}
	// Check if the hostname matches one of the denied regexes. If it does then
	// the server is banned from the room.
	for _, expr := range acls.deniedRegexes {
		if (*expr).MatchString(string(serverName)) {
			return true
		}
	}
	// Check if the hostname matches one of the allowed regexes. If it does then
	// the server is NOT banned from the room.
	for _, expr := range acls.allowedRegexes {
		if (*expr).MatchString(string(serverName)) {
			return false
		}
	}
	// If we've got to this point then we haven't matched any regexes or an IP
	// hostname if disallowed. The spec calls for default-deny here.
	return true
}
