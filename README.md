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
The current build opens a demo forest of twelve invented repositories, so the world is alive from the first launch.
Future builds will add first-run repo connection, persistence, and live updates.

## Keys

- `← →` or `h l` wander the forest; hold shift to stride.
- `tab` / `n` and `shift+tab` / `p` walk town to town; `g` oldest, `G` newest.
- `enter` or `i` inspect the focused town; numbers live here only.
- `f` mark a town finished; it freezes as a monument.
- `d` open the groundskeeper's almanac and preview years of neglect in seconds.
- In the almanac, `+` / `-` shift by day, `<` / `>` by month, `[` / `]` by year, `1`-`6` jump to stages, and `0` restores real time.
- `?` help, `esc` dismisses overlays, `q` quits from the forest.

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
The demo forest is built from generated data.
Real git scanning, persistence, and live updates are next.
