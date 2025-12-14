package caching

import "github.com/matrix-org/gomatrixserverlib/fclient"

// RoomHierarchyCache caches responses to federated room hierarchy requests (A.K.A. 'space summaries')
type RoomHierarchyCache interface {
	GetRoomHierarchy(roomID string) (r fclient.RoomHierarchyResponse, ok bool)
	StoreRoomHierarchy(roomID string, r fclient.RoomHierarchyResponse)
	// GetRoomHierarchyFailure returns true if we've recently failed to fetch hierarchy for this room
	GetRoomHierarchyFailure(roomID string) (ok bool)
	// StoreRoomHierarchyFailure marks a room as having failed federation hierarchy lookup
	StoreRoomHierarchyFailure(roomID string)
}

func (c Caches) GetRoomHierarchy(roomID string) (r fclient.RoomHierarchyResponse, ok bool) {
	return c.RoomHierarchies.Get(roomID)
}

func (c Caches) StoreRoomHierarchy(roomID string, r fclient.RoomHierarchyResponse) {
	c.RoomHierarchies.Set(roomID, r)
}

func (c Caches) GetRoomHierarchyFailure(roomID string) (ok bool) {
	_, ok = c.RoomHierarchyFailures.Get(roomID)
	return ok
}

func (c Caches) StoreRoomHierarchyFailure(roomID string) {
	c.RoomHierarchyFailures.Set(roomID, struct{}{})
}
