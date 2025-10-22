# Client-Server API Endpoint Coverage (Template)

List priority endpoints and their parity; use this as a working matrix during implementation.

| Endpoint | Method | Spec | Synapse | Dendrite | Complexity | Notes |
|----------|--------|------|---------|----------|------------|------|
| /_matrix/client/v3/sync | GET | v3 | Yes | Yes | L | Legacy sync; superseded by sliding sync for modern clients. |
| /_matrix/client/unstable/org.matrix.msc4186/sync | GET | MSC4186 | Yes | **No** | XL+ | Sliding Sync; requires new storage/indexing. |
| /_matrix/client/v3/preview_url | GET | v3 | Yes | **No** | M | Fetch+parse OG, SSRF protections, caching, thumbnails. |
| /_matrix/client/v3/login (OIDC) | POST | v3 | Yes | **Partial/No** | L | MAS / OIDC integration; tokens/refresh. |
| /_matrix/client/v3/register (3PID) | POST | v3 | Yes | **Partial/No** | M | Email/SMS verification & identity service. |
