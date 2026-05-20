//go:build !cgo
// +build !cgo

package sqlutil

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestSQLiteNativeDSNExtension verifies that the DSN assembled by
// sqliteDSNExtension causes the connection to apply the expected PRAGMA
// values. Run with CGO_ENABLED=0 (or implicitly on builds without a C
// toolchain) so the modernc.org/sqlite driver is selected.
func TestSQLiteNativeDSNExtension(t *testing.T) {
	dsn := sqliteDSNExtension("file:" + filepath.Join(t.TempDir(), "test.db"))
	db, err := sql.Open(SQLITE_DRIVER_NAME, dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	cases := []struct{ pragma, want string }{
		{"busy_timeout", "10000"},
		{"journal_mode", "wal"},
		{"cache_size", "-32000"},
		{"synchronous", "1"}, // NORMAL = 1
	}
	for _, c := range cases {
		var got string
		if err := db.QueryRowContext(context.Background(), "PRAGMA "+c.pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", c.pragma, err)
		}
		if got != c.want {
			t.Errorf("PRAGMA %s = %q, want %q", c.pragma, got, c.want)
		}
	}
}
