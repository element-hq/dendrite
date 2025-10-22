# Push Gateway API Gaps

| Area | Synapse | Dendrite | Impact | Complexity | Notes |
|------|---------|----------|--------|------------|------|
| Push rules (incl. mentions/threads) | Full | Partial | High | M | Align with MSC3952 & threads. |
| HTTP push gateway | Yes | Partial | Medium | M | Delivery, retries, per-device throttling. |
