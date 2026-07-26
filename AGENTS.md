# Project agent memory

This file is the project's committed home for project-intrinsic agent knowledge: build, test, release, architecture, and sharp-edge notes that should travel with the code.

- Add durable project-specific notes here as they are discovered through real work.
- The local run evidence contract is documented in README.md under
  "Local work plaques."
- Local-run polling stays metadata-only but must hash stable portable identity
  for the active evidence root and evidence files; identity uncertainty publishes
  no cursor and no presence.
- Preserve every observation at a run's maximum causal time through privacy
  filtering, then require complete Presence consensus before choosing a winner.
- A failed git/event scan may publish only negative run state so removed or
  uncertain evidence cannot leave a stale public plaque; it must not advance the
  polling cursor.
