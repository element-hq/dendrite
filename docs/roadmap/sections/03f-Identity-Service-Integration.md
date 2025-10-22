# Identity Service Integration Differences

| Flow | Synapse | Dendrite | Impact | Complexity | Notes |
|------|---------|----------|--------|------------|------|
| 3PID registration (email/phone) | Yes | Partial/No | High | M | Token-based validation. |
| Password reset via email | Yes | No | High | S | SMTP + token flows. |
| 3PID invites / lookup | Yes | Partial/No | Medium | M | Hashing, lookup endpoints. |
