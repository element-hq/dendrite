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
