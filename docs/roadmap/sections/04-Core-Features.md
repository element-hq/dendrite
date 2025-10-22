# 2. Core Feature Comparison

## Solid in Dendrite
- Core messaging, membership, profiles; read receipts, typing, presence (opt-in)
- E2EE basics (device lists, one-time keys, to-device)
- Search (Bleve), user directory, public room directory
- Media upload/download & thumbnails

## Threads (clarified)
- **Works today:** Event relations for threads (MSC2836) allow sending/receiving threaded messages (behind flag).
- **Missing:** Thread-aware `/sync` filters, unread/notification counts, thread-specific receipts, push rules.
- **Where:** Relations handled in `roomserver/`; filtering/counts in `syncapi/` need work.

## Other gaps
- **URL Previews (`/preview_url`)** — implement fetch/parser/cache with SSRF protections.
- **Cross-signing reliability & Key Backup** — improve device list propagation; add server-side backup.
- **3PID flows** — email/phone verification, invites, discovery.
- **Admin & Moderation** — broaden admin endpoints; rate limits & spam hooks.
