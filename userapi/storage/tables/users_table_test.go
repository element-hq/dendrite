package tables_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/element-hq/dendrite/test"
	"github.com/element-hq/dendrite/userapi/api"
	"github.com/element-hq/dendrite/userapi/storage/postgres"
	"github.com/element-hq/dendrite/userapi/storage/sqlite3"
	"github.com/element-hq/dendrite/userapi/storage/tables"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

type seededUser struct {
	userID      string
	createdTS   spec.Timestamp
	lastSeenTS  spec.Timestamp
	displayName string
	avatarURL   string
	deactivated bool
	admin       bool
}

func TestSelectUsers(t *testing.T) {
	ctx := context.Background()
	test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType) {
		db, accountsTable, profilesTable, devicesTable, usersTable, close := mustMakeUsersTables(t, dbType)
		defer close()

		seeded := seedTestUsers(t, ctx, db, dbType, accountsTable, profilesTable, devicesTable)

		t.Run("default_sort_created_desc", func(t *testing.T) {
			users, total, err := usersTable.SelectUsers(ctx, tables.SelectUsersParams{
				Limit:      2,
				SortBy:     api.UserSortByCreated,
				ServerName: spec.ServerName("localhost"),
			})
			if err != nil {
				t.Fatalf("SelectUsers failed: %v", err)
			}
			if total != int64(len(seeded)) {
				t.Fatalf("unexpected total: got %d want %d", total, len(seeded))
			}
			if len(users) != 2 {
				t.Fatalf("expected 2 users got %d", len(users))
			}
			wantFirst := seeded["@dave:localhost"]
			wantSecond := seeded["@carol:localhost"]
			assertUserEqual(t, wantFirst, users[0])
			assertUserEqual(t, wantSecond, users[1])
		})

		t.Run("pagination_second_page", func(t *testing.T) {
			users, total, err := usersTable.SelectUsers(ctx, tables.SelectUsersParams{
				Offset:     2,
				Limit:      5,
				SortBy:     api.UserSortByCreated,
				ServerName: spec.ServerName("localhost"),
			})
			if err != nil {
				t.Fatalf("SelectUsers failed: %v", err)
			}
			if total != int64(len(seeded)) {
				t.Fatalf("unexpected total: got %d want %d", total, len(seeded))
			}
			if len(users) != 2 {
				t.Fatalf("expected 2 users got %d", len(users))
			}
			assertUserEqual(t, seeded["@bob:localhost"], users[0])
			assertUserEqual(t, seeded["@alice:localhost"], users[1])
		})

		t.Run("search_by_localpart", func(t *testing.T) {
			users, total, err := usersTable.SelectUsers(ctx, tables.SelectUsersParams{
				Search:     "ali",
				SortBy:     api.UserSortByCreated,
				ServerName: spec.ServerName("localhost"),
			})
			if err != nil {
				t.Fatalf("SelectUsers failed: %v", err)
			}
			if total != 1 {
				t.Fatalf("unexpected total: got %d want %d", total, 1)
			}
			if len(users) != 1 {
				t.Fatalf("expected 1 user got %d", len(users))
			}
			assertUserEqual(t, seeded["@alice:localhost"], users[0])
		})

		t.Run("search_by_display_name", func(t *testing.T) {
			users, total, err := usersTable.SelectUsers(ctx, tables.SelectUsersParams{
				Search:     "JONES",
				SortBy:     api.UserSortByCreated,
				ServerName: spec.ServerName("localhost"),
			})
			if err != nil {
				t.Fatalf("SelectUsers failed: %v", err)
			}
			if total != 1 {
				t.Fatalf("unexpected total: got %d want %d", total, 1)
			}
			if len(users) != 1 {
				t.Fatalf("expected 1 user got %d", len(users))
			}
			assertUserEqual(t, seeded["@carol:localhost"], users[0])
		})

		t.Run("filter_by_deactivated", func(t *testing.T) {
			activeOnly := false
			users, total, err := usersTable.SelectUsers(ctx, tables.SelectUsersParams{
				Deactivated: &activeOnly,
				SortBy:      api.UserSortByCreated,
				ServerName:  spec.ServerName("localhost"),
			})
			if err != nil {
				t.Fatalf("SelectUsers failed: %v", err)
			}
			if total != 3 {
				t.Fatalf("unexpected total: got %d want %d", total, 3)
			}
			if containsUser(users, "@bob:localhost") {
				t.Fatalf("expected bob to be filtered out: %+v", users)
			}
		})

		t.Run("sort_by_last_seen_desc", func(t *testing.T) {
			users, _, err := usersTable.SelectUsers(ctx, tables.SelectUsersParams{
				SortBy:     api.UserSortByLastSeen,
				ServerName: spec.ServerName("localhost"),
			})
			if err != nil {
				t.Fatalf("SelectUsers failed: %v", err)
			}
			if len(users) == 0 {
				t.Fatalf("expected users, got none")
			}
			if users[0].UserID != "@dave:localhost" {
				t.Fatalf("expected dave first, got %s", users[0].UserID)
			}
		})

		t.Run("count_users_with_filter", func(t *testing.T) {
			inactive := true
			total, err := usersTable.CountUsers(ctx, tables.CountUsersParams{
				Deactivated: &inactive,
				ServerName:  spec.ServerName("localhost"),
			})
			if err != nil {
				t.Fatalf("CountUsers failed: %v", err)
			}
			if total != 1 {
				t.Fatalf("unexpected total: got %d want %d", total, 1)
			}
		})

		noResultsOffset := len(seeded)
		stored := int64(len(seeded))
		t.Run("offset_beyond_total_retains_total", func(t *testing.T) {
			users, total, err := usersTable.SelectUsers(ctx, tables.SelectUsersParams{
				Offset:     noResultsOffset,
				Limit:      5,
				SortBy:     api.UserSortByCreated,
				ServerName: spec.ServerName("localhost"),
			})
			if err != nil {
				t.Fatalf("SelectUsers failed: %v", err)
			}
			if len(users) != 0 {
				t.Fatalf("expected no users, got %d", len(users))
			}
			if total != stored {
				t.Fatalf("expected total %d, got %d", stored, total)
			}
		})
	})
}

func assertUserEqual(t *testing.T, want seededUser, got api.UserResult) {
	t.Helper()
	if want.userID != got.UserID {
		t.Fatalf("unexpected user id: got %s want %s", got.UserID, want.userID)
	}
	if want.displayName != got.DisplayName {
		t.Fatalf("unexpected display name: got %s want %s", got.DisplayName, want.displayName)
	}
	if want.avatarURL != got.AvatarURL {
		t.Fatalf("unexpected avatar url: got %s want %s", got.AvatarURL, want.avatarURL)
	}
	if want.createdTS != got.CreatedTS {
		t.Fatalf("unexpected created ts: got %d want %d", got.CreatedTS, want.createdTS)
	}
	if want.lastSeenTS != got.LastSeenTS {
		t.Fatalf("unexpected last seen ts: got %d want %d", got.LastSeenTS, want.lastSeenTS)
	}
	if want.deactivated != got.Deactivated {
		t.Fatalf("unexpected deactivated flag: got %t want %t", got.Deactivated, want.deactivated)
	}
	if want.admin != got.Admin {
		t.Fatalf("unexpected admin flag: got %t want %t", got.Admin, want.admin)
	}
}

func containsUser(users []api.UserResult, userID string) bool {
	for _, u := range users {
		if u.UserID == userID {
			return true
		}
	}
	return false
}

func mustMakeUsersTables(t *testing.T, dbType test.DBType) (*sql.DB, tables.AccountsTable, tables.ProfileTable, tables.DevicesTable, tables.UsersTable, func()) {
	t.Helper()

	connStr, close := test.PrepareDBConnectionString(t, dbType)
	db, err := sqlutil.Open(&config.DatabaseOptions{
		ConnectionString: config.DataSource(connStr),
	}, nil)
	if err != nil {
		t.Fatalf("failed to open db: %s", err)
	}

	var (
		accTable   tables.AccountsTable
		profTable  tables.ProfileTable
		devTable   tables.DevicesTable
		usersTable tables.UsersTable
	)

	switch dbType {
	case test.DBTypeSQLite:
		accTable, err = sqlite3.NewSQLiteAccountsTable(db, "localhost")
		if err != nil {
			t.Fatalf("unable to create accounts table: %v", err)
		}
		profTable, err = sqlite3.NewSQLiteProfilesTable(db, "")
		if err != nil {
			t.Fatalf("unable to create profiles table: %v", err)
		}
		devTable, err = sqlite3.NewSQLiteDevicesTable(db, "localhost")
		if err != nil {
			t.Fatalf("unable to create devices table: %v", err)
		}
		usersTable, err = sqlite3.NewSQLiteUsersTable(db)
		if err != nil {
			t.Fatalf("unable to create users table: %v", err)
		}
	case test.DBTypePostgres:
		accTable, err = postgres.NewPostgresAccountsTable(db, "localhost")
		if err != nil {
			t.Fatalf("unable to create accounts table: %v", err)
		}
		profTable, err = postgres.NewPostgresProfilesTable(db, "")
		if err != nil {
			t.Fatalf("unable to create profiles table: %v", err)
		}
		devTable, err = postgres.NewPostgresDevicesTable(db, "localhost")
		if err != nil {
			t.Fatalf("unable to create devices table: %v", err)
		}
		usersTable, err = postgres.NewPostgresUsersTable(db)
		if err != nil {
			t.Fatalf("unable to create users table: %v", err)
		}
	default:
		t.Fatalf("unexpected db type: %v", dbType)
	}

	return db, accTable, profTable, devTable, usersTable, close
}

func seedTestUsers(t *testing.T, ctx context.Context, db *sql.DB, dbType test.DBType, acc tables.AccountsTable, profiles tables.ProfileTable, devices tables.DevicesTable) map[string]seededUser {
	t.Helper()

	serverName := spec.ServerName("localhost")
	baseTime := time.Unix(1710000000, 0).UTC()
	users := []struct {
		localpart   string
		displayName string
		avatarURL   string
		created     time.Time
		lastSeen    *time.Time
		deactivated bool
		accountType api.AccountType
	}{
		{
			localpart:   "alice",
			displayName: "Alice Smith",
			avatarURL:   "mxc://example.org/alice",
			created:     baseTime,
			lastSeen:    ptrTime(baseTime.Add(5 * time.Hour)),
			accountType: api.AccountTypeAdmin,
		},
		{
			localpart:   "bob",
			displayName: "Bob Brown",
			avatarURL:   "",
			created:     baseTime.Add(1 * time.Hour),
			lastSeen:    ptrTime(baseTime.Add(4 * time.Hour)),
			deactivated: true,
			accountType: api.AccountTypeUser,
		},
		{
			localpart:   "carol",
			displayName: "Carol Jones",
			avatarURL:   "mxc://example.org/carol",
			created:     baseTime.Add(2 * time.Hour),
			lastSeen:    nil,
			accountType: api.AccountTypeUser,
		},
		{
			localpart:   "dave",
			displayName: "Dave King",
			avatarURL:   "",
			created:     baseTime.Add(3 * time.Hour),
			lastSeen:    ptrTime(baseTime.Add(6 * time.Hour)),
			accountType: api.AccountTypeUser,
		},
	}

	seeded := make(map[string]seededUser)

	for _, u := range users {
		if _, err := acc.InsertAccount(ctx, nil, u.localpart, serverName, "", "", u.accountType); err != nil {
			t.Fatalf("failed to insert account for %s: %v", u.localpart, err)
		}

		setAccountCreatedTimestamp(t, ctx, db, dbType, u.localpart, serverName, spec.AsTimestamp(u.created))

		if u.deactivated {
			if err := acc.DeactivateAccount(ctx, u.localpart, serverName); err != nil {
				t.Fatalf("failed to deactivate account for %s: %v", u.localpart, err)
			}
		}

		if err := profiles.InsertProfile(ctx, nil, u.localpart, serverName); err != nil {
			t.Fatalf("failed to insert profile for %s: %v", u.localpart, err)
		}
		if u.displayName != "" {
			if _, _, err := profiles.SetDisplayName(ctx, nil, u.localpart, serverName, u.displayName); err != nil {
				t.Fatalf("failed to set display name for %s: %v", u.localpart, err)
			}
		}
		if u.avatarURL != "" {
			if _, _, err := profiles.SetAvatarURL(ctx, nil, u.localpart, serverName, u.avatarURL); err != nil {
				t.Fatalf("failed to set avatar for %s: %v", u.localpart, err)
			}
		}

		lastSeenTs := spec.Timestamp(0)
		if u.lastSeen != nil {
			deviceID := fmt.Sprintf("%s-device", u.localpart)
			token := fmt.Sprintf("%s-token", u.localpart)
			if _, err := devices.InsertDevice(ctx, nil, deviceID, u.localpart, serverName, token, nil, "", ""); err != nil {
				t.Fatalf("failed to insert device for %s: %v", u.localpart, err)
			}
			ts := spec.AsTimestamp(*u.lastSeen)
			lastSeenTs = ts
			setDeviceLastSeenTimestamp(t, ctx, db, dbType, u.localpart, serverName, deviceID, ts)
		}

		userID := fmt.Sprintf("@%s:%s", u.localpart, serverName)
		seeded[userID] = seededUser{
			userID:      userID,
			createdTS:   spec.AsTimestamp(u.created),
			lastSeenTS:  lastSeenTs,
			displayName: u.displayName,
			avatarURL:   u.avatarURL,
			deactivated: u.deactivated,
			admin:       u.accountType == api.AccountTypeAdmin,
		}
	}

	return seeded
}

func setAccountCreatedTimestamp(t *testing.T, ctx context.Context, db *sql.DB, dbType test.DBType, localpart string, serverName spec.ServerName, ts spec.Timestamp) {
	t.Helper()
	query := "UPDATE userapi_accounts SET created_ts = $1 WHERE localpart = $2 AND server_name = $3"
	args := []any{int64(ts), localpart, string(serverName)}
	if dbType == test.DBTypeSQLite {
		query = "UPDATE userapi_accounts SET created_ts = ? WHERE localpart = ? AND server_name = ?"
	}
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("failed to set created_ts for %s: %v", localpart, err)
	}
}

func setDeviceLastSeenTimestamp(t *testing.T, ctx context.Context, db *sql.DB, dbType test.DBType, localpart string, serverName spec.ServerName, deviceID string, ts spec.Timestamp) {
	t.Helper()
	query := "UPDATE userapi_devices SET last_seen_ts = $1 WHERE localpart = $2 AND server_name = $3 AND device_id = $4"
	args := []any{int64(ts), localpart, string(serverName), deviceID}
	if dbType == test.DBTypeSQLite {
		query = "UPDATE userapi_devices SET last_seen_ts = ? WHERE localpart = ? AND server_name = ? AND device_id = ?"
	}
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("failed to set last_seen_ts for %s: %v", localpart, err)
	}
}

func ptrTime(ti time.Time) *time.Time {
	return &ti
}
