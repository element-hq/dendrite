# 7. Configuration & Deployment

- Single YAML; straightforward monolith; Prometheus metrics; Docker images.
- Add knobs: rate limits, cache sizes, spam checker hooks.
- Expose OIDC and 3PID/email config blocks.
- Expand metrics: sync/federation backlogs, DB timings, JetStream stats.
- Provide deployment recipes (docker-compose, systemd, reverse proxy).
- Document active-passive failover; backups for DB, media, Bleve index, keys.
