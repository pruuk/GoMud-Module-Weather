# GoMud Weather Module

A weather system for [GoMud](https://github.com/GoMudEngine/GoMud) worlds.
Weather forms as discrete, named **systems (fronts)** that move across a graph
of your world's geography, gather or lose strength based on the **terrain they
cross**, and express themselves through GoMud's existing room **mutators** —
room names and descriptions, alerts, light, ambient emotes, and curated,
overridable buffs.

> A storm forms over the coast, rolls inland across the plains gathering
> strength, climbs into the mountains where the terrain bleeds it dry, and
> dissipates on the far side — and players in each zone along the way feel it
> arrive, pass, and leave.

Built in the same spirit as the
[GoMud Module Playtest Harness](https://github.com/GoMudEngine/GoMud-Module-Playtest-Harness):
engine-native, compiled-in, data-driven, and testable in isolation.

**Status: M3 complete.** Weather works end-to-end on a stock GoMud world:
install, run, and storms travel, rooms render `(storm-wracked)`, ambient lines
play indoors and out, and state survives reboots. Remaining before a public
release: M4 (default content for every stock biome, polish, builder guide) and
the one-time module-registry listing. The
[design spec](docs/superpowers/specs/2026-06-08-weather-module-design.md)
remains the source of truth for scope and architecture; dated status notes in
it record exactly what each milestone shipped.

---

## What this module is

- **A weather simulation at zone granularity.** Every zone has exactly one
  current weather type (`clear`, `overcast`, `rain`, `storm`, `fog`, `snow`,
  `blizzard`, `dust`, `heatwave` out of the box — the set is open data, not a
  hardcoded enum). Fronts travel zone-to-zone along exits your world already
  has.
- **Biome-aware, in both directions.** A zone's biome decides which weather can
  form there and how likely it is (deserts birth dust, not blizzards), and the
  terrain a front crosses feeds or saps it (oceans feed storms; mountains
  bleed them dry, so systems die crossing a range).
- **Deterministic and persistent.** The simulation core is a pure function over
  a seeded RNG: the same seed and world replay the same weather, and active
  fronts + RNG state are saved across reboots. Great for debugging and tests.
- **Data-driven presentation.** The engine owns weather *state*; your world
  owns its *voice*. Everything players read lives in YAML you can override:
  mutator specs (room name/description/alert/light/buffs per weather type) and
  emote tables keyed by weather × biome × indoor/outdoor.
- **Zero engine changes.** The module compiles in against existing GoMud APIs
  (mutators, events, gametime, plugin storage, plugin data files). Nothing in
  `internal/` is patched.

## What this module is NOT

- **Not per-room weather.** Simulation granularity is the zone. Indoor rooms
  are not rained on — they get indoor *presentation* ("rain drums on the roof")
  — but two outdoor rooms in one zone always share weather.
- **Not seasons.** Seasons are designed-for (the data formats and clock leave
  seams) but deliberately deferred to v2.
- **Not a wind/pressure/temperature simulation.** No vector fields, no
  thermodynamics. Weather types carry coarse implications (a blizzard is cold)
  through the buffs and prose you configure.
- **Not a prose author.** We ship sensible default text so it works out of the
  box, but the defaults are meant to be replaced with your world's voice.
- **Not a drop-in plugin for a running server.** GoMud modules are *compiled
  into* the server binary. Installing means adding source and rebuilding —
  see Installation.
- **Not client-side rendering / GMCP.** A weather GMCP package is a listed
  future enhancement, not part of v1.

## Requirements

- **Go 1.25+** ([go.dev/dl](https://go.dev/dl/)). You don't need to know Go to
  *use* the module, but you need the toolchain to build GoMud at all.
- **A current upstream GoMud checkout.** The module binds to engine features
  that exist on upstream `master` as of mid-2026, most importantly
  **plugin-filesystem data loading for mutators** (the engine wires
  `mutators.RegisterFS(plugins.GetPluginRegistry())` in `main.go`). If your
  engine predates that, the module's weather mutators never load and every
  weather change logs a "no mutator spec loaded" warning — the server stays
  healthy, but rooms won't render weather.
- The stock-world content it reuses by default: buff ids **31** (Freezing
  Snow) and **33** (Thirsty), and the stock color patterns `gray`, `blue`,
  `mute-dblue`, `frost`, `brown`, `embers`. Missing any of these degrades
  gracefully (see *What can break it*).

## Installation

These steps assume you've never built GoMud before.

1. **Install Go** from [go.dev/dl](https://go.dev/dl/) and confirm it works:

   ```sh
   go version
   ```

2. **Get GoMud:**

   ```sh
   git clone https://github.com/GoMudEngine/GoMud.git
   cd GoMud
   ```

3. **Add the module.** Once the module is listed in the GoMud registry this
   will be one command (`go run . module install weather`). Until then, copy
   this repository's contents into the checkout at `modules/weather/`,
   **excluding `go.mod` and `go.sum`** (in-checkout modules have no module
   file of their own — they build as part of the engine). From this repo:

   ```powershell
   pwsh scripts/sync-to-checkout.ps1 -Checkout <path-to-your-GoMud-checkout>
   ```

   (or copy by hand; also skip `docs/` and `scripts/` — only the Go packages
   and `files/` matter at runtime).

4. **Register modules and build.** From the GoMud checkout root:

   ```sh
   go generate ./...   # regenerates modules/all-modules.go to include weather
   go build
   ```

   `go generate` is the step people forget: modules are wired in by a
   generated import file, so without it the module silently isn't in the
   binary.

5. **Run the server:**

   ```sh
   go run .            # or run the binary you just built
   ```

   Connect with any telnet/MUD client to the port in `_datafiles/config.yaml`
   (stock default: 33333).

That's the whole install. Weather is **enabled by default**
(`Modules.weather.Enabled: true` ships in the module's config overlay): on the
first game round the module crawls your world's zones and exits into a
geography graph, caches it, seeds the simulation from your zone names (stable
per world), and starts ticking once per game hour. No data authoring, no room
tagging, no world prep.

### What you'll see in the boot log

```
mutators.LoadDataFiles()  loadedCount=18        ← stock 10 + our 8 weather specs
Weather: built geography graph  zones=15 edges=10 components=6
Weather: fresh simulation state  seed=17214436859030717895
```

On later boots: `loaded geography cache` and `restored simulation state
fronts=N` instead.

## Using it in game

Any player:

| Command | Output |
|---|---|
| `weather` | `The weather in Frostfang is clear.` — plus the dominant front and felt intensity when a system covers your zone. |

Admins (and mods granted the `weather` permission key):

| Command | What it does |
|---|---|
| `weather status` | Graph summary, active front count, next tick round, emote/buff/persist settings. |
| `weather zones` | Every zone and its current weather. |
| `weather fronts` | Active systems: id, type, center zone, intensity, moisture, age. |
| `weather spawn <type> <zone> [intensity]` | Force a front (e.g. `weather spawn storm Frostfang 0.9`). Zone names may contain spaces; intensity is an optional trailing number 0..1. |
| `weather clear [zone]` | Remove all fronts, or every front whose coverage reaches the named zone. |
| `weather graph [zone]` | A zone's graph neighbors and border weights (crawler spot-check). |
| `weather rebuild` | Re-crawl the world and rewrite the graph cache (run after adding zones/exits). |

Weather shows up without anyone running commands, of course: room names get a
tag like `(raining)`, descriptions gain a weather line, severe weather adds an
alert banner and dims light, and occupied rooms hear ambient lines every ~20
rounds (indoor rooms get indoor variants).

## How it works (one paragraph)

A **crawler** walks every room exit once at boot and reduces your world to a
zone-adjacency graph (zone = node, "rooms in A have exits into B" = weighted
edge). A pure, seeded **simulation** ticks once per game hour: fronts age,
terrain feeds or saps them, they move along edges (wide borders are likelier),
their type drifts toward what the local climate supports, dead ones are
removed, new ones spawn within a budget, and every zone resolves to one
weather type (strong fronts project onto neighboring zones, so a big storm
covers an area, not a point). The **engine adapter** then makes the world
match: each zone's `ZoneConfig.Mutators` gets exactly the right `weather-*`
mutator (the engine merges zone mutators into every room render), and an
emote scheduler voices occupied rooms. State is saved through plugin storage
and reconciled on boot.

## Configuration

All knobs live under `Modules.weather.*`. Defaults ship in this module's
`files/data-overlays/config.yaml` — to change them, edit that file and
rebuild. **Gotcha (inherited from the engine's overlay mechanics):** a
`Modules.weather:` block in your server's `config-overrides.yaml` will NOT
merge; module config comes from module overlays.

| Key | Default | Meaning |
|---|---|---|
| `Enabled` | `true` | Master switch. Off = module registers nothing but an inert command. |
| `Seed` | `0` | RNG seed. `0` derives a stable seed from your zone names (same world ⇒ same seed; negative values are treated as 0). |
| `TickEveryGameHours` | `1` | Simulation cadence in game hours (minimum 1). |
| `MaxActiveFronts` | `8` | Global front budget (minimum 1). |
| `SpawnRateScale` | `1.0` | Multiplier on spawn pressure. `0` stops new fronts entirely. |
| `EmoteMode` | `module` | `module` = we emit ambient lines; `tag-only` = we stay silent and your room scripts react to the weather mutators/alerts instead. |
| `EmoteEveryRounds` | `20` | Ambient emote cadence in rounds, jittered ±25% (minimum 5). |
| `BuffsEnabled` | `true` | Apply the curated default buffs carried by weather mutators (blizzard → 31 Freezing Snow, heatwave → 33 Thirsty). `false` strips buff ids from the weather specs at boot. |
| `Persist` | `true` | Save/restore fronts + RNG across reboots. |
| `IncludeSecretExits` | `true` | Crawler counts secret/locked exits as zone adjacency (weather doesn't care about locks). |
| `RebuildGraphOnBoot` | `false` | Force a fresh crawl each boot instead of using the cache. |

Planned but not yet config keys (deferred to M4+): `PrevailingWind`,
`PerRoomRefinement`, `Buffs.Overrides`, `ExcludeZonePatterns` (the crawler
currently always skips `instance_*`/`ephemeral_*` zones).

## Customizing the content

Everything a builder would want to change is YAML under `files/datafiles/`,
rebuilt into the binary. No Go required.

- **Prose & ambiance** — `files/datafiles/emotes/<type>.yaml`. Lines are keyed
  by biome with a `default` fallback, split `outdoor:` / `indoor:` (indoor
  never falls back to outdoor — silence beats "rain falls around you" inside a
  tavern). Add a biome key to give, say, forests their own storm lines. Prefer
  total control? Set `EmoteMode: tag-only` and react to the weather mutators
  from your own room scripts.
- **Room rendering & mechanics** — `files/datafiles/mutators/weather_<type>.yaml`,
  standard engine `MutatorSpec` schema: name tag, description line, alert,
  `lightmod`, buff ids. Two rules our validation tests enforce, learned the
  hard way against the live engine: weather specs must never set
  `respawnrate` (it would fight the orchestrator and prevent cleanup) and
  never set `decayintoid` (the engine's `Remove` instantly resurrects the
  decay target — see *What can break it*). `decayrate` stays: it's the
  self-heal safety net if the module is disabled mid-storm.
- **Climate** — drop `files/datafiles/climate/<biome>.yaml` to override a
  biome's weather weights, terrain influence, and spawn pressure (schema in
  the design spec §7.3). Biomes without a file use built-in defaults for the
  standard biomes (plains/forest/mountain/desert/tundra/swamp/ocean) plus a
  mild `default` profile for everything else. Note: an override replaces the
  biome's profile wholesale — omitted fields become zero, including
  `spawnWeight`.
- **A new weather type** is just data: reference it in a climate file, add
  `mutators/weather_<type>.yaml` and `emotes/<type>.yaml`. The filename must
  be the mutator id lowercased with non-alphanumerics as `_` (engine loader
  rule): id `weather-acid-rain` ⇒ file `weather_acid_rain.yaml`.

## API for other modules

Via the plugin export mechanism (`plugin.ExportFunction`):

- `GetWeather(zone string) map[string]any` — `{"type": "storm", "intensity": 0.72}`.
- `GetFronts() []map[string]any` — active systems.
- `SpawnFront(type, zone string, intensity float64) bool` — e.g. a quest that
  summons a storm.

All are safe to call any time (they return empty-but-valid answers before the
sim finishes starting) and run on the engine's single game-loop goroutine.

## What can break it — customizing your MUD away from stock GoMud

The module is built to fail soft, but these are the realistic ways a
customized world or forked engine changes its behavior. Roughly in order of
likelihood:

1. **Forking the engine (API drift).** The module's only engine-coupled code
   is the root package and `engine/`; the simulation (`sim/`), crawler, and
   data layer (`content/`) compile against nothing of GoMud's. Real example —
   **DOGMud** changed one signature: upstream
   `users.UserRecord.SendText(text string)` vs DOGMud's
   `SendText(category, text string)`. That's a *compile* error, and because
   every player-facing line in this module flows through one helper
   (`sendLine` in `weather.go`), the backport is a one-line change. That's the
   pattern to expect from forks: the damage is a compile error in the thin
   adapter layer, not silent misbehavior — *unless* the fork changes engine
   *semantics* rather than signatures (see #6).

2. **Reusing or removing the stock buff ids (31, 33).** The default specs
   reference engine buffs by **numeric id**. If your world deleted buff 31,
   blizzards just apply nothing (harmless). But if you *reassigned* id 31 to
   something else — "Vampiric Frenzy", say — blizzards will cheerfully apply
   it, with no warning, because an id is all the spec knows. If you've
   renumbered buffs, set `BuffsEnabled: false` or edit the two specs.

3. **Replacing the stock color patterns.** The specs color their text with
   `gray`, `blue`, `mute-dblue`, `frost`, `brown`, `embers` from your world's
   `color-patterns.yaml`. Removing/renaming those names just renders the text
   uncolored — cosmetic, but easy to miss.

4. **Claiming the `weather-` mutator namespace.** All module mutator ids start
   with `weather-`, and the module *enforces* that namespace at runtime: its
   reconciler removes any live `weather-*` zone mutator that doesn't match
   the simulation's view. If you hand-author a mutator named `weather-eclipse`
   and place it on a zone, the module will strip it within one tick. Use a
   different prefix for your own mutators. (A duplicate of one of our exact
   ids is caught at boot — the engine logs `duplicate mutator id` and keeps
   the disk version.)

5. **Biome data the module doesn't know.** Zones with no biome, or custom
   biome ids (`crystalwastes`), silently fall back to the mild `default`
   climate — weather still works, just blander and less varied. Fix by
   shipping a climate file per custom biome. Related: **indoor detection is a
   biome-id heuristic** (`cave`, `underground`, `dungeon`, `indoor`, `tunnel`,
   `sewer`). A custom `cavern` biome is treated as *outdoors* — players in it
   would see "rain patters down around you" — until M4's configurable indoor
   handling, the workaround is using one of the recognized ids or overriding
   the emote tables.

6. **Modifying engine internals the module's behavior depends on.** The
   adapter binds to `internal/mutators`, `internal/rooms` (zone configs),
   `internal/gametime`, and `internal/events` (`NewRound`). Signature changes
   show up as compile errors (good). *Behavioral* changes are subtler — as
   evidence that these internals genuinely matter, two real upstream behaviors
   shaped this module during development: `MutatorList.Remove` instantly
   resurrects any mutator whose spec has `decayintoid` (so our specs must not
   carry it), and `plugins.Load()` harvests a module's commands *before*
   calling its `onLoad` (so registration must happen in `init()`). A fork
   that "cleans up" mutator lifecycle or plugin loading can break weather in
   ways that compile fine. The boot smoke checklist in CONTRIBUTING.md is the
   quick way to validate a fork.

7. **Worlds the crawler sees differently than players do.** The graph is built
   from **room exits only**. Zones reachable solely by teleport, scripted
   movement, or magic words have no edges — weather never travels to or from
   them (each island runs independent weather; that's by design for planes,
   surprising for a teleport-hub world). Zone names matching `instance_*` or
   `ephemeral_*` are skipped entirely. After adding zones or exits, run
   `weather rebuild`.

8. **Aggressive game-time changes.** Ticks are scheduled in *game hours* via
   the engine's `gametime`; emotes in *rounds*. If you change `RoundSeconds`
   or the game-time calendar so an hour passes very fast or very slow, scale
   `TickEveryGameHours` / `EmoteEveryRounds` to taste. (A tick cadence far
   above a spec's `decayrate` is also safe — the module re-asserts mutators
   every tick — but between ticks a long-lived storm may briefly flicker as
   the safety-net decay fires.)

## Development & testing

The repo splits along an engine boundary:

```
sim/        pure simulation core — graph, fronts, climate, Step()   (no engine imports)
crawler/    pure geography crawler — exits → zone-adjacency graph   (no engine imports)
content/    pure data-file layer — climate + emote YAML parsing     (no engine imports)
engine/     the engine adapter — the ONLY package calling internal/* world APIs
weather*.go module root — plugin lifecycle, tick loop, commands, exports
files/      shipped data: config overlay, mutator specs, emote tables
```

Pure packages are tested standalone, no server needed:

```sh
go test ./sim/... ./crawler/... ./content/...
```

(Not `go test ./...` — the engine-coupled packages only compile inside a GoMud
checkout.) Architecture tests fail the build if a pure package ever imports
`internal/*`. For the engine-coupled packages, mirror the module into a
checkout and test there:

```powershell
pwsh scripts/sync-to-checkout.ps1 -Checkout <path-to-GoMud-checkout>
# then, from the checkout:
go test ./modules/weather/...
```

`CONTRIBUTING.md` covers the module/engine ownership boundary, the OOBE
requirement, architecture rules, and the boot smoke-test checklist. Each Go
package carries a `context.md` describing its responsibilities in detail.

## Roadmap

- **M4 — polish & default content:** per-room indoor/biome mutator variants,
  `Buffs.Overrides`, full per-biome emote/climate coverage for every stock
  biome, builder guide, CI.
- **Registry onboarding** — one-time listing so `module install weather` works.
- **v2 — seasons:** a season clock modulating climate weights; the data
  formats already leave the seams.

## License

[GPLv3](LICENSE), matching the GoMud engine.
