// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package input

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/roomserver/api"
	"github.com/element-hq/dendrite/roomserver/state"
	"github.com/element-hq/dendrite/roomserver/types"
)

// UpdateStateAfterResync updates the current state and memberships after a partial state resync.
// This is called after state events have been stored as outliers via SendStateAsOutliers.
// It creates a new state snapshot from the stored events, calculates the state delta,
// updates the membership table, and notifies downstream components (syncapi).
//
// stateEventIDs are the event IDs of the state events that were fetched during resync.
func (r *Inputer) UpdateStateAfterResync(ctx context.Context, roomID string, stateEventIDs []string) error {
	logger := logrus.WithFields(logrus.Fields{
		"room_id":          roomID,
		"state_event_count": len(stateEventIDs),
		"trace":            "partial_state_resync",
	})
	logger.Info("Updating current state after partial state resync")

	// Get room info
	roomInfo, err := r.DB.RoomInfo(ctx, roomID)
	if err != nil {
		return fmt.Errorf("r.DB.RoomInfo: %w", err)
	}
	if roomInfo == nil {
		return fmt.Errorf("room %s not found", roomID)
	}

	// Convert state event IDs to StateEntry array
	stateEntries, err := r.DB.StateEntriesForEventIDs(ctx, stateEventIDs, true)
	if err != nil {
		return fmt.Errorf("r.DB.StateEntriesForEventIDs: %w", err)
	}

	// Debug: Count EventTypeNIDs in loaded state entries
	loadedEventTypeNIDCounts := make(map[types.EventTypeNID]int)
	loadedMemberCount := 0
	for _, entry := range stateEntries {
		loadedEventTypeNIDCounts[entry.EventTypeNID]++
		if entry.EventTypeNID == types.MRoomMemberNID {
			loadedMemberCount++
		}
	}

	logger.WithFields(logrus.Fields{
		"state_entries":           len(stateEntries),
		"loaded_member_events":    loadedMemberCount,
		"loaded_type_nid_counts":  loadedEventTypeNIDCounts,
	}).Debug("Loaded state entries from event IDs with EventTypeNID breakdown")

	if len(stateEntries) == 0 {
		logger.Warn("No state entries found for resync, skipping state update")
		return nil
	}

	// Deduplicate state entries (in case of duplicates)
	stateEntries = types.DeduplicateStateEntries(stateEntries)

	// Get the room updater (for transaction and locking)
	var succeeded bool
	updater, err := r.DB.GetRoomUpdater(ctx, roomInfo)
	if err != nil {
		return fmt.Errorf("r.DB.GetRoomUpdater: %w", err)
	}
	defer sqlutil.EndTransactionWithCheck(updater, &succeeded, &err)

	// Get current state snapshot NID
	oldStateNID := updater.CurrentStateSnapshotNID()

	logger.WithField("old_state_nid", oldStateNID).Debug("Got old state snapshot NID")

	// MSC3706 Fix: Preserve local member events from the old state.
	// The remote server's /state response doesn't include our local user's join event,
	// so we need to merge it into the new state snapshot. Without this, the local user's
	// join would be lost when we replace the state, breaking membership table updates.
	roomState := state.NewStateResolution(updater, roomInfo, r.Queryer)
	oldStateEntries, err := roomState.LoadStateAtSnapshot(ctx, oldStateNID)
	if err != nil {
		return fmt.Errorf("roomState.LoadStateAtSnapshot: %w", err)
	}

	// Build a map of state keys in the new state (from remote server) for quick lookup
	newStateKeys := make(map[types.StateKeyTuple]bool)
	for _, entry := range stateEntries {
		newStateKeys[entry.StateKeyTuple] = true
	}

	// Find local member events in the old state that aren't in the new state
	// These are membership events for local users (like our join event) that the
	// remote server doesn't know about
	localMemberCount := 0
	for _, entry := range oldStateEntries {
		if entry.EventTypeNID == types.MRoomMemberNID {
			// Check if this member event is already in the new state
			if !newStateKeys[entry.StateKeyTuple] {
				// This is a local member event not in the remote state - preserve it
				stateEntries = append(stateEntries, entry)
				newStateKeys[entry.StateKeyTuple] = true
				localMemberCount++
				logger.WithFields(logrus.Fields{
					"event_nid":         entry.EventNID,
					"event_state_key":   entry.EventStateKeyNID,
				}).Debug("Preserving local member event not in remote state")
			}
		}
	}

	if localMemberCount > 0 {
		logger.WithField("preserved_local_members", localMemberCount).
			Info("Preserved local member events in new state snapshot")
	}

	// Deduplicate again after adding local member events
	stateEntries = types.DeduplicateStateEntries(stateEntries)

	// Create a new state snapshot from the merged state entries
	newStateNID, err := updater.AddState(ctx, roomInfo.RoomNID, nil, stateEntries)
	if err != nil {
		return fmt.Errorf("updater.AddState: %w", err)
	}

	logger.WithField("new_state_nid", newStateNID).Debug("Created new state snapshot")

	// Calculate the state delta between old and new snapshots
	// Note: roomState was already created above for LoadStateAtSnapshot
	removed, added, err := roomState.DifferenceBetweeenStateSnapshots(ctx, oldStateNID, newStateNID)
	if err != nil {
		return fmt.Errorf("roomState.DifferenceBetweeenStateSnapshots: %w", err)
	}

	// Debug: Count EventTypeNIDs in added slice
	eventTypeNIDCounts := make(map[types.EventTypeNID]int)
	memberCount := 0
	for _, entry := range added {
		eventTypeNIDCounts[entry.EventTypeNID]++
		if entry.EventTypeNID == types.MRoomMemberNID {
			memberCount++
		}
	}

	logger.WithFields(logrus.Fields{
		"removed":               len(removed),
		"added":                 len(added),
		"added_member_events":   memberCount,
		"event_type_nid_counts": eventTypeNIDCounts,
		"MRoomMemberNID":        types.MRoomMemberNID,
	}).Debug("Calculated state delta with EventTypeNID breakdown")

	// MSC3706 Fix: Ensure all membership events in the new state have corresponding
	// membership rows, not just those in the state delta. This handles the case where
	// a membership event (e.g., the local user's join) was stored during partial state
	// join but the membership table was never updated because the event was treated
	// as an outlier.
	//
	// We need to process ALL membership events from the fetched state, not just
	// those that differ from the old state snapshot.
	addedMemberKeys := make(map[types.EventStateKeyNID]bool)
	for _, entry := range added {
		if entry.EventTypeNID == types.MRoomMemberNID {
			addedMemberKeys[entry.EventStateKeyNID] = true
		}
	}

	// Add membership events from stateEntries that aren't already in added
	membershipEntriesAdded := 0
	for _, entry := range stateEntries {
		if entry.EventTypeNID == types.MRoomMemberNID && !addedMemberKeys[entry.EventStateKeyNID] {
			added = append(added, entry)
			addedMemberKeys[entry.EventStateKeyNID] = true
			membershipEntriesAdded++
		}
	}

	if membershipEntriesAdded > 0 {
		logger.WithField("membership_entries_added", membershipEntriesAdded).
			Info("Added membership events from full state to ensure membership rows exist")
	}

	// Update memberships based on the state delta plus any missing membership events
	var outputEvents []api.OutputEvent
	if len(removed) > 0 || len(added) > 0 {
		// Count membership changes that will be processed
		memberChanges := 0
		for _, entry := range added {
			if entry.EventTypeNID == types.MRoomMemberNID {
				memberChanges++
			}
		}
		for _, entry := range removed {
			if entry.EventTypeNID == types.MRoomMemberNID {
				memberChanges++
			}
		}
		logger.WithField("member_changes_to_process", memberChanges).Debug("About to update memberships")

		outputEvents, err = r.updateMemberships(ctx, updater, removed, added)
		if err != nil {
			return fmt.Errorf("r.updateMemberships: %w", err)
		}
		logger.WithFields(logrus.Fields{
			"output_events":   len(outputEvents),
			"member_changes":  memberChanges,
		}).Debug("Updated memberships (output_events are for retired invites only)")
	}

	// Update the current state snapshot in the room
	// We need to use SetLatestEvents, but we want to keep the latest events unchanged
	// Just update the state snapshot NID
	latestEvents := updater.LatestEvents()
	if len(latestEvents) == 0 {
		// This shouldn't happen for a room with events, but handle gracefully
		logger.Warn("No latest events found for room, skipping state snapshot update")
		succeeded = true
		return nil
	}

	// Get the last event NID that was sent
	lastEventNID := latestEvents[0].EventNID
	for _, latest := range latestEvents {
		if latest.EventNID > lastEventNID {
			lastEventNID = latest.EventNID
		}
	}

	// Update the latest events with the new state snapshot
	if err = updater.SetLatestEvents(roomInfo.RoomNID, latestEvents, lastEventNID, newStateNID); err != nil {
		return fmt.Errorf("updater.SetLatestEvents: %w", err)
	}

	// MSC3706 State Epoch Fix: Record the state snapshot NID after resync completes.
	// This marks the current state as the "authoritative" state from the partial state resync.
	// When processing events later, we use this to detect and suppress state regressions
	// caused by out-of-order events that reference older positions in the DAG.
	if err = updater.UpdateResyncStateNID(roomInfo.RoomNID, newStateNID); err != nil {
		return fmt.Errorf("updater.UpdateResyncStateNID: %w", err)
	}

	logger.WithField("resync_state_nid", newStateNID).Debug("Recorded resync state NID to prevent state regressions")

	// Emit output events to notify downstream components about membership changes
	if len(outputEvents) > 0 {
		if err = r.OutputProducer.ProduceRoomEvents(roomID, outputEvents); err != nil {
			return fmt.Errorf("r.OutputProducer.ProduceRoomEvents: %w", err)
		}
		logger.WithField("output_events", len(outputEvents)).Debug("Produced output events for membership changes")
	}

	succeeded = true

	logger.WithFields(logrus.Fields{
		"old_state_nid": oldStateNID,
		"new_state_nid": newStateNID,
		"removed":       len(removed),
		"added":         len(added),
	}).Info("Successfully updated current state after partial state resync")

	return nil
}
