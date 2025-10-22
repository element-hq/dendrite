# AGENTS.md

This file provides guidance to agents when working with code in this repository.

## Project Status
**Maintenance mode** since v0.15.0 (August 2025) - security fixes only.

## Commands
```bash
# Build all binaries
go build -trimpath -v -o "bin/" ./cmd/...

# Run all tests (requires PostgreSQL service)
POSTGRES_HOST=localhost POSTGRES_USER=postgres POSTGRES_PASSWORD=postgres POSTGRES_DB=dendrite \
go test -json -v ./... 2>&1 | gotestfmt -hide all

# Run single test
go test -v ./path/to/package -run TestName

# Integration tests (excludes all cmd/)
go test -race -json -v -coverpkg=./... -coverprofile=cover.out $(go list ./... | grep -v '/cmd/')

# Lint
golangci-lint run
```

## Database Patterns
- **SQLite transaction limitation**: SQLite doesn't support nested transactions. Some operations pass `nil` for txn parameter to avoid nesting.
- **Writer.Do pattern**: Use `d.Writer.Do(d.DB, txn, func(txn *sql.Tx) error { ... })` for all database write operations.
- **NID race condition**: Insert operations return `sql.ErrNoRows` when row exists due to race condition. Code must re-select after this error.
- **Build tags**: Use `//go:build !wasm && !cgo` for database-specific code to control backend selection.

## Testing Utilities
- `test.WithAllDatabases(t, func(t *testing.T, dbType test.DBType))` - Runs test against both PostgreSQL and SQLite automatically.
- `testrig.CreateConfig(t, dbType)` - Creates test configuration with in-memory NATS and temporary databases.
- `test.NewRequest(t, method, path)` - Creates HTTP test requests with proper JSON marshaling.
- PostgreSQL tests require env vars: `POSTGRES_HOST`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`.

## Configuration
- **Config version**: v2 only. Breaking changes from v1 trigger clear error messages.
- **DataSource detection**: `IsSQLite()` checks for `file:` prefix in connection string.
- **Key ID format**: Must match `ed25519:[a-zA-Z0-9_]+` regex for new keys.
- **Virtual hosts**: Support multiple domains with separate signing keys via `cfg.Global.VirtualHosts`.

## Component Initialization
Components must be initialized in specific order with explicit dependency wiring:
1. DNS cache, tracing, Sentry
2. Federation client, connection manager
3. Caches (Ristretto)
4. NATS, roomserver
5. Other APIs with explicit `rsAPI.SetFederationAPI(fsAPI, keyRing)` calls