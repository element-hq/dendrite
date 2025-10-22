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
