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
