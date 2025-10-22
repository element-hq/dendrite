# Dendrite vs Synapse — Feature Parity Analysis (Compiled v2)

_Last compiled: **2025-10-22 07:55 UTC**_

This single report stitches together all revised sections from `feature_parity_markdown_v2/` and includes: migration path, client compatibility, admin API specifics, expanded architecture, and a more realistic roadmap with dependencies.


---

<!-- 01-Executive-Summary.md -->

# Executive Summary

Dendrite (Go) is a capable Matrix homeserver for small-to-medium deployments, but it trails Synapse (Python) on Matrix 2.0 features and some operational tooling. Core messaging, federation, and most Client-Server API basics work; gaps remain around **Sliding Sync**, **OIDC/MAS**, **Threads (full)**, **3PID flows**, and richer **admin/moderation** APIs. This pack details parity gaps, concrete implementation plans, and a pragmatic roadmap—with more realistic effort estimates and explicit dependencies.



---

<!-- 02-Critical-Gaps.md -->

# Critical Gaps Requiring Immediate Attention

1) **Sliding Sync (MSC4186)** — Missing in Dendrite; required for Element X and modern clients.  
2) **Next-Gen Auth / OIDC (MSC3861+ family)** — Missing; needed for SSO and token-based auth.  
3) **Threads (MSC3440)** — Partial: event relations exist; thread-aware sync/notifications missing.  
4) **3PID & Identity Service Flows** — Missing/Partial: registration, password reset, discovery.  
5) **Admin & Moderation Tooling** — Minimal; needs user/room management parity.  
6) **URL Previews** — Missing: safe fetch, parsing, thumbnails, SSRF protections.  
7) **Cross-signing reliability & Key Backup (MSC1212)** — Partial/incomplete.  
8) **Federation extras (MSC3083/MSC3266)** — Restricted rooms partial; Knock missing.



---

<!-- 03-Spec-Compliance.md -->

# 1. Matrix Specification Compliance

Dendrite has solid coverage of Client-Server and Federation basics. Remaining gaps align with newer Matrix 2.0 MSCs and optional/advanced flows.

**High-priority MSCs for parity:** MSC4186 (Sliding Sync), MSC3861 (OIDC/MAS), MSC3440 (Threads), MSC1212 (Key Backup), MSC3952 (Intentional mentions), MSC3083 (Restricted rooms), MSC3266 (Knock).



---

<!-- 03a-MSC-Coverage-Table.md -->

# MSC Coverage Table (Revised)

> Status key: **Complete** (implemented), **Partial** (some support; missing pieces), **Missing** (not implemented), **TBD** (verify).  
> Fill in versions/links as you validate against upstream repos and spec.

| MSC # | Title | Spec Status | Synapse | Dendrite | Priority | Notes (impl hints / code areas) |
|-----:|-------|-------------|---------|----------|----------|----------------------------------|
| 4186 | Sliding Sync | Stable/Latest | Complete | **Missing** | Critical | New sync subsystem in `syncapi/` (lists/windows, ops, ordering, tokens). |
| 3861 | OIDC / MAS | Stable | Complete | **Missing** | High | OIDC flows & tokens in `userapi/`; MAS integration points. |
| 3440 | Threads | Stable | Complete | **Partial** | High | Event relations exist; add thread-aware `/sync`, counts & push rules in `syncapi/`. |
| 1212 | Key Backup | Stable | Complete | **Partial** | High | Server-side encrypted key backup in `userapi/` storage; E2EE flows. |
| 3952 | Intentional mentions | Stable | Complete | **Missing** | Medium | Mention flags & push rules; client-visible notifications. |
| 3874 | Filter threads | Draft/Stable? | Partial/Complete | **Missing** | Medium | Sync filters for threads in `syncapi/`. |
| 3083 | Restricted rooms | Stable | Complete | **Partial** | Medium | Join rules; ensure `make_join/send_join` paths; `federationapi/`. |
| 3266 | Knock | Stable | Complete | **Missing** | Medium | Knock endpoints, membership transitions; `roomserver/`, `federationapi/`. |
| 3391 | Remove deprecated APIs | Stable | Complete | **Partial** | Medium | Audit/retire deprecated endpoints across `clientapi/`. |



---

<!-- 03b-Client-Server-API-Coverage.md -->

# Client-Server API Endpoint Coverage (Template)

List priority endpoints and their parity; use this as a working matrix during implementation.

| Endpoint | Method | Spec | Synapse | Dendrite | Complexity | Notes |
|----------|--------|------|---------|----------|------------|------|
| /_matrix/client/v3/sync | GET | v3 | Yes | Yes | L | Legacy sync; superseded by sliding sync for modern clients. |
| /_matrix/client/unstable/org.matrix.msc4186/sync | GET | MSC4186 | Yes | **No** | XL+ | Sliding Sync; requires new storage/indexing. |
| /_matrix/client/v3/preview_url | GET | v3 | Yes | **No** | M | Fetch+parse OG, SSRF protections, caching, thumbnails. |
| /_matrix/client/v3/login (OIDC) | POST | v3 | Yes | **Partial/No** | L | MAS / OIDC integration; tokens/refresh. |
| /_matrix/client/v3/register (3PID) | POST | v3 | Yes | **Partial/No** | M | Email/SMS verification & identity service. |



---

<!-- 03c-Server-Server-API-Coverage.md -->

# Server-Server (Federation) API Coverage (Template)

| Endpoint / Query | Method | Spec | Synapse | Dendrite | Complexity | Notes |
|------------------|--------|------|---------|----------|------------|------|
| /_matrix/federation/v2/send | PUT | v2 | Yes | Yes | M | Outgoing PDU/EDU queues, backoff, retries. |
| /_matrix/federation/v1/backfill/{roomId} | GET | v1 | Yes | Yes | M | History fetch. |
| /_matrix/federation/v2/invite/{roomId}/{eventId} | PUT | v2 | Yes | Yes | M | Invite flow. |
| Restricted rooms (MSC3083) | — | MSC | Yes | **Partial** | M | Join rules; ensure full create/enforce. |
| Knock (MSC3266) | — | MSC | Yes | **Missing** | M | Knock lifecycle, auth. |



---

<!-- 03d-Application-Service-API.md -->

# Application Service API Differences

| Area | Synapse | Dendrite | Impact | Complexity | Notes |
|------|---------|----------|--------|------------|------|
| Registration & rate limits | Rich controls | Basic | Medium | S | Add per-AS limits & quotas. |
| E2EE device handling | Mature | Partial | High | M | Better to-device and device list sync for bridges. |
| Transaction reliability | Mature | Needs polish | Medium | M | Retries, idempotency, metrics. |



---

<!-- 03e-Push-Gateway-API.md -->

# Push Gateway API Gaps

| Area | Synapse | Dendrite | Impact | Complexity | Notes |
|------|---------|----------|--------|------------|------|
| Push rules (incl. mentions/threads) | Full | Partial | High | M | Align with MSC3952 & threads. |
| HTTP push gateway | Yes | Partial | Medium | M | Delivery, retries, per-device throttling. |



---

<!-- 03f-Identity-Service-Integration.md -->

# Identity Service Integration Differences

| Flow | Synapse | Dendrite | Impact | Complexity | Notes |
|------|---------|----------|--------|------------|------|
| 3PID registration (email/phone) | Yes | Partial/No | High | M | Token-based validation. |
| Password reset via email | Yes | No | High | S | SMTP + token flows. |
| 3PID invites / lookup | Yes | Partial/No | Medium | M | Hashing, lookup endpoints. |



---

<!-- 04-Core-Features.md -->

# 2. Core Feature Comparison

## Solid in Dendrite
- Core messaging, membership, profiles; read receipts, typing, presence (opt-in)
- E2EE basics (device lists, one-time keys, to-device)
- Search (Bleve), user directory, public room directory
- Media upload/download & thumbnails

## Threads (clarified)
- **Works today:** Event relations for threads (MSC2836) allow sending/receiving threaded messages (behind flag).
- **Missing:** Thread-aware `/sync` filters, unread/notification counts, thread-specific receipts, push rules.
- **Where:** Relations handled in `roomserver/`; filtering/counts in `syncapi/` need work.

## Other gaps
- **URL Previews (`/preview_url`)** — implement fetch/parser/cache with SSRF protections.
- **Cross-signing reliability & Key Backup** — improve device list propagation; add server-side backup.
- **3PID flows** — email/phone verification, invites, discovery.
- **Admin & Moderation** — broaden admin endpoints; rate limits & spam hooks.



---

<!-- 05-Matrix-2.0.md -->

# 3. Matrix 2.0 Features (Sliding Sync, OIDC, Mentions)

## Sliding Sync (MSC4186)
- Supersedes `/sync` v2 for low-latency startup and incremental updates.
- Requires per-device list/window state, room ordering, and delta ops.

**Implementation sketch (`syncapi/`):**
- New handler for sliding sync; persistent list/window state per device.
- In-memory indexes with periodic persistence; reuse stream positions.
- Validate with Element X; feature-flag rollout.

## OIDC / MAS (MSC3861 family)
- Web-based auth, refresh tokens, SSO across IdPs.

**Implementation sketch (`userapi/`):**
- OIDC login/registration, token & refresh handling, account linking.
- MAS compatibility; multiple providers; migration from password auth.

## Mentions & Threads (MSC3952 + MSC3874 + MSC3440)
- Add intentional mentions flags; push rules; thread-only filters and counts.
- Lives in `clientapi/` (event flags) and `syncapi/` (filters, notifications).



---

<!-- 05a-Client-Compatibility-Matrix.md -->

# Client Compatibility Matrix (Indicative)

> Purpose: clarify current UX and feature impacts per client. Update as features land.

| Client | Works Today | Degraded / Missing | Notes |
|--------|-------------|--------------------|-------|
| Element X | **Limited** | Needs Sliding Sync, full Threads, Mentions | Without MSC4186, startup/scroll perf suffers. |
| Element Web/Desktop | **Mostly** | Thread counts, some push rules | Legacy `/sync` works; threads incomplete UX. |
| Element Android/iOS | **Mostly** | Registration quirks; threads | Push gateway OK; sliding sync would help perf. |
| Hydrogen | **Mostly** | Occasional sync quirks | Monitor since-tokens & filters. |
| Nheko/Quaternion | **Mostly** | Advanced MSCs | Core messaging fine; fewer 2.0 features needed. |



---

<!-- 06-Performance-Scalability.md -->

# 4. Performance & Scalability (Expanded)

## Caching
- Dendrite uses an internal cache (Ristretto-based) under `internal/caching/` to reduce DB load for hot paths (state snapshots, membership, event metadata). Tune sizes per workload.

## Known Bottlenecks
- **State resolution (roomserver):** Expensive in large/complicated rooms; optimize queries, add memoization.
- **DB contention:** SQLite exhibits `database is locked` under write concurrency; prefer PostgreSQL for anything beyond small labs.
- **JetStream overhead:** NATS JetStream persistence adds latency at very high event rates; tune stream retention and batching.
- **Sync fan-out:** High fan-out rooms stress `/sync`; Sliding Sync mitigates by selective windows.

## Practical Guidance
- **“Small server” (rule-of-thumb):** up to ~200–500 MAU and ~1–2k rooms on a modest VM (2–4 vCPU, 4–8 GB RAM) with PostgreSQL. Validate in staging; adjust caches.
- Use monolith mode for simplicity; scale by adding more `syncapi` and `federationapi` instances behind LB as needed (future multi-process hardening required).
- Add dashboards: DB latency, JetStream queue depths, federation retry/backoff, sync lag.

## Next Optimizations
- Targeted caches (current state, device data, recent events)
- Background maintenance (extremity pruning; see Phase 3)
- Parallelize federation sender & backfill; bounded queues with metrics



---

<!-- 07-Database-Schema-Migrations.md -->

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



---

<!-- 08-Federation-Compatibility.md -->

# 6. Federation & Protocol Compatibility (Strengthened)

## Strengths
- **Reliable queues & retries:** `federationapi/queue/` implements per-destination queues, backoff, and blacklisting to avoid hot loops.
- **Server statistics:** `federationapi/statistics/` tracks success/failure/latency per remote, improving operability.
- **Auth & signatures:** Full event auth checks and key verification; solid interop in mainstream rooms.

## Gaps & Next
- **MSC3083 (Restricted rooms):** Partial — ensure full create/enforce and comprehensive tests.
- **MSC3266 (Knock):** Missing — implement knock membership lifecycle and federation endpoints.
- **Space summaries & misc:** Nice-to-have parity items.

## Recommendations
- Expand Complement/Sytest scenarios for outage recovery, large fan-out rooms.
- Improve observability: queue depth, retry counters, failure reasons.



---

<!-- 09-Config-Deployment.md -->

# 7. Configuration & Deployment

- Single YAML; straightforward monolith; Prometheus metrics; Docker images.
- Add knobs: rate limits, cache sizes, spam checker hooks.
- Expose OIDC and 3PID/email config blocks.
- Expand metrics: sync/federation backlogs, DB timings, JetStream stats.
- Provide deployment recipes (docker-compose, systemd, reverse proxy).
- Document active-passive failover; backups for DB, media, Bleve index, keys.



---

<!-- 09a-Admin-APIs.md -->

# 7a. Admin & Moderation APIs (Specifics)

## Exists (Unstable; selection)
- **User management:** create/reset via shared-secret auth; *whois* lookup.
- **Evacuation:** user evacuation (leave all rooms), room evacuation (evict all local users).
- **Room purge:** purge room from DB (dangerous; admin-only).
- **Server notices:** send notice to a user.
- **Device maintenance:** refresh device lists; trigger search indexing.

## Missing vs Synapse (Examples)
- Deactivate/delete user (lifecycle & GDPR workflows)
- List/search users; edit attributes (3PID management)
- Room admin: targeted purge history, ban/kick via admin, remote media purge/quarantine
- Shadow-banning / spam modules integration
- Quotas & rate-limit inspection

## Recommendations
- Prioritize **deactivate user**, **list users**, **room history purge**, **media quarantine** as small, high-impact endpoints.



---

<!-- 10-Testing-QA.md -->

# 8. Testing & QA (Expanded)

## Automated Testing
- **Unit tests:** Broad coverage across components; target ≥70% on new/changed code.
- **Race detector:** Run `test-race` in CI to catch concurrency issues.
- **Integration:** Go-based **Complement** suite; plus **Sytest** compatibility where applicable.
- **Infra:** `test/testrig/` spins up in-process servers for end-to-end tests.
- **Coverage tracking:** Codecov (or equivalent) enforces/report trends.

## Status (fill with current numbers)
- Sytest pass rate: **TBD** (track per release)
- Complement pass rate: **TBD** (track per release)

## Next
- Load/perf harness: target N msgs/s and p95 latencies; record CPU/mem.
- E2EE robustness: cross-signing flows, key backup restore tests.
- Fuzzing for federation inputs (malformed PDUs/EDUs).
- CI matrix for PostgreSQL and SQLite.



---

<!-- 11-Architecture.md -->

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



---

<!-- 12-Roadmap.md -->

# Implementation Roadmap (Revised)

## Phase 1a — Critical UX Blockers (0–4 months; parallelizable)
- **Sliding Sync (MSC4186)** — **XL+** (multi-quarter if 1 engineer). New storage/indexes, lists/windows, ops, ordering.
- **Threads (MSC3440 full)** — **L**. Sync filters, unread/notification counts, push rule integration.

## Phase 1b — Auth & Security (starts by month 2; 2–4 months)
- **OIDC / MAS (MSC3861)** — **L/XL**. OIDC flows, tokens/refresh, MAS compat.
- **Key Backup (MSC1212)** — **M/L**. Backup versions, encrypted blobs, restore flows.
- **3PID flows** — **M**. Email/SMS verification, invites, discovery.

## Phase 2 — Operability & Bridges (3–6 months)
- **Admin & Moderation APIs** — **S/M** each (deactivate user, list users, purge history, media quarantine).
- **Rate limiting & basic spam hooks** — **S/M**.
- **Appservice/E2EE polish** — **M**.
- **URL Previews** — **M** (security, caching, thumbnails).
- **Metrics expansion & dashboards** — **S**.

## Phase 3 — Scale & Hardening (ongoing; 6–12+ months)
- **Background maintenance jobs** (extremity pruning, media retention) — **S/M**.
- **Selective caching & DB tuning** — **M**.
- **Multi-process hardening (sync/fed)** — **XL** (6–12+ months).
- **Perf & fuzz test suites** — **S**.

## Dependencies
- Sliding Sync ↔ Threads (thread-aware filters enhance sliding sync value).
- OIDC ↔ 3PID (account flows & recovery interplay).
- Admin APIs before large-scale ops (Phase 3) to manage deployments.
- Cross-signing reliability prior to Key Backup GA.

> T-shirt sizes: S≈days, M≈weeks, L≈1–2 months, XL≈3–4 months, **XL+** multi-quarter. Sizes assume 1 experienced engineer; parallelize where possible.



---

<!-- 12a-Quick-Wins.md -->

# Quick Wins (S-sized or small M)

- Add **list users** and **deactivate user** admin endpoints.
- Expose **rate-limiting** knobs; basic spam checker hook.
- Implement **URL preview** MVP with allow-list and size/time limits (upgrade later).
- Expand **Prometheus metrics** (sync lag, federation retries, JetStream subjects).
- Fix minor **E2EE device list** propagation issues.



---

<!-- 13-Migration-From-Synapse.md -->

# Synapse → Dendrite Migration (Considerations)

## Current Status
- No official direct migration path. Dendrite DB schema is **not** compatible with Synapse.

## Challenges
- **Data:** Different schemas; events/state cannot be directly imported.
- **E2EE:** Devices & cross-signing keys must be re-established or carefully migrated.
- **Media:** Copy media store separately; URLs and hashes must match if reusing.
- **Continuity:** Keep the same `server_name` and **signing keys** to maintain identity in federation.

## Suggested Approach
- Run Dendrite in parallel for evaluation.
- If attempting a move, stand up Dendrite with the same domain and signing keys; rejoin rooms via federation (expect history gaps).
- Prepare users for re-login, new device verification, and possible history limitations.

## Future
- Plan for a migration utility or guide post-parity; define minimal viable migration (accounts, rooms, media pointers).
