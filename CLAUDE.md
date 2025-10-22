# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Dendrite is a second-generation Matrix homeserver written in Go. It's designed as an efficient, reliable alternative to Synapse with a microservice-oriented architecture.

**Key Characteristics**:
- Go 1.23+ codebase using Go modules
- Dual-licensed: AGPL-3.0 or Element Commercial License
- PostgreSQL and SQLite database support
- NATS JetStream for internal pub/sub messaging
- Active development targeting Matrix 2.0 features (Sliding Sync, MAS)

## Development Commands

### Building

```bash
# Build all binaries to bin/
make build
# Or directly with go
go build -o bin/ ./cmd/...

# Build specific binary
go build -o bin/dendrite ./cmd/dendrite
go build -o bin/create-account ./cmd/create-account
go build -o bin/generate-keys ./cmd/generate-keys
```

### Testing

```bash
# Run all unit tests
make test
# Or: go test ./...

# Run tests with coverage report
make test-coverage

# Generate HTML coverage report (opens in browser)
make coverage-report

# Run tests with race detector
make test-race

# Run short tests only (for pre-commit)
make test-short

# Check coverage meets 70% threshold
make coverage-check

# Run specific package tests
go test ./roomserver/...
go test -v ./internal/sqlutil/...

# Run single test
go test -run TestFunctionName ./package/path

# Run tests with coverage for specific package
go test -coverprofile=coverage.out ./mediaapi/routing/...
go tool cover -func=coverage.out
go tool cover -html=coverage.out -o coverage.html
```

### Code Quality

```bash
# Run linter (requires golangci-lint installed)
make lint

# Format code
make fmt
# Or: gofmt -s -w .

# Install pre-commit hook
make pre-commit-install

# Clean build artifacts
make clean
```

### Running Dendrite Locally

```bash
# Generate Matrix signing key (required)
./bin/generate-keys --private-key matrix_key.pem

# Generate self-signed TLS certificate (optional, for testing)
./bin/generate-keys --tls-cert server.crt --tls-key server.key

# Copy and configure
cp dendrite-sample.yaml dendrite.yaml
# Edit dendrite.yaml: set server_name, database paths, key paths

# Run the server
./bin/dendrite --tls-cert server.crt --tls-key server.key --config dendrite.yaml

# Create user account (in another terminal)
./bin/create-account --config dendrite.yaml --username alice
# Add --admin flag for admin users
```

## Architecture

Dendrite uses a **component-based architecture** where each major feature area is isolated into its own package. Components communicate via:
1. **Direct API calls** (interface-based) for in-process monolith mode
2. **NATS JetStream** for asynchronous event propagation
3. **HTTP APIs** for client/federation communication

### Core Components

Located in top-level directories, each is a semi-independent service:

#### `roomserver/`
- **Purpose**: Authoritative source for room state and event storage
- **Key responsibility**: Event validation, state resolution, DAG maintenance
- **Storage**: Room events, state snapshots, membership
- **API**: `roomserver/api/` - `RoomserverInternalAPI` interface
- **Critical package**: Core of Dendrite's event processing

#### `clientapi/`
- **Purpose**: Implements Matrix Client-Server API
- **Endpoints**: `/sync`, `/send`, `/register`, `/login`, `/profile`, etc.
- **Dependencies**: Calls roomserver, syncapi, userapi
- **Routing**: `clientapi/routing/`

#### `federationapi/`
- **Purpose**: Implements Matrix Server-Server (Federation) API
- **Key functions**: Event sending/receiving between homeservers, `/send`, `/make_join`, `/send_join`
- **Queue system**: `federationapi/queue/` - outbound federation queue with retry logic
- **Statistics**: Tracks server health and backoff

#### `syncapi/`
- **Purpose**: Handles `/sync` endpoint state for incremental client updates
- **Storage**: Sync positions, notification counts, typing notifications
- **Notifier**: Real-time event notification system (`syncapi/notifier/`)

#### `mediaapi/`
- **Purpose**: Media repository (upload/download files, thumbnails)
- **Endpoints**: `/_matrix/media/v3/upload`, `/download`, `/thumbnail`
- **Storage**: Media metadata + filesystem or database blob storage

#### `userapi/`
- **Purpose**: User account management and authentication
- **Storage**: User accounts, passwords, devices, access tokens
- **Key features**: Account creation, login, device management

#### `appservice/`
- **Purpose**: Application Service API support
- **Function**: Routes events to registered application services (bridges, bots)

#### `relayapi/`
- **Purpose**: Relay server support for P2P Matrix
- **Use case**: Federating when direct connections aren't possible

### Supporting Packages

#### `internal/`
Shared utilities and common code:
- `internal/caching/` - In-memory caching (Ristretto-based)
- `internal/sqlutil/` - Database abstraction (PostgreSQL/SQLite)
- `internal/pushrules/` - Push notification rule evaluation
- `internal/httputil/` - HTTP helpers, routing, error handling
- `internal/eventutil/` - Event building and validation helpers
- `internal/fulltext/` - Full-text search (Bleve)
- `internal/transactions/` - Transaction ID deduplication

#### `setup/`
Application initialization and configuration:
- `setup/config/` - YAML configuration parsing and validation
- `setup/jetstream/` - NATS JetStream setup
- `setup/process/` - Process lifecycle management
- `setup/base/` - Base dependencies (HTTP server, databases)

#### `cmd/`
Binary entrypoints:
- `cmd/dendrite/` - Main monolith server
- `cmd/create-account/` - CLI tool for creating users
- `cmd/generate-keys/` - Generate signing keys and certificates
- `cmd/generate-config/` - Generate sample config
- `cmd/dendrite-demo-pinecone/` - P2P demo using Pinecone overlay
- `cmd/dendrite-demo-yggdrasil/` - P2P demo using Yggdrasil

#### `test/`
Testing infrastructure:
- `test/testrig/` - Test harness for integration tests

### Data Flow Example: Sending a Message

1. **Client** → `POST /_matrix/client/v3/rooms/{roomId}/send/m.room.message` → **clientapi**
2. **clientapi** validates request, builds event → calls `roomserver.InputRoomEvents()`
3. **roomserver** validates event, performs state resolution, stores in database
4. **roomserver** publishes event to NATS JetStream topic
5. **syncapi** consumes event from NATS, updates sync state
6. **federationapi** consumes event, queues for remote server delivery
7. **appservice** consumes event, forwards to registered application services
8. **syncapi** notifies waiting `/sync` requests, returns event to clients

### Database Architecture

- **Storage abstraction**: Each component has `storage/` package with PostgreSQL and SQLite implementations
- **Shared interfaces**: `storage/tables/` defines table interfaces
- **Migrations**: `storage/postgres/deltas/` and `storage/sqlite3/deltas/`
- **Connection pooling**: Managed by `internal/sqlutil/`

Components maintain their own databases/schemas:
- `dendrite_roomserver` - Room events and state
- `dendrite_syncapi` - Sync positions and state
- `dendrite_mediaapi` - Media metadata
- `dendrite_userapi` - User accounts and devices
- etc.

### Testing Patterns

#### Unit Tests
- Co-located with source: `package_test.go` alongside `package.go`
- Use `testrig` for common test infrastructure
- Table-driven tests are common
- Mock interfaces defined in `api/` packages using standard Go interfaces

#### Table Tests in Storage Layer
- Files like `roomserver/storage/tables/*_table_test.go` test database operations
- Use `test.WithAllDatabases()` to run against both PostgreSQL and SQLite

#### Integration Tests
- Located in package root (e.g., `roomserver/roomserver_test.go`)
- May require PostgreSQL (set via environment variables)
- Often use Docker containers for dependencies

#### Coverage Workflow
See `docs/development/test-coverage-workflow.md` for detailed testing workflow.

**Key practices**:
- Use `unit-test-writer` agent for generating comprehensive unit tests
- Target ≥70% overall coverage, ≥80% for new code
- Use `make test-coverage` and `make coverage-report` frequently
- Always run `make test-race` to detect race conditions

## Key Conventions

### Code Organization
- **API interfaces first**: Each component exports clean interfaces in `api/` package
- **Internal implementations**: Implementation details in `internal/` subdirectory
- **Storage isolation**: Database code isolated in `storage/` subdirectory with interface definitions

### Error Handling
- Use wrapped errors with context: `fmt.Errorf("failed to do X: %w", err)`
- Define typed errors for API boundaries (e.g., `api.ErrRoomUnknownOrNotAllowed`)
- Log errors at the point of handling, not at every level

### Context Usage
- All API calls take `context.Context` as first parameter
- Use context for request tracing, cancellation, deadlines
- Don't store context in structs (pass it through call chains)

### Logging
- Use `logrus` for structured logging
- Include relevant context fields: `logrus.WithFields(logrus.Fields{...})`
- Log at appropriate levels: Debug, Info, Warn, Error

### JSON Handling
- Use `encoding/json` for standard JSON
- Use `tidwall/gjson` and `tidwall/sjson` for efficient JSON querying/modification without full unmarshaling

## Important Gotchas

### Race Conditions
- **Always run `make test-race`** before committing
- Common issue: Loop variable capture in goroutines - use loop variable shadowing:
  ```go
  for _, item := range items {
      item := item // Shadow variable for goroutine safety
      go func() {
          process(item) // Now safe
      }()
  }
  ```
- Parallel subtests require proper variable scoping:
  ```go
  for name, tc := range tests {
      tc := tc // Required for parallel tests
      t.Run(name, func(t *testing.T) {
          t.Parallel()
          // Use tc safely
      })
  }
  ```

### Database Testing
- Tests that touch databases should support both PostgreSQL and SQLite
- Use `test.WithAllDatabases(t, func(t *testing.T, db *sql.DB) { ... })`
- Clean up test data properly to avoid flaky tests

### Event Handling
- Matrix events are immutable - never modify received events
- Use gomatrixserverlib for event building and validation
- Respect room versions - different rooms may have different validation rules

### Federation
- Federation traffic can be untrusted - validate everything
- Server keys need verification - use the keyring properly
- Implement proper backoff for failing servers

## Testing Culture

This project maintains high test coverage standards:
- Overall project coverage target: ≥70%
- New code (patches): ≥80% coverage
- Critical packages: ≥75% coverage

**Before committing**:
1. Run `make test` - all tests must pass
2. Run `make test-race` - no race conditions allowed
3. Run `make lint` - code must pass linting
4. Check coverage for modified packages

**Test writing workflow**:
- Use the `unit-test-writer` agent to generate tests (don't write manually)
- Verify tests pass and improve coverage
- See `docs/development/test-coverage-workflow.md` for detailed workflow

## Git Workflow

### Commits
- Sign off commits with `-s` flag: `git commit -s -m "message"`
- This adds `Signed-off-by: Your Name <email>` per DCO requirements
- Write clear commit messages describing "why" not just "what"
- Reference issues: `Fixes #123` or `Relates to #456`

### Pull Requests
- Create PRs against `main` branch
- Include test coverage for new code
- Ensure CI passes (tests, linting, coverage)
- Respond to review feedback promptly

### CI Requirements
- All tests must pass
- No race conditions detected
- Linting must pass (golangci-lint)
- Coverage should not decrease

## Useful Resources

- **Documentation**: `docs/` directory
- **Matrix spec**: https://spec.matrix.org/
- **Element documentation**: https://element-hq.github.io/dendrite/
- **Matrix community rooms**:
  - `#dendrite:matrix.org` - General discussion
  - `#dendrite-dev:matrix.org` - Development discussion
  - `#dendrite-alerts:matrix.org` - Release notifications

## Common Tasks

### Adding a New Endpoint
1. Define request/response types in component's `api/` package
2. Implement handler in component's `routing/` package
3. Register route in component's routing setup
4. Add validation logic
5. Write unit tests for handler
6. Add integration test if needed
7. Update API documentation if public-facing

### Adding a New Database Table
1. Define table interface in `storage/tables/`
2. Implement for PostgreSQL in `storage/postgres/`
3. Implement for SQLite in `storage/sqlite3/`
4. Add migration in `storage/postgres/deltas/` and `storage/sqlite3/deltas/`
5. Write table tests in `storage/tables/*_table_test.go`
6. Update storage interface to expose table operations

### Debugging
- Enable debug logging: Set log level to `debug` in config
- Use Jaeger for distributed tracing (configure in `global.tracing`)
- Check Prometheus metrics at `http://localhost:8008/metrics`
- Use `pprof` endpoints for profiling (enabled in development mode)

### Performance Considerations
- Database queries are often the bottleneck - use EXPLAIN ANALYZE
- Cache aggressively where appropriate (see `internal/caching/`)
- Be mindful of N+1 queries - batch database operations
- Profile before optimizing - don't guess at performance issues
