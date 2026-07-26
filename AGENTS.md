# Project agent memory

This file is the project's committed home for project-intrinsic agent knowledge: build, test, release, architecture, and sharp-edge notes that should travel with the code.

- Add durable project-specific notes here as they are discovered through real work.
- Local run evidence is authority-tiered: fully scan `.agentforest` first and
  fail closed on uncertainty there; consult `.gnhf` only after a clean
  authoritative scan yields no valid causal observation. Once `.agentforest`
  has a valid candidate, lower-tier compatibility state must not be traversed
  or allowed to veto its equal-maximum-time consensus.
