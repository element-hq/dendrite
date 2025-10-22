# Server-Server (Federation) API Coverage (Template)

| Endpoint / Query | Method | Spec | Synapse | Dendrite | Complexity | Notes |
|------------------|--------|------|---------|----------|------------|------|
| /_matrix/federation/v2/send | PUT | v2 | Yes | Yes | M | Outgoing PDU/EDU queues, backoff, retries. |
| /_matrix/federation/v1/backfill/{roomId} | GET | v1 | Yes | Yes | M | History fetch. |
| /_matrix/federation/v2/invite/{roomId}/{eventId} | PUT | v2 | Yes | Yes | M | Invite flow. |
| Restricted rooms (MSC3083) | — | MSC | Yes | **Partial** | M | Join rules; ensure full create/enforce. |
| Knock (MSC3266) | — | MSC | Yes | **Missing** | M | Knock lifecycle, auth. |
