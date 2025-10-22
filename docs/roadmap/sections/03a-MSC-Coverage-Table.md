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
