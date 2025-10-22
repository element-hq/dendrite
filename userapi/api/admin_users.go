package api

import "github.com/matrix-org/gomatrixserverlib/spec"

// UserSortBy represents the sortable fields for admin user queries.
type UserSortBy string

const (
	UserSortByCreated  UserSortBy = "created_ts"
	UserSortByLastSeen UserSortBy = "last_seen_ts"
)

// UserResult captures the user data returned from admin user queries.
type UserResult struct {
	UserID      string         `json:"user_id"`
	DisplayName string         `json:"display_name"`
	AvatarURL   string         `json:"avatar_url"`
	CreatedTS   spec.Timestamp `json:"created_ts"`
	LastSeenTS  spec.Timestamp `json:"last_seen_ts"`
	Deactivated bool           `json:"deactivated"`
	Admin       bool           `json:"admin"`
}

// QueryAdminUsersRequest contains the filters for listing users via the admin API.
type QueryAdminUsersRequest struct {
	ServerName  spec.ServerName
	Search      string
	From        int
	Limit       int
	SortBy      UserSortBy
	Deactivated *bool
}

// QueryAdminUsersResponse contains the results for the admin users query.
type QueryAdminUsersResponse struct {
	Users []UserResult
	Total int64
	// NextFrom is the offset that should be used to fetch the next page. A value of -1 indicates no more results.
	NextFrom int
}
