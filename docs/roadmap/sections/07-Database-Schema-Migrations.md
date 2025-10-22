# 5. Database Schema & Migrations (Expanded)

## Layout
- **Per-component schemas** (e.g., `dendrite_roomserver`, `dendrite_syncapi`, `dendrite_userapi`, `dendrite_federationapi`, etc.) in PostgreSQL. Isolation reduces coupling and eases targeted migrations.
- **Backends:** PostgreSQL (production), SQLite (testing/small deployments). DBs are **not** compatible with Synapse schemas.

## Migration System
- Each component maintains migrations under `storage/postgres/deltas/` and `storage/sqlite3/deltas/`. On startup, Dendrite auto-applies missing deltas to upgrade schemas.

## New Feature Ownership
- **OIDC tables →** `userapi/storage/` (providers, sessions, tokens, refresh).
- **Sliding Sync tables →** `syncapi/storage/` (per-device list/window state, ordering indices, tokens).
- **Key Backup tables →** `userapi/storage/` (backup versions, encrypted payloads) or new `keyserver/` if split later.
- **URL Preview cache →** `mediaapi/storage/` (preview metadata, thumbnails, TTL).

## Indexing & Patterns
- Add composite indexes for stream position queries (sync), event lookup (roomserver), device queries (userapi).
- Prefer fewer, larger transactions in Postgres; avoid SQLite for high write loads.
