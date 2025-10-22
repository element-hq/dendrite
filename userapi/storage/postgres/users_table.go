package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/element-hq/dendrite/userapi/api"
	"github.com/element-hq/dendrite/userapi/storage/tables"
	"github.com/matrix-org/gomatrixserverlib/spec"
)

type usersStatements struct {
	db *sql.DB
}

func NewPostgresUsersTable(db *sql.DB) (tables.UsersTable, error) {
	return &usersStatements{db: db}, nil
}

func (s *usersStatements) SelectUsers(ctx context.Context, params tables.SelectUsersParams) ([]api.UserResult, int64, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	orderColumn := map[api.UserSortBy]string{
		api.UserSortByCreated:  "created_ts",
		api.UserSortByLastSeen: "last_seen_ts",
	}[params.SortBy]
	if orderColumn == "" {
		orderColumn = "created_ts"
	}
	orderDir := "DESC"

	var (
		builder strings.Builder
		args    []any
		argPos  = 1
	)

	builder.WriteString(`
WITH user_data AS (
    SELECT a.localpart, a.server_name, a.created_ts, a.is_deactivated, a.account_type,
           COALESCE(p.display_name, '') AS display_name,
           COALESCE(p.avatar_url, '') AS avatar_url,
           COALESCE(MAX(d.last_seen_ts), 0) AS last_seen_ts
    FROM userapi_accounts a
    LEFT JOIN userapi_profiles p ON a.localpart = p.localpart AND a.server_name = p.server_name
    LEFT JOIN userapi_devices d ON a.localpart = d.localpart AND a.server_name = d.server_name
    WHERE a.server_name = $1`)
	args = append(args, string(params.ServerName))

	if search := strings.TrimSpace(params.Search); search != "" {
		argPos++
		pattern := "%" + strings.ToLower(search) + "%"
		builder.WriteString(fmt.Sprintf(`
      AND (
          LOWER(a.localpart) LIKE $%[1]d OR
          LOWER(p.display_name) LIKE $%[1]d OR
          LOWER('@' || a.localpart || ':' || a.server_name) LIKE $%[1]d
      )`, argPos))
		args = append(args, pattern)
	}

	if params.Deactivated != nil {
		argPos++
		builder.WriteString(fmt.Sprintf("\n      AND a.is_deactivated = $%d", argPos))
		args = append(args, *params.Deactivated)
	}

	builder.WriteString(`
    GROUP BY a.localpart, a.server_name, a.created_ts, a.is_deactivated, a.account_type, p.display_name, p.avatar_url
)
SELECT localpart, server_name, created_ts, is_deactivated, account_type,
       display_name, avatar_url, last_seen_ts,
       COUNT(*) OVER () AS total_count
FROM user_data
`)
	builder.WriteString(fmt.Sprintf("ORDER BY %s %s\n", orderColumn, orderDir))

	argPos++
	builder.WriteString(fmt.Sprintf("LIMIT $%d\n", argPos))
	args = append(args, limit)

	argPos++
	builder.WriteString(fmt.Sprintf("OFFSET $%d", argPos))
	args = append(args, offset)

	rows, err := s.db.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var (
		results []api.UserResult
		total   int64
	)

	for rows.Next() {
		var (
			localpart      string
			serverName     string
			createdTS      int64
			isDeactivated  bool
			accountTypeVal int16
			displayName    sql.NullString
			avatarURL      sql.NullString
			lastSeenTS     sql.NullInt64
			rowTotal       int64
		)

		if err = rows.Scan(&localpart, &serverName, &createdTS, &isDeactivated, &accountTypeVal, &displayName, &avatarURL, &lastSeenTS, &rowTotal); err != nil {
			return nil, 0, err
		}

		total = rowTotal

		user := api.UserResult{
			UserID:      fmt.Sprintf("@%s:%s", localpart, serverName),
			DisplayName: "",
			AvatarURL:   "",
			CreatedTS:   toTimestamp(createdTS),
			LastSeenTS:  0,
			Deactivated: isDeactivated,
			Admin:       api.AccountType(accountTypeVal) == api.AccountTypeAdmin,
		}

		if displayName.Valid {
			user.DisplayName = displayName.String
		}
		if avatarURL.Valid {
			user.AvatarURL = avatarURL.String
		}
		if lastSeenTS.Valid {
			user.LastSeenTS = toTimestamp(lastSeenTS.Int64)
		}

		results = append(results, user)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, err
	}

	if len(results) == 0 {
		count, err := s.CountUsers(ctx, tables.CountUsersParams{
			ServerName:  params.ServerName,
			Search:      params.Search,
			Deactivated: params.Deactivated,
		})
		if err != nil {
			return nil, 0, err
		}
		total = count
	}

	return results, total, nil
}

func (s *usersStatements) CountUsers(ctx context.Context, params tables.CountUsersParams) (int64, error) {
	var (
		builder strings.Builder
		args    []any
		argPos  = 1
	)

	builder.WriteString(`
SELECT COUNT(*) FROM (
    SELECT a.localpart
    FROM userapi_accounts a
    LEFT JOIN userapi_profiles p ON a.localpart = p.localpart AND a.server_name = p.server_name
    WHERE a.server_name = $1`)
	args = append(args, string(params.ServerName))

	if search := strings.TrimSpace(params.Search); search != "" {
		argPos++
		pattern := "%" + strings.ToLower(search) + "%"
		builder.WriteString(fmt.Sprintf(`
      AND (
          LOWER(a.localpart) LIKE $%[1]d OR
          LOWER(p.display_name) LIKE $%[1]d OR
          LOWER('@' || a.localpart || ':' || a.server_name) LIKE $%[1]d
      )`, argPos))
		args = append(args, pattern)
	}

	if params.Deactivated != nil {
		argPos++
		builder.WriteString(fmt.Sprintf("\n      AND a.is_deactivated = $%d", argPos))
		args = append(args, *params.Deactivated)
	}

	builder.WriteString(`
    GROUP BY a.localpart, a.server_name
) AS filtered_users`)

	row := s.db.QueryRowContext(ctx, builder.String(), args...)
	var count int64
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func toTimestamp(ts int64) spec.Timestamp {
	if ts < 0 {
		return 0
	}
	return spec.Timestamp(ts)
}
