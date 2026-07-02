# agentforest

Your repositories, growing as a forest in your terminal.

*(screenshot coming soon)*

## What this is

`agentforest` is a personal, terminal-native keepsake.
Every git repository you tend grows as a town in one continuous stretch of wild forest, drawn entirely in character art.
You roam it left and right the way you would walk through real woods, and it quietly reflects your real coding history.
Opening it should feel like visiting a place you made.

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
Roots are scanned recursively; every git repository found becomes a town, and then the forest is yours.
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
agentforest connect <dir>    connect a root and scan it
agentforest towns            list every town
agentforest refresh          rescan all connected roots
agentforest exclude <name>   hide a town (history kept)
agentforest include <name>   restore a hidden town
```

Output is structured, errors carry a help line, and every command answers `--help`.

## Where it lives

Everything sits in `~/.config/agentforest` (or `$AGENTFOREST_HOME`) as plain files you can read:
`settings.json` holds your roots, excludes, and finished towns; `events.jsonl` is the append-only history the forest grows from.
Repositories that vanish from disk keep their towns; ruins never disappear.

## Snapshots and reference sheets

For scripts and screenshots:

```
agentforest --snapshot --plain --width 170 --height 40 --at winterwell
agentforest --gallery species
agentforest --gallery decay
```

Snapshots accept `--seed n`, `--width n`, `--height n`, `--at name`, `--t sec`, and `--plain`.
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
