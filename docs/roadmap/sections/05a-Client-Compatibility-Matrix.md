# Client Compatibility Matrix (Indicative)

> Purpose: clarify current UX and feature impacts per client. Update as features land.

| Client | Works Today | Degraded / Missing | Notes |
|--------|-------------|--------------------|-------|
| Element X | **Limited** | Needs Sliding Sync, full Threads, Mentions | Without MSC4186, startup/scroll perf suffers. |
| Element Web/Desktop | **Mostly** | Thread counts, some push rules | Legacy `/sync` works; threads incomplete UX. |
| Element Android/iOS | **Mostly** | Registration quirks; threads | Push gateway OK; sliding sync would help perf. |
| Hydrogen | **Mostly** | Occasional sync quirks | Monitor since-tokens & filters. |
| Nheko/Quaternion | **Mostly** | Advanced MSCs | Core messaging fine; fewer 2.0 features needed. |
