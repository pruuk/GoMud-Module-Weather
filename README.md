# GoMud Weather Module

[![CI](https://github.com/GoMudEngine/GoMud-Module-Weather/actions/workflows/ci.yml/badge.svg)](https://github.com/GoMudEngine/GoMud-Module-Weather/actions/workflows/ci.yml)

A living weather-and-seasons system for [GoMud](https://github.com/GoMudEngine/GoMud)
worlds. Weather forms as discrete, named **systems (fronts)** that move across a
graph of your world's geography, gather or lose strength based on the **terrain
they cross**, and express themselves through GoMud's existing room **mutators** —
room names and descriptions, alerts, light, ambient emotes, and curated,
overridable buffs. Layered over that, a **seasonal calendar** quietly bends the
odds: which weather forms, how often, and how the world *reads* between storms.

> A storm forms over the coast, rolls inland across the plains gathering
> strength, climbs into the mountains where the terrain bleeds it dry, and
> dissipates on the far side — and players in each zone along the way feel it
> arrive, pass, and leave. The **same coast** plays differently in deep winter
> than in high summer: in winter that rain comes as freezing sleet rattling off
> the stones; in the calm between fronts, the season speaks for itself —
> "a skin of ice creaks at the edges of still water."

Built in the same spirit as the
[GoMud Module Playtest Harness](https://github.com/GoMudEngine/GoMud-Module-Playtest-Harness):
engine-native, compiled-in, data-driven, and testable in isolation.

**Status: weather complete (M3); seasons complete (S1–S3); polish complete
(M4).** The module works end-to-end on a stock GoMud world: install, run, and
storms travel, rooms render `(storm-wracked)`, indoor rooms read sheltered
("rain drums on the roof") while outdoor rooms get rained on, ambient lines
play indoors and out, state survives reboots, and the calendar shifts each
zone's climate and voice — winter rain reads as sleet, calm winter rooms get
their own quiet ambience. M4 added per-room refinement (indoor mutator
variants), the module's own gentle weather buffs, `BuffOverrides`,
`ExcludeZonePatterns`, per-biome emote variants across the stock biomes, admin
page validation, and CI. Remaining before a public release: the one-time
module-registry version bump. Two design
specs are the source of truth for scope and architecture — the
[weather design](docs/superpowers/specs/2026-06-08-weather-module-design.md) and
the [seasons design](docs/superpowers/specs/2026-06-10-seasons-design.md) — and
dated status notes in each record exactly what every milestone shipped.

---

## What this module is

- **A weather simulation at zone granularity.** Every zone has exactly one
  current weather type (`clear`, `overcast`, `rain`, `storm`, `fog`, `snow`,
  `blizzard`, `dust`, `heatwave` out of the box — the set is open data, not a
  hardcoded enum). Fronts travel zone-to-zone along exits your world already has.
- **Biome-aware, in both directions.** A zone's biome decides which weather can
  form there and how likely it is (deserts birth dust, not blizzards), and the
  terrain a front crosses feeds or saps it (oceans feed storms; mountains bleed
  them dry, so systems die crossing a range).
- **Calendar-driven seasons.** Each biome is bound to a named **season track**;
  the in-game calendar advances the season, and the season re-weights that
  biome's climate (winter favors snow over rain, monsoon's wet season drowns the
  jungle in storms). Two tracks ship — `temperate` (winter/spring/summer/autumn)
  and `monsoon` (wet/dry) — and seasons are **pure YAML**, including esoteric
  ones that introduce weather a biome never normally sees (a "glass-rain
  Shattering"). Seasons run at zone granularity: one season per zone.
- **Deterministic and persistent.** The simulation core is a pure function over
  a seeded RNG: the same seed and world replay the same weather, and active
  fronts + RNG state are saved across reboots. Great for debugging and tests.
- **Data-driven presentation.** The engine owns weather and season *state*; your
  world owns its *voice*. Everything players read lives in YAML you can override:
  mutator specs (room name/description/alert/light/buffs per weather type and per
  season) and emote tables keyed by weather × biome × indoor/outdoor — now with
  optional **per-season variants** and a standalone **seasonal-ambience** layer.
- **Zero engine changes.** The module compiles in against existing GoMud APIs
  (mutators, events, gametime, plugin storage, plugin data files). Nothing in
  `internal/` is patched.

## What this module is NOT

- **Not per-room weather *simulation*.** Simulation granularity is the zone:
  two outdoor rooms in one zone always share weather. Per-room **refinement**
  (the M4 default) varies the *presentation* room by room — indoor rooms are
  not rained on; they get a sheltered indoor mutator variant ("rain drums on
  the roof") with no light penalty and no buffs — but the underlying weather
  is still one type per zone.
- **Not per-room or per-biome seasonal variation.** Seasons resolve per zone (one
  season, one `season-*` mutator per zone). Per-biome seasonal mutator variants
  and per-zone track overrides are deferred to a later milestone. Emote tables
  *do* vary by biome within a season; mutator descriptions do not yet.
- **Not a wind/pressure/temperature simulation.** No vector fields, no
  thermodynamics, no prevailing wind. Weather types and seasons carry coarse
  implications (a blizzard is cold; winter is colder) through the buffs and prose
  you configure — not a numeric temperature model.
- **Not a prose author.** We ship sensible default text so it works out of the
  box, but the defaults are sparse by design and meant to be replaced with your
  world's voice.
- **Not a drop-in plugin for a running server.** GoMud modules are *compiled
  into* the server binary. Installing means adding source and rebuilding — see
  Installation.
- **Not client-side rendering / GMCP.** A weather GMCP package is a listed future
  enhancement, not part of v1.

## Requirements

- **Go 1.24+** ([go.dev/dl](https://go.dev/dl/)) — the same minimum as the GoMud
  engine itself; the module needs nothing newer. You don't need to know Go to
  *use* the module, but you need the toolchain to build GoMud at all.
- **A current upstream GoMud checkout.** The module binds to engine features that
  exist on upstream `master` as of mid-2026, most importantly **plugin-filesystem
  data loading for mutators** (the engine wires
  `mutators.RegisterFS(plugins.GetPluginRegistry())` in `main.go`). If your
  engine predates that, the module's weather and season mutators never load and
  every change logs a "no mutator spec loaded" warning — the server stays
  healthy, but rooms won't render weather or seasons.
- **A usable game calendar** for the seasons layer. Seasons read the active
  calendar from `gametime`; the stock 12-month calendar is what the shipped
  tracks assume. With no usable calendar the module fails soft — seasons simply
  switch off and weather runs exactly as it would without them (see *What can
  break it*).
- The stock color patterns `gray`, `blue`, `mute-dblue`, `frost`, `brown`,
  `embers` (from your world's `color-patterns.yaml`). Missing any of these
  degrades gracefully (see *What can break it*). The module no longer borrows
  any stock buffs: since M4 it ships its own gentle weather buffs in the
  reserved id range **59001–59099** (a disk buff at one of those ids wins the
  collision — again, see *What can break it*).

## Installation

These steps assume you've never built GoMud before. **The module manager is the
install path** — you do not copy files by hand.

1. **Install Go** from [go.dev/dl](https://go.dev/dl/) and confirm it works:

   ```sh
   go version
   ```

2. **Get GoMud:**

   ```sh
   git clone https://github.com/GoMudEngine/GoMud.git
   cd GoMud
   ```

3. **Install the module through GoMud's module manager** (the standard path —
   the module is listed in the official registry):

   ```sh
   go run . module install weather
   ```

   This downloads the release archive, verifies its checksum, and extracts it to
   `modules/weather/`. You'll be asked to confirm a third-party install (the
   module is community-authored, not by the GoMud team).

4. **Register modules and build.** From the GoMud checkout root:

   ```sh
   go generate ./...   # regenerates modules/all-modules.go to include weather
   go build
   ```

   `go generate` is the step people forget: modules are wired in by a generated
   import file, so without it the module silently isn't in the binary.

5. **Run the server:**

   ```sh
   go run .            # or run the binary you just built
   ```

   Connect with any telnet/MUD client to the port in `_datafiles/config.yaml`
   (stock default: 33333).

That's the whole install. Weather **and** seasons are **enabled by default**
(the module's code defaults to `Enabled: true` — no config needed): on the
first game round the module crawls your world's zones and exits into a geography
graph, caches it, seeds the simulation from your zone names (stable per world),
binds each biome's season track to the calendar, and starts ticking once per game
hour. No data authoring, no room tagging, no world prep.

### What you'll see in the boot log

```
mutators.LoadDataFiles()  loadedCount=32        ← your world's specs + our 22 (8 weather + 8 indoor + 6 season)
Weather: built geography graph  zones=15 edges=10 components=6
Weather: seasons active  tracks=2 seasonalZones=8
Weather: fresh simulation state  seed=17214436859030717895 currentRound=...
```

`tracks=2` is the two shipped tracks loaded; `seasonalZones=8` is how many of the
stock world's zones have a biome bound to a track (all temperate, on the stock
world — the `monsoon` track ships but no stock biome binds to it). On later boots
you'll see `loaded geography cache` and `restored simulation state fronts=N`
instead, and the `seasons active` line re-asserts each zone's season mutator
(zone mutators don't survive reboots, so the module re-applies them) with **no**
flood of season-change events.

If `SeasonsEnabled: false`, the `seasons active` line is absent entirely and
weather runs exactly as it did before seasons existed.

## Using it in game

Any player:

| Command | Output |
|---|---|
| `weather` | `The weather in Frostfang is clear.` — plus `The season here is winter.` when seasons are on, and the dominant front and felt intensity when a system covers your zone. |

Admins (and mods granted the `weather` permission key):

| Command | What it does |
|---|---|
| `weather status` | Graph summary, active front count, next tick round, emote/buff/persist settings, and the seasons summary (tracks loaded, seasonal zones). |
| `weather zones` | Every zone and its current weather. |
| `weather fronts` | Active systems: id, type, center zone, intensity, moisture, age. |
| `weather seasons` | Each loaded track, the season it sits in right now, and the blend percentage when inside a transition window. |
| `weather spawn <type> <zone> [intensity]` | Force a front (e.g. `weather spawn storm Frostfang 0.9`). Zone names may contain spaces; intensity is an optional trailing number 0..1. |
| `weather clear [zone]` | Remove all fronts, or every front whose coverage reaches the named zone. |
| `weather graph [zone]` | A zone's graph neighbors and border weights (crawler spot-check). |
| `weather rebuild` | Re-crawl the world and rewrite the graph cache (run after adding zones/exits); also re-resolves every zone's season. |

Weather and seasons show up without anyone running commands, of course: outdoor
room names get a tag like `(raining)`, descriptions gain a weather line and a
season line, severe weather adds an alert banner and dims light, and occupied
rooms hear ambient lines every ~20 rounds. Indoor rooms are sheltered on every
channel: a muffled description line ("rain drums on the roof"), no name tag, no
alert, no light penalty, no buffs, and indoor ambient emote variants.

**Across a season boundary**, a player sees the change accumulate rather than
flash: room descriptions pick up the new `season-*` line on the next render, the
weather odds drift toward the new season over the track's transition window
(`transitionDays`), and the ambient voice changes — in calm winter weather a room
might offer "your breath plumes white and hangs in the frozen air," while the same
room in a summer storm hears the winter-less base storm lines. One ambient line
per room per pass, and **weather always wins** over a quiet seasonal line.

### Admin page

The module ships a browser-based admin page at `/admin/weather` (visible in the
GoMud web admin under **Modules → Weather**). Three sections: **Status** — an
auto-refreshing (every 5 s) view of simulation state: sim/seasons flags, current
round, graph summary, the refinement mode with its **occupied rooms** count,
active fronts table, and zones with their current weather and season.
**Configuration** — every config key with its current value, a **typed** edit
control (checkboxes for booleans, dropdowns for enums like `EmoteMode` and
`PerRoomRefinement`, read-only rows for keys set in `config-overrides.yaml`), and a
badge that says exactly when a change takes effect (**live**, **applies on next
graph rebuild**, **takes effect on reboot**, …). Writes are validated per key —
a bad value (out-of-range number, unknown enum choice) is rejected with a clear
error instead of being silently clamped — and valid changes are persisted to
the world's config-overrides file
(`_datafiles/world/default/config-overrides.yaml`) so they survive reboots.
**Actions** — spawn a front (type + zone + intensity), clear weather (one zone or
all), and rebuild the geography graph; results are asynchronous and appear in the
next status refresh.

The page's write operations (config saves and actions) require the `weather.write`
permission. The engine uses prefix matching (`userrecord.go:435`): a user granted
`weather` already satisfies `weather.write`, so one grant covers both the
in-game command tools and the admin page's write endpoints.

## How it works (one paragraph, plus the seasons transform)

A **crawler** walks every room exit once at boot and reduces your world to a
zone-adjacency graph (zone = node, "rooms in A have exits into B" = weighted
edge). A pure, seeded **simulation** ticks once per game hour: fronts age,
terrain feeds or saps them, they move along edges (wide borders are likelier),
their type drifts toward what the local climate supports, dead ones are removed,
new ones spawn within a budget, and every zone resolves to one weather type
(strong fronts project onto neighboring zones, so a big storm covers an area, not
a point). The **engine adapter** then makes the world match. In the default
**per-room** mode, each occupied room gets exactly the right room-level mutator
— `weather-<type>` outdoors, the sheltered `weather-<type>-indoor` variant
indoors — refreshed each tick and the moment a player walks into a room; with
`PerRoomRefinement: off`, each zone's `ZoneConfig.Mutators` carries one
zone-wide `weather-*` mutator instead (the engine merges zone mutators into
every room render). Either way an emote scheduler voices occupied rooms, and
state is saved through plugin storage and reconciled on boot.

**The seasons transform sits in front of the simulation and behind the
presentation.** Each tick, before `sim.Step` runs, the season resolver reads the
calendar's day-of-year, resolves each track to its current season (blending
across the transition window), and produces an **effective climate** — the
biome's base weights scaled and re-weighted by the season's multipliers and
additions — which is what the simulation actually rolls against. Independently, a
`season-*` mutator is reconciled onto every season-bound zone (its own namespace,
alongside the `weather-*` layer), so room descriptions carry the season. Finally,
the **emote arbiter** picks one ambient line per occupied room per pass: if the
zone has non-calm weather it sends a weather line (using the season's *variant*
lines when the active emote table has them), and only in **calm** zones, at a
reduced 1-in-3 cadence, does it fall through to the zone's standalone seasonal
ambience. Weather wins; seasonal ambience is the quiet voice between fronts.

## Configuration

All knobs live under `Modules.weather.*`. Defaults live in **code**
(`buildConfig` in `weather_config.go`) — every key may be omitted, and a fresh
install boots enabled with the values below. To change a setting, use the
admin page or add a `Modules: weather:` block to your world's
`config-overrides.yaml` (`_datafiles/world/default/config-overrides.yaml`).
The shipped `files/data-overlays/config.yaml` is **documentation only** — it
carries no active keys, deliberately: the engine's overlay merge
(`configs.OverlayOverrides`) *replaces* the world's `Modules.weather` block
when an overlay introduces keys the block lacks, which would wipe operator
settings on reboot. The file's header has the full story.

| Key | Default | Meaning |
|---|---|---|
| `Enabled` | `true` | Master switch. Off = module registers nothing but an inert command. |
| `Seed` | `0` | RNG seed. `0` derives a stable seed from your zone names (same world ⇒ same seed; negative values are treated as 0). |
| `TickEveryGameHours` | `1` | Simulation cadence in game hours (minimum 1). |
| `MaxActiveFronts` | `8` | Global front budget (minimum 1). |
| `SpawnRateScale` | `1.0` | Multiplier on spawn pressure. `0` stops new fronts entirely. |
| `EmoteMode` | `module` | `module` = we emit ambient lines (weather and seasonal); `tag-only` = we stay silent and your room scripts react to the weather/season mutators and alerts instead. |
| `EmoteEveryRounds` | `20` | Ambient emote cadence in rounds, jittered ±25% (minimum 5). The seasonal layer fires at ~1-in-3 of these passes, in calm zones only. |
| `BuffsEnabled` | `true` | Apply the buffs carried by weather mutators — the module's own gentle trio (blizzard → 59001 Weather Chilled, storm → 59002 Weather Soaked, heatwave → 59003 Weather Parched). `false` strips buff ids from both the weather and season specs at boot, and wins over any `BuffOverrides`. |
| `Persist` | `true` | Save/restore fronts + RNG across reboots. |
| `IncludeSecretExits` | `true` | Crawler counts secret/locked exits as zone adjacency (weather doesn't care about locks). |
| `RebuildGraphOnBoot` | `false` | Force a fresh crawl each boot instead of using the cache. |
| `SeasonsEnabled` | `true` | Master switch for the seasons layer. `false` = weather runs exactly as v1 (no climate shifts, no `season-*` mutators, no `WeatherSeasonChanged` events, no `GetSeason` response, no seasonal ambience). |
| `PerRoomRefinement` | `occupied` | Where weather mutators live. `occupied` = on the rooms that hold players (refined on entry and each tick); `all` = on every room in every zone (force-loads rooms — see *What can break it*); `off` = one zone-wide mutator per zone (the pre-M4 behavior; indoor rooms get rained on again). Seasons stay zone-wide in every mode. |
| `BuffOverrides.<type>` | *(unset)* | Per-weather-type buff replacement: a comma-separated list of buff ids **replaces** the outdoor `weather-<type>` spec's player buffs; an **empty string strips** that type's buffs. Unset = the shipped buffs. Applied at boot, before the `BuffsEnabled` strip (so `BuffsEnabled: false` still wins). See *Extending the module*. |
| `ExcludeZonePatterns` | `instance_*,ephemeral_*` | Comma-separated globs of zone names the geography crawler skips. Applies on the next graph rebuild (`weather rebuild` or the admin page's Rebuild action). Empty falls back to the default; to effectively disable exclusion, set a single never-matching token. |

Planned but not yet config keys: `PrevailingWind` and a configurable
`seasonalEmoteOneIn` cadence (a `const` in `engine/emotes.go` today).

## Customizing the content

Everything a builder would want to change is YAML under `files/datafiles/`,
rebuilt into the binary. No Go required.

### Climate and season-track binding

- **Climate** — drop `files/datafiles/climate/<biome>.yaml` to override a biome's
  weather weights, terrain influence, spawn pressure, and **its season track**
  (`track: temperate`). Schema is in the weather design spec §7.3. Biomes without
  a file use built-in defaults for the standard biomes
  (plains/forest/mountain/desert/tundra/swamp/ocean, all bound to `temperate`,
  plus a `jungle` profile bound to `monsoon`) and a mild `default` profile for
  everything else. Note: an override replaces the biome's profile wholesale —
  omitted fields become zero, including `spawnWeight` and `track` (so re-state
  the track when overriding a bound biome).
- A biome with **no track** is *season-unbound*: it still gets weather, just no
  seasonal climate shift, no `season-*` mutator, and no seasonal ambience.

### Season tracks (pure YAML, including esoteric ones)

A track lives in `files/datafiles/seasons/<track>.yaml`: a name and an ordered
list of seasons, each owning a set of 1-based calendar months. Two shipped:
`temperate.yaml` (winter/spring/summer/autumn) and `monsoon.yaml` (wet/dry). A
season re-weights the bound biome's climate; the multipliers blend across
`transitionDays` at the season's start.

Two optional per-season fields let you introduce weather types **completely
absent** from a biome's base climate — the "glass rain during the Shattering"
pattern from the seasons spec §3.1a:

```yaml
# files/datafiles/seasons/mystic.yaml
track: stillness
seasons:
  - name: calm
    months: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
  - name: shattering           # a Broken-Earth-style season
    months: [11, 12]
    transitionDays: 2
    baseWeightScale: 0.0       # suppress ALL normal weather (default 1.0)
    weatherWeightAdditions:    # absolute weights ADDED — may introduce absent types
      glassrain: 8
      ashfall: 3
    spawnWeightMultiplier: 2.0
    influence: { intensityDelta: 0.05 }
```

`baseWeightScale` multiplies all of the biome's normal weights *before*
`weatherWeightAdditions` are added on top, so `0.0` + additions means "during the
Shattering, only glass rain and ashfall, nothing else." Then bind a biome to the
track (`track: stillness` in its climate file) and follow the new-weather-type
recipe below for `glassrain`/`ashfall`. A glass-rain season is pure YAML, no Go.

### Mutator specs — two namespaces, two rules

Room rendering and mechanics come from `MutatorSpec` files (standard engine
schema: name tag, description line, alert, `lightmod`, buff ids). There are two
namespaces, validated under the same rules:

- **`files/datafiles/mutators/weather_<type>.yaml`** — one per weather type,
  **plus** a sheltered `weather_<type>_indoor.yaml` twin used by per-room
  refinement (description line only — the shipped-data tests enforce the
  pairing and forbid `lightmod`, buffs, and alerts on indoor variants;
  omitting the name tag is authoring convention, not test-enforced; see
  *Extending the module*).
- **`files/datafiles/mutators/season_<track>_<season>.yaml`** — one per shipped
  (track, season): `season-temperate-winter`, …, `season-monsoon-dry`. Each
  appends one description line to every room in the zone, e.g. *"Winter holds the
  land; frost rims every edge and breath hangs in the air."*

Two rules our validation tests enforce, learned the hard way against the live
engine and applied to **both** namespaces: a spec must never set `respawnrate`
(it would fight the orchestrator and prevent cleanup) and never set `decayintoid`
(the engine's `Remove` instantly resurrects the decay target — see *What can
break it*). `decayrate` stays: it's the self-heal safety net if the module is
disabled mid-storm or mid-season. The mutator-id-to-filename rule is the engine
loader's: id lowercased with non-alphanumerics as `_` (id `weather-acid-rain` ⇒
file `weather_acid_rain.yaml`).

### Emote tables — base, seasonal variants, and seasonal ambience

- **Weather emotes** — `files/datafiles/emotes/<type>.yaml`. Lines are keyed by
  biome with a `default` fallback, split `outdoor:` / `indoor:` (indoor never
  falls back to outdoor — silence beats "rain falls around you" inside a tavern).
  Add a biome key to give, say, forests their own storm lines. Since M4 the
  shipped tables do exactly that — ~30 per-biome variant lines across the stock
  outdoor biomes (city rain hissing off slate roofs, shore storms, farmland
  heatwaves) — and the shipped-data tests reject any biome key that isn't a
  `sim.DefaultClimate` biome id, so a typo can't ship as unreachable prose.
- **Per-season weather variants** — add a `seasonal:` block to any weather emote
  table to override lines for specific seasons. The season key matches by **name
  across tracks** ("winter" is temperate's winter; a monsoon world keys off
  "wet"/"dry"), and lookup falls through to the base lines when a season has no
  variant — sparse by design. Example, from the shipped `rain.yaml`:

  ```yaml
  weather: rain
  outdoor:
    default:
      - "Rain patters down steadily around you."
  seasonal:
    winter:
      outdoor:
        default:
          - "Freezing rain rattles off every surface like thrown gravel."
    wet:
      outdoor:
        default:
          - "The downpour is total, a warm wall of water without seams."
  ```

  Four high-value variants ship (rain×winter, rain×wet, storm×winter,
  heatwave×dry); everything else is builder territory.
- **Seasonal ambience** — `files/datafiles/emotes/seasons/<track>_<season>.yaml`.
  These are the *standalone* voice of a season in **calm** weather (the variant
  blocks above cover weathered moments). Each file declares its `track:` and
  `season:` and carries the same biome-keyed `outdoor:`/`indoor:` shape. The
  subdirectory keeps them away from the (non-recursive) weather-table loader; the
  `<track>_<season>.yaml` filename rule is **ours** (the content loader), not the
  engine's. Six ship — temperate ×4, monsoon ×2 — each with outdoor + indoor
  defaults (winter carries a `forest` variant as the worked example).
- **Total control?** Set `EmoteMode: tag-only` and react to the `weather-*` and
  `season-*` mutators from your own room scripts; the module emits no ambient
  lines of either kind.

### Builder seam — seasonal exits

To add a season-only crossing (a frozen river, a snowed-in pass), create a
world-specific override of the season spec and add an `exits:` block. The field
shape matches the engine's standard `MutatorSpec` (verified against
`pushed_boulder.yaml` and `internal/exit/exit.go`):

```yaml
# my_world/mutators/season_temperate_winter.yaml  (override of the shipped spec)
mutatorid: season-temperate-winter
descriptionmodifier:
  behavior: append
  text: Winter holds the land; frost rims every edge and breath hangs in the air.
  colorpattern: frost
decayrate: 24 hours
exits:
  across the ice:
    roomid: 123
```

While `season-temperate-winter` is active on the zone, players see the exit
`across the ice` leading to room 123; when the season flips, the exit disappears.
The same `exits:` field works for weather specs if you want a storm-only secret
passage.

### Buffs posture

Since M4 the module ships its **own** weather buffs instead of borrowing the
stock world's: **59001 Weather Chilled** (blizzard, −5 speed/−3 strength),
**59002 Weather Soaked** (storm, −5 perception), and **59003 Weather Parched**
(heatwave, −5 speed). All three live in `files/datafiles/buffs/`, are
deliberately gentle nuisances (the engine's 31 Freezing Snow deals damage; 33
Thirsty is −20 across five stats), and fade two rounds after the player finds
shelter (`triggercount: 2`). They are carried only by the **outdoor** specs —
stepping indoors is real shelter. Want harsher (or different) effects? Replace
them per type with `BuffOverrides.<type>` (see *Extending the module*).

The shipped **season** specs remain **description-only** — no buffs, no exits.
Zone-wide buffs that persist for an entire ~4-real-day season are too
heavy-handed to ship as defaults, and `BuffOverrides` covers weather types
only; a world that wants season buffs adds them via a world-specific override
of the season spec (the same seam as seasonal exits above). The
`BuffsEnabled: false` toggle strips buffs from **both** namespaces — overrides
included — if you want a zero-buff install.

## Extending the module — builder recipes

Everything in *Customizing the content* above edits what already exists. This
section is the recipe book for adding things that don't. Throughout, "shipped
data" means the YAML under this module's `files/datafiles/` (changing it means
editing the module and rebuilding — the shipped-data tests then hold you to the
rules); a **world override** is the same-named file in your world's own data
directory, which wins without touching the module — but that seam exists only
for **mutator specs and buffs**, the two kinds of file the engine loads
disk-first. Climate files, weather emote tables, seasonal-ambience tables, and
season tracks are read exclusively from the module's embedded data — a
same-named world file is silently ignored — so extending those always means
editing the module and rebuilding.

### Recipe: a new weather type, end to end

Four files (one optional), in dependency order. Worked example: `ashfall`.

1. **Make it formable — climate weights.** A weather type exists wherever a
   climate gives it weight. Either add it to a biome's base weights
   (`files/datafiles/climate/<biome>.yaml`: `biome: tundra` plus
   `weather: { ashfall: 4, ... }` — the `biome:` key is **required**, a file
   without it is rejected with a logged warning; and remember a climate file
   replaces the biome's profile wholesale) or introduce it only
   during a season via `weatherWeightAdditions:` (the glass-rain pattern in
   *Season tracks* above — no climate edit needed).
2. **Make it render — a mutator spec PAIR.** `weather_ashfall.yaml` (id
   `weather-ashfall`: name tag, description, optional alert/`lightmod`/buffs)
   **and** `weather_ashfall_indoor.yaml` (id `weather-ashfall-indoor`:
   description line only). The pairing is enforced — `TestShippedMutatorSpecs`
   fails any outdoor `weather-<type>` without its `-indoor` twin, and fails an
   indoor variant that sets `lightmod`, buffs, or an alert. Both specs need
   `decayrate` and must NOT set `respawnrate` or `decayintoid` (see the rules
   in *Mutator specs* above). Without the indoor twin, indoor rooms in an
   ashfall zone would log a warn-once and render nothing.
3. **Make it speak — an emote table.** `files/datafiles/emotes/ashfall.yaml`
   with at least one `outdoor: default:` and one `indoor: default:` line
   (test-enforced); per-biome and `seasonal:` variants at your leisure.
4. **Optionally, make it bite — a buff.** See the buff recipe below, then add
   `playerbuffids: [ 590xx ]` to the **outdoor** spec only.

One Go touch for module contributors: add the type to `sim.KnownWeatherTypes`
(`sim/weather.go`). A drift-guard test fails in both directions — a shipped
outdoor spec missing from the list, or a listed type with no shipped spec — and
the list is what makes `BuffOverrides.<type>` recognize the new key. (There is
no world-override path to a new weather type: steps 1 and 3 — climate weights
and emote tables — load only from the module's embedded data, so a new type is
always a module edit and a rebuild. While you're in there, add the list entry
too, or `BuffOverrides.ashfall` is ignored — the config reader enumerates
exactly `sim.KnownWeatherTypes`.)

### Recipe: a season track or esoteric season

Pure YAML, no Go — *Season tracks* above is the full reference, including the
`baseWeightScale: 0.0` + `weatherWeightAdditions:` "Shattering" example.
Checklist form: (1) `files/datafiles/seasons/<track>.yaml` whose seasons'
`months:` exactly cover your calendar's months — gaps or overlaps reject the
track at load; (2) bind biomes to it (`track: <name>` in their climate files);
(3) a `season_<track>_<season>.yaml` mutator spec per season (description
line; same `decayrate`-yes / `respawnrate`-`decayintoid`-no rules); (4)
optional voice: `emotes/seasons/<track>_<season>.yaml` ambience tables and
`seasonal:` variant blocks in the weather tables (variant keys match by season
*name* across tracks).

### Recipe: overriding or stripping weather buffs

`BuffOverrides.<type>` keys in your world's `config-overrides.yaml`, under
`Modules: weather:` (`files/data-overlays/config.yaml` carries commented
examples):

```yaml
BuffOverrides.storm: "59002, 12"   # replace storm's player buffs wholesale
BuffOverrides.blizzard: ""         # empty string = strip blizzard's buffs
```

The semantics, in precedence order:

- A key **replaces** that type's outdoor `playerbuffids` wholesale at boot; an
  **empty string** is an explicit strip; an absent key keeps the shipped buff.
- `BuffsEnabled: false` runs **after** the overrides and strips everything —
  disabling buffs always wins.
- Indoor variants never carry buffs; overrides can't add them there.
- Bad tokens warn and are skipped; a value with no usable ids at all falls back
  to the shipped buffs; an override naming a weather type with no loaded spec
  (`clear`, or a typo) warns once at apply time.
- Changes take effect **on reboot** (boot-time spec mutation — the admin page
  shows the `BuffOverrides.*` summary as a read-only row).

Convention: module-shipped buffs use ids **59001–59099**. Custom buffs you
write for overrides can use any free id, but staying out of that range avoids
colliding with future module buffs.

### Recipe: choosing a per-room refinement mode

What each `PerRoomRefinement` value costs and shows:

| Mode | Players see | Cost |
|---|---|---|
| `occupied` *(default)* | Correct outdoor/indoor rendering in every room a player is in — refined on entry (`RoomChange`) and every tick. One honest caveat: the engine renders the entry look *before* the `RoomChange` listener runs, so the very first description of a freshly entered, previously unoccupied room can miss the weather line; it's correct from the next render on (a one-render lag the design spec accepts). An *empty* room's data may be momentarily stale; no player is there to see it, and it heals on entry. | Negligible: a handful of mutator reconciles per move/tick; never force-loads a room. |
| `all` | Correct rendering in every room, occupied or not — for worlds with scripts or scrying that read unoccupied rooms' mutators. | Force-loads **every room in every zone each tick**. Above the engine's room-memory threshold that means load/save disk churn every tick. Use deliberately. |
| `off` | The pre-M4 behavior: one zone-wide mutator; indoor rooms render the outdoor weather ("rain patters down" in the tavern). | Cheapest: one mutator list per zone. |

In both room modes there is **no zone-level weather mutator at all** — rooms
are the only carriers (seasons stay zone-wide in every mode). World scripts
that check zone mutator tags must check room mutators instead; `off` restores
the old contract (see *What can break it*).

### Recipe: seasonal exits and per-biome emote variants

- **Seasonal (or weather) exits** — the `exits:` block in a world override of a
  mutator spec; *Builder seam — seasonal exits* above has the worked frozen-
  river example. The same field on a `weather-*` spec makes a storm-only
  passage; in room modes, put it on the outdoor spec (indoor rooms carry the
  `-indoor` variant).
- **Per-biome emote variants** — in any emote table section (`outdoor:`,
  `indoor:`, or inside a `seasonal:` block), keys are biome ids. Resolution is
  two steps: the room's biome id → that key's lines; no key for that biome →
  the `default` key. There is no chain beyond that (and indoor never falls
  back to outdoor). Valid keys are the `sim.DefaultClimate` biome ids — the
  shipped-data tests enforce this, because an unknown key is simply
  unreachable: no room ever reports that biome.

## API for other modules

Via the plugin export mechanism (`plugin.ExportFunction`), and the event bus:

- `GetWeather(zone string) map[string]any` — `{"type": "storm", "intensity": 0.72}`.
- `GetFronts() []map[string]any` — active systems (id, type, zone, intensity,
  moisture, age).
- `SpawnFront(type, zone string, intensity float64) bool` — e.g. a quest that
  summons a storm.
- `GetSeason(zone string) map[string]any` —
  `{"track": "temperate", "season": "winter", "blend": 0.0}`; empty strings when
  seasons are off, the zone is unknown, or its biome is season-unbound.
- **`WeatherSeasonChanged{Zone, Track, From, To}`** event — queued on the engine
  event bus when a zone's resolved season flips. Listen with
  `events.RegisterListener(weather.WeatherSeasonChanged{}, handler)`. It is never
  emitted on the baseline resolution at boot (so reboots don't replay a flood),
  and only fires within a single track (`From`/`To` are always seasons of the
  same track — an admin biome reassignment emits nothing).

All exports are safe to call any time (they return empty-but-valid answers before
the sim finishes starting) and run on the engine's single game-loop goroutine.

## What can break it — customizing your MUD away from stock GoMud

The module is built to fail soft, but these are the realistic ways a customized
world or forked engine changes its behavior. Roughly in order of likelihood:

1. **Forking the engine (API drift).** The module's only engine-coupled code is
   the root package and `engine/`; the simulation (`sim/`), crawler, data layer
   (`content/`), and season resolver (`seasons/`) compile against nothing of
   GoMud's. Real example — **DOGMud** changed one signature: upstream
   `users.UserRecord.SendText(text string)` vs DOGMud's
   `SendText(category, text string)`. That's a *compile* error, and because every
   player-facing line in this module flows through one helper (`sendLine` in
   `weather.go`), the backport is a one-line change. That's the pattern to expect
   from forks: the damage is a compile error in the thin adapter layer, not silent
   misbehavior — *unless* the fork changes engine *semantics* rather than
   signatures (see #6).

2. **A world buff already living in the 59001–59099 range.** The module ships
   its three weather buffs (59001/59002/59003) in that reserved range and the
   engine merges plugin buffs *under* disk buffs: on an id collision **the disk
   copy wins** and the module's buff is skipped, with a log line at startup.
   The weather mutators then apply *your* buff — whatever it is — every round
   the weather holds, because an id is all the spec knows. If your world
   already uses those ids, either renumber your buffs out of the range or
   remap the module's per type via `BuffOverrides.<type>` (or set
   `BuffsEnabled: false`).

3. **Replacing the stock color patterns.** The specs color their text with
   `gray`, `blue`, `mute-dblue`, `frost`, `brown`, `embers` from your world's
   `color-patterns.yaml`. Removing/renaming those names just renders the text
   uncolored — cosmetic, but easy to miss.

4. **Claiming the `weather-` or `season-` mutator namespace.** All module mutator
   ids start with `weather-` or `season-`, and the module *enforces* both
   namespaces at runtime: each reconciler removes any live `weather-*` /
   `season-*` mutator — on zones, and in room modes on the rooms it refines —
   that doesn't match the simulation's (or the calendar's)
   view. If you hand-author a mutator named `weather-eclipse` or
   `season-festival` and place it on a zone or room, the module strips it within
   one tick — with one qualifier: in the default `occupied` mode the reconciler
   never visits an *unoccupied* room, so a stray there lingers until its
   `decayrate` retires it or a player walks in (refine-on-entry strips it then).
   Use a different prefix for your own mutators. (A duplicate of one of our exact
   ids is caught at boot — the engine logs `duplicate mutator id` and keeps the
   disk version.)

5. **Biome data the module doesn't know.** Zones with no biome, or custom biome
   ids (`crystalwastes`), silently fall back to the mild `default` climate —
   weather still works, just blander and less varied — **and they are
   season-unbound** (no `default`-profile track), so they get no seasonal climate
   shift, no `season-*` mutator, and no seasonal ambience until you ship a climate
   file that names a `track:`. Related: **indoor detection is a biome-id
   heuristic** (the `indoorBiomes` set in `engine/worldreader.go`: `cave`,
   `underground`, `dungeon`, `indoor`, `tunnel`, `sewer`; unknown ids count as
   outdoors). Since M4
   the heuristic decides more than emote flavor: in room modes it picks which
   mutator a room gets, so a custom `cavern` biome is treated as *outdoors* —
   rained on, name-tagged, buffed — not just told "rain patters down around
   you." The workaround is unchanged: use a recognized indoor biome id (or
   extend the set in the module).

6. **Modifying engine internals the module's behavior depends on.** The adapter
   binds to `internal/mutators`, `internal/rooms` (zone configs),
   `internal/gametime`, and `internal/events` (`NewRound`). Signature changes show
   up as compile errors (good). *Behavioral* changes are subtler — two real
   upstream behaviors shaped this module during development:
   `MutatorList.Remove` instantly resurrects any mutator whose spec has
   `decayintoid` (so our specs must not carry it), and `plugins.Load()` harvests a
   module's commands *before* calling its `onLoad` (so registration must happen in
   `init()`). A fork that "cleans up" mutator lifecycle or plugin loading can break
   weather in ways that compile fine. The boot smoke checklist in CONTRIBUTING.md
   is the quick way to validate a fork.

7. **Worlds the crawler sees differently than players do.** The graph is built
   from **room exits only**. Zones reachable solely by teleport, scripted
   movement, or magic words have no edges — weather never travels to or from them
   (each island runs independent weather; that's by design for planes, surprising
   for a teleport-hub world). Zone names matching the `ExcludeZonePatterns` globs
   (default `instance_*,ephemeral_*`) are skipped entirely. After adding zones,
   exits, or exclusion patterns, run `weather rebuild`.

8. **Aggressive game-time changes.** Ticks are scheduled in *game hours* via the
   engine's `gametime`; emotes in *rounds*. If you change `RoundSeconds` or the
   game-time calendar so an hour passes very fast or very slow, scale
   `TickEveryGameHours` / `EmoteEveryRounds` to taste. (A tick cadence far above a
   spec's `decayrate` is also safe — the module re-asserts mutators every tick —
   but between ticks a long-lived storm may briefly flicker as the safety-net decay
   fires.)

9. **A calendar that isn't 12 months.** The shipped tracks map seasons onto
   1-based months of the **stock 12-month calendar**, and every track is
   validated against your world's real calendar at load: a track whose months
   don't exactly cover `1..N` (no gaps, no out-of-range months) is **rejected
   with a logged warning**. On an 8-month or 14-month calendar the shipped
   tracks therefore fail validation and **seasons switch off entirely** —
   weather runs exactly as v1, same as having no usable calendar. This is
   deliberate fail-soft: a silently misaligned winter is worse than no seasons.
   **Authoring note:** a custom calendar needs custom tracks — write `months:`
   lists that cover its real month numbers and the module takes them from
   there.

10. **Claiming the `season-` namespace** (the seasonal half of #4). Worth calling
    out on its own because seasons add a whole second namespace of long-lived
    zone mutators: any `season-*` mutator you place by hand is reconciled away on
    the next tick or rebuild, and a zone whose biome loses its track binding has
    its `season-*` mutator removed automatically (the layer self-heals).

11. **`seasonal:` blocks keyed to a season no track defines.** A per-season
    variant in a weather emote table is matched by season *name*. If you add a
    `seasonal: { harvest: ... }` block but no track ever resolves to a season
    named `harvest`, those lines are simply unreachable — no error, no warning,
    just dead prose. Keep variant keys in sync with your tracks' season names.
    (Our shipped-data tests enforce this for the bundled tables.)

12. **Custom biomes are weather-bland *and* season-silent until bound.** The flip
    side of #5, stated for seasons: a custom biome with no climate file gets the
    `default` profile, which has **no track**, so it never speaks with a season's
    voice. Binding it is one climate file with a `track:` key — the same file that
    fixes its bland weather.

13. **World scripts keyed on ZONE weather mutators.** This one is a behavior
    change on upgrade to M4, not a customization: in the room modes (`occupied`
    — the default — and `all`) zone configs carry **no** weather mutators at
    all; the `weather-<type>` (and `weather-<type>-indoor`) mutators live on
    individual rooms. A script that inspects the zone's mutator list to ask "is
    it storming here?" silently sees calm forever. Check the *room's* mutators
    instead — or set `PerRoomRefinement: off`, which restores the zone-level
    contract exactly. (Seasons are unaffected: `season-*` mutators are
    zone-level in every mode.)

14. **Room weather mutators persist in room instance files.** Room mutators are
    part of room state, so a refined room saved to disk keeps its
    `weather-rain` entry. That's how the design works — strays heal *lazily*:
    the engine runs the mutator lifecycle on room load and each round tick, so
    the spec's `decayrate` retires a stale entry within a few game hours, and
    refine-on-entry corrects any room the moment a player walks in. The
    corollary is what happens if you switch modes or **uninstall** mid-storm:
    switching to `off` strips occupied rooms immediately and leaves the rest to
    the decay safety net; removing the module entirely leaves `weather-*`
    entries whose specs no longer exist — inert (nothing to render) and
    harmless. Nothing to clean by hand; just don't be surprised by leftovers
    in room instance files.

15. **`PerRoomRefinement: all` on a big world.** `all` exists for worlds whose
    scripts read *unoccupied* rooms' mutators, and it pays for that by
    force-loading **every room in every zone on every weather tick**. Once the
    world is over the engine's room-memory threshold, that means continuous
    room load/save disk churn. `occupied` is the default for a reason — it
    never force-loads anything and players can't tell the difference.

## Development & testing

The repo splits along an engine boundary:

```
sim/        pure simulation core — graph, fronts, climate, Step()        (no engine imports)
crawler/    pure geography crawler — exits → zone-adjacency graph         (no engine imports)
content/    pure data-file layer — climate + emote YAML (incl. seasonal)  (no engine imports)
seasons/    pure season resolver — tracks → effective-climate transform   (no engine imports)
engine/     the engine adapter — the ONLY package calling internal/* world APIs
weather*.go module root — plugin lifecycle, tick loop, commands, exports
files/      shipped data: config key reference (doc-only overlay), mutator specs, emote tables, season tracks
```

Pure packages are tested standalone, no server needed:

```sh
go test ./sim/... ./crawler/... ./content/... ./seasons/...
```

(Not `go test ./...` — the engine-coupled packages only compile inside a GoMud
checkout.) Architecture tests fail the build if a pure package ever imports
`internal/*`. For the engine-coupled packages, mirror the module into a checkout
and test there:

```powershell
pwsh scripts/sync-to-checkout.ps1 -Checkout <path-to-GoMud-checkout>
# then, from the checkout:
go test ./modules/weather/...
```

(`sync-to-checkout.ps1` is a **development** tool for iterating on this repo
against a live engine. It is not an installation mechanism — operators install
through GoMud's module manager as described above.)

CI (`.github/workflows/ci.yml`, the badge at the top) runs the same split on
every push to `main` and every pull request: a **standalone** job (the four pure packages,
gofmt/vet/`-race`) and an **engine-coupled** job that clones upstream GoMud
*master*, syncs the module in (mirroring `sync-to-checkout.ps1`), and
builds/vets/tests it there. The engine-coupled job deliberately tracks a
moving target as an **upstream-drift early warning**: red there while
standalone stays green usually means upstream moved an engine API the adapter
binds to — that's signal, not noise; patch `engine/` against the new upstream.

`CONTRIBUTING.md` covers the module/engine ownership boundary, the OOBE
requirement, architecture rules, and the boot smoke-test checklist. Each Go
package carries a `context.md` describing its responsibilities in detail.

## Roadmap

- **M4 — polish & default content: done.** Per-room refinement with indoor
  mutator variants, the module's own gentle weather buffs,
  `BuffOverrides.<type>`, `ExcludeZonePatterns`, per-biome emote variants
  across the stock biomes, admin page validation and typed inputs, this
  builder guide, and CI.
- **v2 remainder:** `PrevailingWind`, per-biome *seasonal* mutator variants, a
  configurable seasonal-ambience cadence, a configurable indoor-biome set, and
  a weather/seasons GMCP package for client-side rendering.
- **Registry version bump** — a one-time tarball + `plugins.New` version bump and
  registry PR now that weather, seasons, and polish are feature-complete.

## License

[GPLv3](LICENSE), matching the GoMud engine.
