# agentforest

Your repositories, growing as a forest in your terminal.

*(screenshot coming soon)*

## What this is

`agentforest` is a personal, terminal-native keepsake.
Every git repository you tend grows as a town in one continuous stretch of wild forest, drawn entirely in character art.
You roam it left and right the way you would walk through real woods, and it quietly reflects your real coding history.
Opening it should feel like visiting a place you made.

At the heart of every town stands one hand-hewn cabin, the repo's hearth, nestled in a gap barely wider than its walls.
The name board hangs off its eave, and the size of the homestead grows with the work done there.
While a repo is being worked, woodsmoke rises, the cord is stacked under the lean-to, and an axe waits in the chopping block.
Leave it, and the traces scatter, the roof opens, the walls fall, until only the stone chimney stands.
The chimney always stands.

## The rules that make it itself

- **Shape carries meaning, never color.**
  Age, language, size, and neglect are read from silhouette, height, density, and decay.
  The world is greyscale on near-black, with a single warm accent reserved for focal points.
- **A wild forest, never a grid.**
  Irregular spacing, rolling treelines, winding undergrowth.
  If it starts to look like a bar chart, it is wrong.
- **The honesty layer.**
  A town you have stopped tending is slowly, beautifully reclaimed by the forest.
  A commit revives it.
  Marking it finished freezes it as a monument.
  A long-dead town decays to ruins, but it never disappears.
- **Numberless by default.**
  Stats exist, but only when you deliberately inspect a town.

## Running it

```
go build -o agentforest . && ./agentforest
```

One self-contained binary, plain character art, no image protocols.
On first run it opens over a demo forest and walks you through connecting the folders where your repositories live.
Roots are scanned recursively, skipping hidden folders, `node_modules`, and repositories nested inside another repository.
Every repository with commits becomes a town, and empty repositories stay quiet until their first commit.
While the app is open, a new commit in any connected repository revives its town within seconds.
No daemon runs and nothing is watched when the app is closed; the next launch simply catches up.

`agentforest --demo` opens the demo forest of twelve invented repositories any time.

## Keys

- `← →` or `h l` wander the forest; hold shift to stride.
- `tab` / `n` and `shift+tab` / `p` walk town to town; `g` oldest, `G` newest.
- `enter` or `i` inspect the focused town; numbers live here only.
- `f` mark a town finished; it freezes as a monument.
- `d` open the groundskeeper's almanac and preview years of neglect in seconds.
- In the almanac, `+` / `-` shift by day, `<` / `>` by month, `[` / `]` by year, `1`-`6` jump to stages, and `0` restores real time.
- `c` connect another root; `x` exclude the focused town; `r` rescan every root.
- `?` help, `esc` dismisses overlays, `q` quits from the forest.

## Commands

The same forest can be tended from scripts:

```
agentforest connect <dir>        connect a root and scan it
agentforest towns                list every visible town
agentforest refresh              rescan all connected roots
agentforest exclude <name|path>  hide a town (history kept)
agentforest include <name|path>  restore a hidden town
```

Use a full path when duplicate town names collide.
Output is structured, and command-specific errors include help when there is an obvious next step.
Every command answers `--help`.

## Where it lives

By default, everything sits in `~/.config/agentforest`; `$XDG_CONFIG_HOME` moves it to `$XDG_CONFIG_HOME/agentforest`, and `$AGENTFOREST_HOME` overrides both.
The storage is plain files you can read:
`settings.json` holds your roots, excludes, and finished towns; `events.jsonl` is the append-only history the forest grows from.
Repositories that vanish from disk keep their towns; ruins never disappear.

## Snapshots and reference sheets

For scripts and screenshots:

```
agentforest --snapshot --demo --plain --width 170 --height 40 --at winterwell
agentforest --gallery species
agentforest --gallery decay
agentforest --gallery homestead
```

Snapshots accept `--seed n`, `--demo`, `--width n`, `--height n`, `--at name`, `--t sec`, and `--plain`.
Without `--demo`, snapshots read the persisted forest and do not rescan; run `agentforest refresh` first for fresh data.
If `--at` is wrong, the error lists valid town names.
Galleries accept `--width`, `--height`, and `--plain`.
`--version` prints the binary version.

## Privacy

Everything stays on your machine.
No telemetry, no analytics, no social features, no leaderboards.
Shareable only because it is beautiful.

## Status

Pre-v1.
Real git scanning, persistence, and live updates are in.
macOS first (Terminal.app, iTerm2, Ghostty); other platforms may work but are not the target yet.
