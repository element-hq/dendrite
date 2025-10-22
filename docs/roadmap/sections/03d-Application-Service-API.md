# Application Service API Differences

| Area | Synapse | Dendrite | Impact | Complexity | Notes |
|------|---------|----------|--------|------------|------|
| Registration & rate limits | Rich controls | Basic | Medium | S | Add per-AS limits & quotas. |
| E2EE device handling | Mature | Partial | High | M | Better to-device and device list sync for bridges. |
| Transaction reliability | Mature | Needs polish | Medium | M | Retries, idempotency, metrics. |
