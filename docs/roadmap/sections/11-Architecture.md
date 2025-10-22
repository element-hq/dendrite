# 9. Code Architecture (Expanded)

## Components (top-level packages)
- **clientapi/** — Client-Server API endpoints (registration, login, send, profiles, etc.).
- **roomserver/** — Event ingestion, room state, auth, persistence.
- **syncapi/** — Client sync timelines (legacy `/sync` and future sliding sync).
- **federationapi/** — Server-Server APIs; outbound queues, retries, key fetch.
- **userapi/** — Accounts, devices, E2EE key storage (cross-signing & backup integration).
- **mediaapi/** — Media upload/download, thumbnails (future URL previews cache).
- **appservice/** — Application service (bridge) transactions and namespaces.
- **internal/caching/** — Ristretto-backed caches for hot data.

## Internal Bus (NATS JetStream)
- Asynchronous event distribution between components.
- Supports monolith (embedded) and split-process topologies.
- Tune streams/subjects for durability and throughput; monitor lag.

## Storage
- Per-component schemas: `dendrite_roomserver`, `dendrite_syncapi`, `dendrite_userapi`, etc.
- Backends: PostgreSQL (production), SQLite (small/testing). Auto-migrations via `storage/*/deltas/`.

## Where new features integrate
- **Sliding Sync:** `syncapi/` (new handlers, storage for lists/windows and ordering indexes).
- **OIDC/MAS:** `userapi/` (OIDC providers, tokens/refresh, device binding).
- **Key Backup (MSC1212):** `userapi/` (backup versions & encrypted data).
- **Mentions/Threads:** `clientapi/` (flags) + `syncapi/` (filters, counts, push rules).
- **URL Previews:** `mediaapi/` (fetch, parse, cache; thumbnails).

## Future Evolution
- Extension hooks (auth/spam); selective multi-process hardening for `syncapi`/`federationapi`.
- Keep boundaries strict; design for distribution even if running monolith.
