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
