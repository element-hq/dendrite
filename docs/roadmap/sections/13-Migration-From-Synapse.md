# Synapse → Dendrite Migration (Considerations)

## Current Status
- No official direct migration path. Dendrite DB schema is **not** compatible with Synapse.

## Challenges
- **Data:** Different schemas; events/state cannot be directly imported.
- **E2EE:** Devices & cross-signing keys must be re-established or carefully migrated.
- **Media:** Copy media store separately; URLs and hashes must match if reusing.
- **Continuity:** Keep the same `server_name` and **signing keys** to maintain identity in federation.

## Suggested Approach
- Run Dendrite in parallel for evaluation.
- If attempting a move, stand up Dendrite with the same domain and signing keys; rejoin rooms via federation (expect history gaps).
- Prepare users for re-login, new device verification, and possible history limitations.

## Future
- Plan for a migration utility or guide post-parity; define minimal viable migration (accounts, rooms, media pointers).
