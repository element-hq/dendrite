//go:build !cgo
// +build !cgo

package sqlutil

import (
	"strings"
)

const SQLITE_DRIVER_NAME = "sqlite"

func sqliteDSNExtension(dsn string) string {
	// add query parameters to the dsn
	if strings.Contains(dsn, "?") {
		dsn += "&"
	} else {
		dsn += "?"
	}

	// wait some time before erroring if the db is locked
	// https://gitlab.com/cznic/sqlite/-/issues/106#note_1058094993
	dsn += "_pragma=busy_timeout%3d10000"

	// WAL mode lets readers proceed while a writer is active and is the
	// SQLite-recommended setting for any application with concurrent
	// readers and writers, which Dendrite's storage layer always is.
	// https://www.sqlite.org/wal.html
	dsn += "&_pragma=journal_mode%3dWAL"

	// 32 MiB page cache. The pure-Go SQLite default of 2 MiB results in
	// excessive page evictions for federation and sync workloads where
	// many rooms are touched per request. Negative value = KiB.
	// https://www.sqlite.org/pragma.html#pragma_cache_size
	dsn += "&_pragma=cache_size%3d-32000"

	// NORMAL fsyncs only at WAL checkpoints rather than at every commit.
	// Safe under WAL for application-level durability that survives
	// process crashes; a power loss may roll back the most recent commit
	// to the last checkpoint. Matrix is event-sourced and re-syncs from
	// peers, so this trade-off is acceptable.
	// https://www.sqlite.org/pragma.html#pragma_synchronous
	dsn += "&_pragma=synchronous%3dNORMAL"

	return dsn
}
