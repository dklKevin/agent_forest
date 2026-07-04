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
While a repo is being worked, the dooryard reads alive without numbers: a fresh day throws a tall dense smoke plume, split chips, lamplight dots, and nearly unbroken paths; through the week those traces thin; after that a quiet-but-kept hearth holds only a banked thread and a set-down axe.
Leave it longer, and the traces scatter, the roof opens, the walls fall, until only the stone chimney stands.
The chimney always stands.

A repo with real structure grows past a homestead into a settlement.
Its major directories stand as buildings around the hearth, chosen by file spread as well as byte weight so one oversized generated artifact cannot swallow the rest of a working tree.
The dominant component is the barn, the tests keep a watchtower, the docs keep a schoolhouse, and the rest take cabins, workshops, and sheds by their weight.
Every building lives and dies on its own component's clock.
Work the engine daily while the docs doze, and the barn can bustle with open doors, hay at the threshold, and a busy path while the schoolhouse sits quiet.
Leave the docs long enough and that same schoolhouse will be breaking apart while other buildings still show recent work.
A directory deleted from the repo keeps its building forever, falling slowly into the settlement's haunted quarter.
Worn footpaths and split-rail fence fragments tie the yards together, tended ground trims the understory near kept yards, and the old growth stands behind the rooflines.
The inspect panel names each building's component and stage; the forest itself stays numberless.

## The rules that make it itself

- **Shape carries meaning, never color.**
  Age, language, size, recent tending, and neglect are read from silhouette, height, density, and decay.
  The world is greyscale on near-black, with a single warm accent reserved for focal points.
- **A wild forest, never a grid.**
  Irregular spacing, rolling treelines, winding undergrowth.
  If it starts to look like a bar chart, it is wrong.
- **The honesty layer.**
  A town you have stopped tending is slowly, beautifully reclaimed by the forest.
  A commit revives it through smoke, paths, lamplight, and work left out.
  Laying it to rest turns it into a monument, with a short epitaph carved if you leave one.
  A long-dead town decays to ruins, but it never disappears.
- **Numberless by default.**
  Stats exist, but only when you deliberately inspect a town.

## The guidebook

Every town can explain itself.
Press `b` on a focused town, from the forest or while inspecting, to open its guidebook: a quiet page where the place says what it is.
The first words of the repository's own README lead the page, followed by the landmarks in plain words (what the town is built of, which buildings stand), the present state (planted, last tended, work underway off the default branch, a monument's standing), and which pages it keeps: readme, license, docs.
Everything on the page is read from files already sitting in the repository, on your machine.
Nothing is fetched, asked, or sent anywhere.
A repository without a readable README still gets a page; it just says there are no guidebook pages yet.

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
On a later real launch, towns with commits that landed while you were away may briefly stir with the same revival motion, capped to a handful of notable towns; first runs and upgrades from older settings stay quiet.

`agentforest --demo` opens the demo forest of twelve invented repositories any time.

## Keys

- `← →` or `h l` wander the forest; hold shift to stride.
- `tab` / `n` and `shift+tab` / `p` walk town to town; `g` oldest, `G` newest.
- `enter` or `i` inspect the focused town; numbers live here only.
- `a` while inspecting opens the town's almanac: its memoir, folded from its history - planted, long quiets and wakings, releases staked, and, leading a finished town's page, its carved words.
- `b` opens the town's guidebook, from the forest or while inspecting: its own pages, read from local files alone.
- `f` lay a town to rest as a monument: a short ceremony plays, and you may carve one short line, up to 40 characters (read back in inspect and the almanac, never on the map).
  On a monument, `f` quietly lights the hearth again; the carved words are kept.
- `d` preview years of neglect in seconds.
- In the preview, `+` / `-` shift by day, `<` / `>` by month, `[` / `]` by year, `1`-`6` jump to stages, and `0` restores real time.
- `c` connect another root; `x` exclude the focused town; `r` rescan every root.
- `?` help, `esc` dismisses overlays, `q` quits from the forest.

## Commands

The same forest can be tended from scripts:

```
agentforest connect <dir>        connect a root and scan it
agentforest towns                list every visible town
agentforest almanac <name|path>  read a town's memoir
agentforest refresh              rescan all connected roots
agentforest exclude <name|path>  hide a town (history kept)
agentforest include <name|path>  restore a hidden town
agentforest finish <name|path> ["a word to carve"]   lay a town to rest as a monument
agentforest unfinish <name|path> light the hearth again (carved words are kept)
```

Use a full path when duplicate town names collide.
An epitaph is optional, trimmed to one plain line of at most 40 characters; finishing a monument with new words re-carves it.
Output is structured, and command-specific errors include help when there is an obvious next step.
Every command answers `--help`.

## Where it lives

By default, everything sits in `~/.config/agentforest`; `$XDG_CONFIG_HOME` moves it to `$XDG_CONFIG_HOME/agentforest`, and `$AGENTFOREST_HOME` overrides both.
The storage is plain files you can read:
`settings.json` holds your roots, excludes, and last-opened stamp, plus legacy finished entries from older builds; `events.jsonl` is the append-only history the forest grows from, including every finish, unfinish, and carved word.
Repositories that vanish from disk keep their towns; ruins never disappear.

## Snapshots and reference sheets

For scripts and screenshots:

```
agentforest --snapshot --demo --plain --width 170 --height 40 --at winterwell
agentforest --gallery species
agentforest --gallery decay
agentforest --gallery homestead
agentforest --gallery settlement
```

Snapshots accept `--seed n`, `--demo`, `--width n`, `--height n`, `--at name`, `--t sec`, and `--plain`.
Without `--demo`, snapshots read the persisted forest and do not rescan; run `agentforest refresh` first for fresh data.
If `--at` is wrong, the error lists valid town names.
Galleries accept `--width`, `--height`, and `--plain`.
`--version` prints the binary version.

## Development

Use Go 1.26.x. The local gate is the same one CI runs:

```
make build
make vet
make test
```

Those wrap `go build ./...`, `go vet ./...`, and `go test ./...`.
The render/art layer is protected by plain UTF-8 golden frames under `internal/gallery/testdata` and `internal/render/testdata`.
After an intentional art change, run `make golden` (or `go test ./internal/gallery ./internal/render -update`) and review the regenerated `.golden` diff as the art review.

## Privacy

Everything stays on your machine.
No telemetry, no analytics, no social features, no leaderboards.
Shareable only because it is beautiful.

## Status

Pre-v1.
Real git scanning, persistence, and live updates are in.
macOS first (Terminal.app, iTerm2, Ghostty); other platforms may work but are not the target yet.
