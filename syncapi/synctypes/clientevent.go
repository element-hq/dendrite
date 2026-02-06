/* Copyright 2024 New Vector Ltd.
 * Copyright 2017 Vector Creations Ltd
 *
 * SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
 * Please see LICENSE files in the repository root for full details.
 */

package synctypes

import (
	"encoding/json"
	"fmt"

	"github.com/matrix-org/gomatrixserverlib"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// EventUnsignedFields contains field names found in the 'unsigned' data on events
const (
	// UnsignedFieldMembership is the user's membership state at the time of the event, per MSC4115
	// This is the stable field name (MSC4115 completed FCP June 2024)
	UnsignedFieldMembership = "membership"

	// UnsignedFieldMSC4115Membership is the unstable field name for MSC4115
	// Kept for backwards compatibility during transition period
	UnsignedFieldMSC4115Membership = "io.element.msc4115.membership"
)

// PrevEventRef represents a reference to a previous event in a state event upgrade
type PrevEventRef struct {
	PrevContent   json.RawMessage `json:"prev_content"`
	ReplacesState string          `json:"replaces_state"`
	PrevSenderID  string          `json:"prev_sender"`
}

type ClientEventFormat int

const (
	// FormatAll will include all client event keys
	FormatAll ClientEventFormat = iota
	// FormatSync will include only the event keys required by the /sync API. Notably, this
	// means the 'room_id' will be missing from the events.
	FormatSync
	// FormatSyncFederation will include all event keys normally included in federated events.
	// This allows clients to request federated formatted events via the /sync API.
	FormatSyncFederation
)

// ClientFederationFields extends a ClientEvent to contain the additional fields present in a
// federation event. Used when the client requests `event_format` of type `federation`.
type ClientFederationFields struct {
	Depth      int64        `json:"depth,omitempty"`
	PrevEvents []string     `json:"prev_events,omitempty"`
	AuthEvents []string     `json:"auth_events,omitempty"`
	Signatures spec.RawJSON `json:"signatures,omitempty"`
	Hashes     spec.RawJSON `json:"hashes,omitempty"`
}

// ClientEvent is an event which is fit for consumption by clients, in accordance with the specification.
type ClientEvent struct {
	Content        spec.RawJSON   `json:"content"`
	EventID        string         `json:"event_id,omitempty"`         // EventID is omitted on receipt events
	OriginServerTS spec.Timestamp `json:"origin_server_ts,omitempty"` // OriginServerTS is omitted on receipt events
	RoomID         string         `json:"room_id,omitempty"`          // RoomID is omitted on /sync responses
	Sender         string         `json:"sender,omitempty"`           // Sender is omitted on receipt events
	SenderKey      spec.SenderID  `json:"sender_key,omitempty"`       // The SenderKey for events in pseudo ID rooms
	StateKey       *string        `json:"state_key,omitempty"`
	Type           string         `json:"type"`
	Unsigned       spec.RawJSON   `json:"unsigned,omitempty"`
	Redacts        string         `json:"redacts,omitempty"`

	// Only sent to clients when `event_format` == `federation`.
	ClientFederationFields
}

// ToClientEvents converts server events to client events.
func ToClientEvents(serverEvs []gomatrixserverlib.PDU, format ClientEventFormat, userIDForSender spec.UserIDForSender) []ClientEvent {
	evs := make([]ClientEvent, 0, len(serverEvs))
	for _, se := range serverEvs {
		if se == nil {
			continue // TODO: shouldn't happen?
		}
		ev, err := ToClientEvent(se, format, userIDForSender)
		if err != nil {
			logrus.WithError(err).Warn("Failed converting event to ClientEvent")
			continue
		}
		evs = append(evs, *ev)
	}
	return evs
}

// ToClientEventDefault converts a single server event to a client event.
// It provides default logic for event.SenderID & event.StateKey -> userID conversions.
func ToClientEventDefault(userIDQuery spec.UserIDForSender, event gomatrixserverlib.PDU) ClientEvent {
	ev, err := ToClientEvent(event, FormatAll, userIDQuery)
	if err != nil {
		return ClientEvent{}
	}
	return *ev
}

// If provided state key is a user ID (state keys beginning with @ are reserved for this purpose)
// fetch it's associated sender ID and use that instead. Otherwise returns the same state key back.
//
// # This function either returns the state key that should be used, or an error
//
// TODO: handle failure cases better (e.g. no sender ID)
func FromClientStateKey(roomID spec.RoomID, stateKey string, senderIDQuery spec.SenderIDForUser) (*string, error) {
	if len(stateKey) >= 1 && stateKey[0] == '@' {
		parsedStateKey, err := spec.NewUserID(stateKey, true)
		if err != nil {
			// If invalid user ID, then there is no associated state event.
			return nil, fmt.Errorf("Provided state key begins with @ but is not a valid user ID: %w", err)
		}
		senderID, err := senderIDQuery(roomID, *parsedStateKey)
		if err != nil {
			return nil, fmt.Errorf("Failed to query sender ID: %w", err)
		}
		if senderID == nil {
			// If no sender ID, then there is no associated state event.
			return nil, fmt.Errorf("No associated sender ID found.")
		}
		newStateKey := string(*senderID)
		return &newStateKey, nil
	} else {
		return &stateKey, nil
	}
}

// ToClientEvent converts a single server event to a client event.
func ToClientEvent(se gomatrixserverlib.PDU, format ClientEventFormat, userIDForSender spec.UserIDForSender) (*ClientEvent, error) {
	ce := ClientEvent{
		Content:        se.Content(),
		Sender:         string(se.SenderID()),
		Type:           se.Type(),
		StateKey:       se.StateKey(),
		Unsigned:       se.Unsigned(),
		OriginServerTS: se.OriginServerTS(),
		EventID:        se.EventID(),
		Redacts:        se.Redacts(),
	}

	switch format {
	case FormatAll:
		ce.RoomID = se.RoomID().String()
	case FormatSync:
	case FormatSyncFederation:
		ce.RoomID = se.RoomID().String()
		ce.AuthEvents = se.AuthEventIDs()
		ce.PrevEvents = se.PrevEventIDs()
		ce.Depth = se.Depth()
		// TODO: Set Signatures & Hashes fields
	}

	if format != FormatSyncFederation && se.Version() == gomatrixserverlib.RoomVersionPseudoIDs {
		err := updatePseudoIDs(&ce, se, userIDForSender, format)
		if err != nil {
			return nil, err
		}
	}

	return &ce, nil
}

func updatePseudoIDs(ce *ClientEvent, se gomatrixserverlib.PDU, userIDForSender spec.UserIDForSender, format ClientEventFormat) error {
	ce.SenderKey = se.SenderID()

	userID, err := userIDForSender(se.RoomID(), se.SenderID())
	if err == nil && userID != nil {
		ce.Sender = userID.String()
	}

	sk := se.StateKey()
	if sk != nil && *sk != "" {
		skUserID, err := userIDForSender(se.RoomID(), spec.SenderID(*sk))
		if err == nil && skUserID != nil {
			skString := skUserID.String()
			ce.StateKey = &skString
		}
	}

	var prev PrevEventRef
	if err := json.Unmarshal(se.Unsigned(), &prev); err == nil && prev.PrevSenderID != "" {
		prevUserID, err := userIDForSender(se.RoomID(), spec.SenderID(prev.PrevSenderID))
		if err == nil && userID != nil {
			prev.PrevSenderID = prevUserID.String()
		} else {
			errString := "userID unknown"
			if err != nil {
				errString = err.Error()
			}
			logrus.Warnf("Failed to find userID for prev_sender in ClientEvent: %s", errString)
			// NOTE: Not much can be done here, so leave the previous value in place.
		}
		ce.Unsigned, err = json.Marshal(prev)
		if err != nil {
			err = fmt.Errorf("Failed to marshal unsigned content for ClientEvent: %w", err)
			return err
		}
	}

	switch se.Type() {
	case spec.MRoomCreate:
		updatedContent, err := updateCreateEvent(se.Content(), userIDForSender, se.RoomID())
		if err != nil {
			err = fmt.Errorf("Failed to update m.room.create event for ClientEvent: %w", err)
			return err
		}
		ce.Content = updatedContent
	case spec.MRoomMember:
		updatedEvent, err := updateInviteEvent(userIDForSender, se, format)
		if err != nil {
			err = fmt.Errorf("Failed to update m.room.member event for ClientEvent: %w", err)
			return err
		}
		if updatedEvent != nil {
			ce.Unsigned = updatedEvent.Unsigned()
		}
	case spec.MRoomPowerLevels:
		updatedEvent, err := updatePowerLevelEvent(userIDForSender, se, format)
		if err != nil {
			err = fmt.Errorf("Failed update m.room.power_levels event for ClientEvent: %w", err)
			return err
		}
		if updatedEvent != nil {
			ce.Content = updatedEvent.Content()
			ce.Unsigned = updatedEvent.Unsigned()
		}
	}

	return nil
}

func updateCreateEvent(content spec.RawJSON, userIDForSender spec.UserIDForSender, roomID spec.RoomID) (spec.RawJSON, error) {
	if creator := gjson.GetBytes(content, "creator"); creator.Exists() {
		oldCreator := creator.Str
		userID, err := userIDForSender(roomID, spec.SenderID(oldCreator))
		if err != nil {
			err = fmt.Errorf("Failed to find userID for creator in ClientEvent: %w", err)
			return nil, err
		}

		if userID != nil {
			var newCreatorBytes, newContent []byte
			newCreatorBytes, err = json.Marshal(userID.String())
			if err != nil {
				err = fmt.Errorf("Failed to marshal new creator for ClientEvent: %w", err)
				return nil, err
			}

			newContent, err = sjson.SetRawBytes([]byte(content), "creator", newCreatorBytes)
			if err != nil {
				err = fmt.Errorf("Failed to set new creator for ClientEvent: %w", err)
				return nil, err
			}

			return newContent, nil
		}
	}

	return content, nil
}

func updateInviteEvent(userIDForSender spec.UserIDForSender, ev gomatrixserverlib.PDU, eventFormat ClientEventFormat) (gomatrixserverlib.PDU, error) {
	if inviteRoomState := gjson.GetBytes(ev.Unsigned(), "invite_room_state"); inviteRoomState.Exists() {
		userID, err := userIDForSender(ev.RoomID(), ev.SenderID())
		if err != nil || userID == nil {
			if err != nil {
				err = fmt.Errorf("invalid userID found when updating invite_room_state: %w", err)
			}
			return nil, err
		}

		newState, err := GetUpdatedInviteRoomState(userIDForSender, inviteRoomState, ev, ev.RoomID(), eventFormat)
		if err != nil {
			return nil, err
		}

		var newEv []byte
		newEv, err = sjson.SetRawBytes(ev.JSON(), "unsigned.invite_room_state", newState)
		if err != nil {
			return nil, err
		}

		return gomatrixserverlib.MustGetRoomVersion(ev.Version()).NewEventFromTrustedJSON(newEv, false)
	}

	return ev, nil
}

type InviteRoomStateEvent struct {
	Content  spec.RawJSON `json:"content"`
	SenderID string       `json:"sender"`
	StateKey *string      `json:"state_key"`
	Type     string       `json:"type"`
}

func GetUpdatedInviteRoomState(userIDForSender spec.UserIDForSender, inviteRoomState gjson.Result, event gomatrixserverlib.PDU, roomID spec.RoomID, eventFormat ClientEventFormat) (spec.RawJSON, error) {
	var res spec.RawJSON
	inviteStateEvents := []InviteRoomStateEvent{}
	err := json.Unmarshal([]byte(inviteRoomState.Raw), &inviteStateEvents)
	if err != nil {
		return nil, err
	}

	if event.Version() == gomatrixserverlib.RoomVersionPseudoIDs && eventFormat != FormatSyncFederation {
		for i, ev := range inviteStateEvents {
			userID, userIDErr := userIDForSender(roomID, spec.SenderID(ev.SenderID))
			if userIDErr != nil {
				return nil, userIDErr
			}
			if userID != nil {
				inviteStateEvents[i].SenderID = userID.String()
			}

			if ev.StateKey != nil && *ev.StateKey != "" {
				userID, senderErr := userIDForSender(roomID, spec.SenderID(*ev.StateKey))
				if senderErr != nil {
					return nil, senderErr
				}
				if userID != nil {
					user := userID.String()
					inviteStateEvents[i].StateKey = &user
				}
			}

			updatedContent, updateErr := updateCreateEvent(ev.Content, userIDForSender, roomID)
			if updateErr != nil {
				updateErr = fmt.Errorf("Failed to update m.room.create event for ClientEvent: %w", userIDErr)
				return nil, updateErr
			}
			inviteStateEvents[i].Content = updatedContent
		}
	}

	res, err = json.Marshal(inviteStateEvents)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func updatePowerLevelEvent(userIDForSender spec.UserIDForSender, se gomatrixserverlib.PDU, eventFormat ClientEventFormat) (gomatrixserverlib.PDU, error) {
	if !se.StateKeyEquals("") {
		return se, nil
	}

	newEv := se.JSON()

	usersField := gjson.GetBytes(se.JSON(), "content.users")
	if usersField.Exists() {
		pls, err := gomatrixserverlib.NewPowerLevelContentFromEvent(se)
		if err != nil {
			return nil, err
		}

		newPls := make(map[string]int64)
		var userID *spec.UserID
		for user, level := range pls.Users {
			if eventFormat != FormatSyncFederation {
				userID, err = userIDForSender(se.RoomID(), spec.SenderID(user))
				if err != nil {
					return nil, err
				}
				user = userID.String()
			}
			newPls[user] = level
		}

		var newPlBytes []byte
		newPlBytes, err = json.Marshal(newPls)
		if err != nil {
			return nil, err
		}
		newEv, err = sjson.SetRawBytes(se.JSON(), "content.users", newPlBytes)
		if err != nil {
			return nil, err
		}
	}

	// do the same for prev content
	prevUsersField := gjson.GetBytes(se.JSON(), "unsigned.prev_content.users")
	if prevUsersField.Exists() {
		prevContent := gjson.GetBytes(se.JSON(), "unsigned.prev_content")
		if !prevContent.Exists() {
			evNew, err := gomatrixserverlib.MustGetRoomVersion(se.Version()).NewEventFromTrustedJSON(newEv, false)
			if err != nil {
				return nil, err
			}

			return evNew, err
		}
		pls := gomatrixserverlib.PowerLevelContent{}
		err := json.Unmarshal([]byte(prevContent.Raw), &pls)
		if err != nil {
			return nil, err
		}

		newPls := make(map[string]int64)
		for user, level := range pls.Users {
			if eventFormat != FormatSyncFederation {
				userID, userErr := userIDForSender(se.RoomID(), spec.SenderID(user))
				if userErr != nil {
					return nil, userErr
				}
				user = userID.String()
			}
			newPls[user] = level
		}

		var newPlBytes []byte
		newPlBytes, err = json.Marshal(newPls)
		if err != nil {
			return nil, err
		}
		newEv, err = sjson.SetRawBytes(newEv, "unsigned.prev_content.users", newPlBytes)
		if err != nil {
			return nil, err
		}
	}

	evNew, err := gomatrixserverlib.MustGetRoomVersion(se.Version()).NewEventFromTrustedJSONWithEventID(se.EventID(), newEv, false)
	if err != nil {
		return nil, err
	}

	return evNew, err
}

// AnnotateEventWithMembership adds the requesting user's membership state to the event's unsigned field.
// This implements MSC4115: Membership metadata on events.
//
// The membership parameter should be the user's membership state at the time of the event:
// - "join" if the user was joined
// - "invite" if the user was invited
// - "leave" if the user had not yet joined, been invited, or had left
// - "ban" if the user was banned
// - "knock" if the user was knocking
//
// This function modifies the ClientEvent in place by adding the membership field to unsigned.
// It supports both the stable field name ("membership") and unstable field name
// ("io.element.msc4115.membership") for backwards compatibility.
//
// Returns an error if the unsigned field cannot be modified.
func AnnotateEventWithMembership(event *ClientEvent, membership string, useStableIdentifier bool) error {
	if event == nil {
		return fmt.Errorf("cannot annotate nil event")
	}

	// Choose field name based on stability preference
	// For sjson, dots in field names need to be escaped with backslashes
	// to prevent them from being interpreted as nested paths
	fieldName := UnsignedFieldMSC4115Membership
	sjsonFieldName := "io\\.element\\.msc4115\\.membership"
	if useStableIdentifier {
		fieldName = UnsignedFieldMembership
		sjsonFieldName = fieldName
	}

	// If unsigned is empty, create a minimal JSON object
	unsigned := event.Unsigned
	if len(unsigned) == 0 {
		unsigned = spec.RawJSON("{}")
	}

	// Add membership field to unsigned using sjson
	membershipJSON, err := json.Marshal(membership)
	if err != nil {
		return fmt.Errorf("failed to marshal membership value: %w", err)
	}

	newUnsigned, err := sjson.SetRawBytes(unsigned, sjsonFieldName, membershipJSON)
	if err != nil {
		return fmt.Errorf("failed to set membership in unsigned: %w", err)
	}

	event.Unsigned = newUnsigned
	return nil
}

// DetermineMembershipAtEvent determines what the user's membership was at the time of the given event.
// This follows MSC4115's algorithm:
//
// 1. If the event is the user's own membership event, use that event's membership
// 2. Otherwise, look up the membership from the provided state (state after event)
// 3. Default to "leave" if no membership found
//
// Parameters:
//   - event: The PDU event we're determining membership for
//   - userID: The user whose membership we're checking
//   - stateAfterEvent: Map of (event_type, state_key) -> PDU representing state after this event
//
// Returns the membership string ("join", "invite", "leave", "ban", "knock")
func DetermineMembershipAtEvent(
	event gomatrixserverlib.PDU,
	userID string,
	stateAfterEvent map[string]gomatrixserverlib.PDU,
) string {
	// Case 1: This is the user's own membership event
	if event.Type() == spec.MRoomMember && event.StateKey() != nil && *event.StateKey() == userID {
		membership := gjson.GetBytes(event.Content(), "membership")
		if membership.Exists() {
			return membership.String()
		}
	}

	// Case 2: Look up membership from state after event
	if stateAfterEvent != nil {
		stateKey := spec.MRoomMember + "|" + userID
		if memberEvent, ok := stateAfterEvent[stateKey]; ok {
			membership := gjson.GetBytes(memberEvent.Content(), "membership")
			if membership.Exists() {
				return membership.String()
			}
		}
	}

	// Case 3: Default to "leave"
	return "leave"
}

// AnnotateEventsWithMembership adds membership metadata to a list of ClientEvents.
// This is a convenience function for annotating multiple events with the same membership.
//
// Parameters:
//   - events: Slice of ClientEvent pointers to annotate
//   - membership: The membership state to add to all events
//   - useStableIdentifier: Whether to use stable ("membership") or unstable field name
//
// Returns an error if any event fails to be annotated.
func AnnotateEventsWithMembership(events []ClientEvent, membership string, useStableIdentifier bool) error {
	for i := range events {
		if err := AnnotateEventWithMembership(&events[i], membership, useStableIdentifier); err != nil {
			return fmt.Errorf("failed to annotate event %d: %w", i, err)
		}
	}
	return nil
}
