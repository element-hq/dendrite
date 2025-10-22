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
