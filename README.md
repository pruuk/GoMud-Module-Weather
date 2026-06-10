# GoMud Weather Module

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

**Status: weather complete (M3); seasons complete (S1–S3).** The module works
end-to-end on a stock GoMud world: install, run, and storms travel, rooms render
`(storm-wracked)`, ambient lines play indoors and out, state survives reboots,
and the calendar shifts each zone's climate and voice — winter rain reads as
sleet, calm winter rooms get their own quiet ambience. Remaining before a public
release: M4 polish (per-room indoor/biome mutator variants, `Buffs.Overrides`,
full per-biome content) and the one-time module-registry version bump. Two design
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

- **Not per-room weather.** Simulation granularity is the zone. Indoor rooms are
  not rained on — they get indoor *presentation* ("rain drums on the roof") — but
  two outdoor rooms in one zone always share weather.
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
- The stock-world content it reuses by default: buff ids **31** (Freezing Snow)
  and **33** (Thirsty), and the stock color patterns `gray`, `blue`,
  `mute-dblue`, `frost`, `brown`, `embers`. Missing any of these degrades
  gracefully (see *What can break it*).

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
(`Modules.weather.Enabled: true` ships in the module's config overlay): on the
first game round the module crawls your world's zones and exits into a geography
graph, caches it, seeds the simulation from your zone names (stable per world),
binds each biome's season track to the calendar, and starts ticking once per game
hour. No data authoring, no room tagging, no world prep.

### What you'll see in the boot log

```
mutators.LoadDataFiles()  loadedCount=24        ← your world's specs + our 14 (8 weather + 6 season)
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

Weather and seasons show up without anyone running commands, of course: room
names get a tag like `(raining)`, descriptions gain a weather line and a season
line, severe weather adds an alert banner and dims light, and occupied rooms hear
ambient lines every ~20 rounds (indoor rooms get indoor variants).

**Across a season boundary**, a player sees the change accumulate rather than
flash: room descriptions pick up the new `season-*` line on the next render, the
weather odds drift toward the new season over the track's transition window
(`transitionDays`), and the ambient voice changes — in calm winter weather a room
might offer "your breath plumes white and hangs in the frozen air," while the same
room in a summer storm hears the winter-less base storm lines. One ambient line
per room per pass, and **weather always wins** over a quiet seasonal line.

## How it works (one paragraph, plus the seasons transform)

A **crawler** walks every room exit once at boot and reduces your world to a
zone-adjacency graph (zone = node, "rooms in A have exits into B" = weighted
edge). A pure, seeded **simulation** ticks once per game hour: fronts age,
terrain feeds or saps them, they move along edges (wide borders are likelier),
their type drifts toward what the local climate supports, dead ones are removed,
new ones spawn within a budget, and every zone resolves to one weather type
(strong fronts project onto neighboring zones, so a big storm covers an area, not
a point). The **engine adapter** then makes the world match: each zone's
`ZoneConfig.Mutators` gets exactly the right `weather-*` mutator (the engine
merges zone mutators into every room render), and an emote scheduler voices
occupied rooms. State is saved through plugin storage and reconciled on boot.

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

All knobs live under `Modules.weather.*`. Defaults ship in this module's
`files/data-overlays/config.yaml` — to change them, edit that file and rebuild.
**Gotcha (inherited from the engine's overlay mechanics):** a `Modules.weather:`
block in your server's `config-overrides.yaml` will NOT merge; module config
comes from module overlays.

| Key | Default | Meaning |
|---|---|---|
| `Enabled` | `true` | Master switch. Off = module registers nothing but an inert command. |
| `Seed` | `0` | RNG seed. `0` derives a stable seed from your zone names (same world ⇒ same seed; negative values are treated as 0). |
| `TickEveryGameHours` | `1` | Simulation cadence in game hours (minimum 1). |
| `MaxActiveFronts` | `8` | Global front budget (minimum 1). |
| `SpawnRateScale` | `1.0` | Multiplier on spawn pressure. `0` stops new fronts entirely. |
| `EmoteMode` | `module` | `module` = we emit ambient lines (weather and seasonal); `tag-only` = we stay silent and your room scripts react to the weather/season mutators and alerts instead. |
| `EmoteEveryRounds` | `20` | Ambient emote cadence in rounds, jittered ±25% (minimum 5). The seasonal layer fires at ~1-in-3 of these passes, in calm zones only. |
| `BuffsEnabled` | `true` | Apply the curated default buffs carried by weather mutators (blizzard → 31 Freezing Snow, heatwave → 33 Thirsty). `false` strips buff ids from both the weather and season specs at boot. |
| `Persist` | `true` | Save/restore fronts + RNG across reboots. |
| `IncludeSecretExits` | `true` | Crawler counts secret/locked exits as zone adjacency (weather doesn't care about locks). |
| `RebuildGraphOnBoot` | `false` | Force a fresh crawl each boot instead of using the cache. |
| `SeasonsEnabled` | `true` | Master switch for the seasons layer. `false` = weather runs exactly as v1 (no climate shifts, no `season-*` mutators, no `WeatherSeasonChanged` events, no `GetSeason` response, no seasonal ambience). |

Planned but not yet config keys (deferred to M4+): `PrevailingWind`,
`PerRoomRefinement`, `Buffs.Overrides`, `ExcludeZonePatterns` (the crawler
currently always skips `instance_*`/`ephemeral_*` zones), and a configurable
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

- **`files/datafiles/mutators/weather_<type>.yaml`** — one per weather type.
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
  Add a biome key to give, say, forests their own storm lines.
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

The shipped season specs are **description-only** — no buffs, no exits. This is a
deliberate scope decision: zone-wide buffs that persist for an entire ~4-real-day
season are too heavy-handed to ship as defaults (buff 31 deals damage per
trigger; buff 33 is −20 all stats). Transient *weather* buffs from M3 remain as
shipped; season-long buffs wait for `Buffs.Overrides` (M4) so worlds opt in
deliberately. The `BuffsEnabled: false` toggle strips buffs from **both**
namespaces if you want a zero-buff install.

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

2. **Reusing or removing the stock buff ids (31, 33).** The default specs
   reference engine buffs by **numeric id**. If your world deleted buff 31,
   blizzards just apply nothing (harmless). But if you *reassigned* id 31 to
   something else — "Vampiric Frenzy", say — blizzards will cheerfully apply it,
   with no warning, because an id is all the spec knows. If you've renumbered
   buffs, set `BuffsEnabled: false` or edit the two specs.

3. **Replacing the stock color patterns.** The specs color their text with
   `gray`, `blue`, `mute-dblue`, `frost`, `brown`, `embers` from your world's
   `color-patterns.yaml`. Removing/renaming those names just renders the text
   uncolored — cosmetic, but easy to miss.

4. **Claiming the `weather-` or `season-` mutator namespace.** All module mutator
   ids start with `weather-` or `season-`, and the module *enforces* both
   namespaces at runtime: each reconciler removes any live `weather-*` /
   `season-*` zone mutator that doesn't match the simulation's (or the calendar's)
   view. If you hand-author a mutator named `weather-eclipse` or
   `season-festival` and place it on a zone, the module strips it within one tick.
   Use a different prefix for your own mutators. (A duplicate of one of our exact
   ids is caught at boot — the engine logs `duplicate mutator id` and keeps the
   disk version.)

5. **Biome data the module doesn't know.** Zones with no biome, or custom biome
   ids (`crystalwastes`), silently fall back to the mild `default` climate —
   weather still works, just blander and less varied — **and they are
   season-unbound** (no `default`-profile track), so they get no seasonal climate
   shift, no `season-*` mutator, and no seasonal ambience until you ship a climate
   file that names a `track:`. Related: **indoor detection is a biome-id
   heuristic** (`cave`, `underground`, `dungeon`, `indoor`, `tunnel`, `sewer`). A
   custom `cavern` biome is treated as *outdoors* — players in it would see "rain
   patters down around you" — until M4's configurable indoor handling; the
   workaround is using a recognized id or overriding the emote tables.

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
   for a teleport-hub world). Zone names matching `instance_*` or `ephemeral_*` are
   skipped entirely. After adding zones or exits, run `weather rebuild`.

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

## Development & testing

The repo splits along an engine boundary:

```
sim/        pure simulation core — graph, fronts, climate, Step()        (no engine imports)
crawler/    pure geography crawler — exits → zone-adjacency graph         (no engine imports)
content/    pure data-file layer — climate + emote YAML (incl. seasonal)  (no engine imports)
seasons/    pure season resolver — tracks → effective-climate transform   (no engine imports)
engine/     the engine adapter — the ONLY package calling internal/* world APIs
weather*.go module root — plugin lifecycle, tick loop, commands, exports
files/      shipped data: config overlay, mutator specs, emote tables, season tracks
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

`CONTRIBUTING.md` covers the module/engine ownership boundary, the OOBE
requirement, architecture rules, and the boot smoke-test checklist. Each Go
package carries a `context.md` describing its responsibilities in detail.

## Roadmap

- **M4 — polish & default content:** per-room and per-biome indoor mutator
  variants, per-biome *seasonal* mutator variants, `Buffs.Overrides`,
  full per-biome emote/climate coverage for every stock biome, a configurable
  seasonal-ambience cadence, a builder guide, and CI.
- **v2 remainder:** `PrevailingWind`, `PerRoomRefinement`, `ExcludeZonePatterns`,
  and a weather/seasons GMCP package for client-side rendering.
- **Registry version bump** — a one-time tarball + `plugins.New` version bump and
  registry PR now that weather and seasons are feature-complete.

## License

[GPLv3](LICENSE), matching the GoMud engine.
