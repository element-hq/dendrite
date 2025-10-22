# 7a. Admin & Moderation APIs (Specifics)

## Exists (Unstable; selection)
- **User management:** create/reset via shared-secret auth; *whois* lookup.
- **Evacuation:** user evacuation (leave all rooms), room evacuation (evict all local users).
- **Room purge:** purge room from DB (dangerous; admin-only).
- **Server notices:** send notice to a user.
- **Device maintenance:** refresh device lists; trigger search indexing.

## Missing vs Synapse (Examples)
- Deactivate/delete user (lifecycle & GDPR workflows)
- List/search users; edit attributes (3PID management)
- Room admin: targeted purge history, ban/kick via admin, remote media purge/quarantine
- Shadow-banning / spam modules integration
- Quotas & rate-limit inspection

## Recommendations
- Prioritize **deactivate user**, **list users**, **room history purge**, **media quarantine** as small, high-impact endpoints.
