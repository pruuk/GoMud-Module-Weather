# GoMud Weather Module — Design Spec

- **Status:** Draft for community review
- **Date:** 2026-06-08
- **Target engines:** GoMud (upstream) and DOGMud (fork) — see [Portability](#13-portability--backporting-gomud--dogmud)
- **Module name:** `weather` (registry id), compiled in under `modules/weather/`
- **Authors:** Calabe Davis + GoMud community
- **Companion lineage:** Built in the same spirit as the [GoMud Module Playtest Harness](https://github.com/GoMudEngine/GoMud-Module-Playtest-Harness) — engine-native, compiled-in, data-driven, testable in isolation.

---

## 1. Purpose & Vision

Give a GoMud world a living atmosphere: weather that is *spatially coherent* (a storm is somewhere, not everywhere), *moves* across the world over time, *responds to the terrain it crosses*, and *expresses itself* through the engine's existing room-mutation machinery — without hard-coding a single line of prose into the engine.

The headline behavior we are building toward:

> A storm forms over the coast, rolls inland across the plains gathering strength, climbs into the mountains where the terrain bleeds it dry, and dissipates on the far side — and players standing in each zone along the way *feel it arrive, pass, and leave*, with flavor and mechanics appropriate to where they are.

We achieve this without inventing a new effects system. GoMud already has **mutators** — temporary, decaying room modifications that can change descriptions, alerts, light level, PvP state, and apply buffs. The weather module is, at its core, an **orchestrator**: it decides *which weather is where*, and translates that into *which mutator is applied to which zone*. The engine does the rest.

### What makes this design worth the effort

- **Spatially coherent & emergent.** Weather is modeled as discrete, named **systems (fronts)** that walk a graph of the world's geography. Storms have a location and a trajectory, not just a per-room dice roll.
- **A real biome feedback loop.** Terrain doesn't just *receive* weather; it *shapes* it. Mountains weaken systems; oceans and plains can feed them. (Section 7.4.)
- **Zero prose in the engine.** The module sets *state*; presentation is a fully overridable data layer of emote tables keyed by `(biome, weather type, indoor/outdoor)`. A MUD's builders write their own ambiance; we ship sensible defaults.
- **Reproducible.** The simulation core is a pure function driven by a seedable RNG, so a given seed + world produces the same weather history. Invaluable for debugging, for tests, and for the AI-tester lineage this project comes from.
- **Portable by construction.** A ports-and-adapters split keeps all engine-specific calls in one thin file, so the same module drops into both GoMud and DOGMud.

---

## 2. Scope: What v1 Delivers / What It Doesn't

### 2.1 Delivers (v1)

| Capability | Notes |
|---|---|
| **Geography crawler** | Builds a zone-adjacency graph from the live world by walking room exits. Cached to disk; rebuildable on demand. |
| **Traveling weather systems** | Discrete fronts that spawn, move zone-to-zone along the graph, intensify/decay, and die. |
| **Biome-aware weather** | Per-biome **climate profiles** decide which weather types are valid and how likely (desert ≠ tundra). |
| **Biome ⇄ weather feedback** | Terrain modifies a passing front's intensity/moisture/speed (mountains sap, oceans feed). |
| **Mutator-based application** | Weather state applied via zone-wide and/or per-room mutators (descriptions, alerts, light, buffs). |
| **Curated, overridable default buffs** | A small, sensible default set (e.g. storm → movement/accuracy penalty, fog → reduced sight, blizzard → cold tick). Every default is toggle-able and replaceable. |
| **Configurable ambient emotes** | Default ambient message tables per `(biome, weather, indoor/outdoor)`; builders override/extend freely. Indoor variants ("rain drums on the roof"). |
| **Persistence + seeded RNG** | Active fronts and RNG state saved on shutdown, restored on boot. Reproducible runs. |
| **Admin/observability commands** | Inspect current weather, list active fronts, force-spawn/clear weather, rebuild the graph. |
| **Exported API** | Other modules and JS scripts can query "what's the weather in zone X?". |
| **Config surface** | All knobs under `Modules.weather.*` with safe defaults; degrades gracefully if optional deps are missing. |

### 2.2 Explicitly does NOT deliver (v1)

| Out of scope (v1) | Where it goes |
|---|---|
| **Seasons** | Designed-for seams now; full implementation is **v2** (Section 11). |
| **Per-room weather granularity** | Simulation is at **zone** granularity. Within-zone variation is handled at *application* time (biome/indoor variants), not simulated. |
| **Authoritative prose / lore** | We ship defaults; we do not write a world's voice. Builders own ambiance. |
| **Client-side weather rendering / GMCP weather package** | Listed as a future enhancement (Section 12). |
| **Wind as a first-class simulated field** | A *prevailing wind bias* exists as a movement knob; full wind/pressure simulation is deferred (Section 12). |
| **Temperature as a continuous physical model** | Weather types carry coarse "cold/hot" tags for effects; no continuous thermodynamics. |
| **Astronomical/tidal systems, moon phases** | Out of scope. (DOGMud has its own moon lore; the module exposes seams but does not own it.) |

The discipline here is deliberate: **YAGNI**. Everything above the line is needed to deliver the headline behavior; everything below it is either a clean v2 or a "nice someday" that the architecture leaves room for.

---

## 3. Alternatives Considered

This section records the forks we evaluated and *why* we chose what we did, so reviewers can challenge the reasoning rather than re-derive it.

### 3.1 Simulation fidelity

| Option | Description | Verdict |
|---|---|---|
| **Atmospheric layer** | Per-region weather states evolving via probability; mostly cosmetic. Cheapest. | Rejected — no genuine *movement*; weather is "ambient noise," not systems you can name and track. |
| **Traveling systems** ✅ | Discrete fronts that move across a geography graph. | **Chosen.** Delivers the headline behavior; still tractable; persists and reproduces cleanly. |
| **Full climate sim** | Traveling systems + seasons + continuous climate + persistence, all at once. | Rejected for v1 — over-scoped. Seasons split out to v2; the rest is exactly "traveling systems." |

### 3.2 Geography graph granularity (what is a node?)

| Option | Verdict |
|---|---|
| **Zone = node** ✅ | **Chosen.** Deterministic, no clustering heuristic, maps directly onto `ZoneConfig` + zone-wide mutators. "Regions" emerge as graph neighborhoods. Fronts are tokens walking zone adjacency. |
| **Clustered regions** | Rejected for v1 — needs a clustering heuristic (exit density / biome similarity) that adds tuning surface for marginal benefit. Can be layered later as an optional "region view" over the zone graph. |
| **Room-level grid** | Rejected — finest movement but heaviest to compute/tune, and the engine's zone-wide mutator support makes zone granularity the path of least resistance. |

### 3.3 Orchestration model (how do fronts live & move?)

| Option | Description | Verdict |
|---|---|---|
| **Centralized tick orchestrator + explicit fronts** ✅ | One coarse weather clock; each tick advances/moves/spawns explicit `Front` objects, then diffs desired vs. applied mutators. Pure-function core. | **Chosen.** Only model that cleanly delivers *traveling* systems, *reproducibility* (seeded), *persistence* (fronts are serializable), and *testability* (pure core). |
| **Per-zone cellular automaton** | Each zone evolves via its biome's Markov chain + neighbor influence; storms *emerge* from spatial correlation. | Rejected as the v1 backbone — elegant and emergent, but storms are diffuse, hard to steer, and awkward to name/persist as units. **We borrow its neighbor-influence idea** as an optional enrichment to front spawning/steering (Section 7.4 / 12). |
| **Mutator-native, near-zero orchestrator** | Self-chaining weather mutators (`clear→cloudy→rain→clear` via `DecayIntoId`/`RespawnRate`); seed zones occasionally. | Rejected — each room/zone evolves *independently*; nothing travels. Undershoots chosen fidelity. (We still use `DecayIntoId`/`DecayRate` as a *safety net* — see 9.2.) |

### 3.4 Biome ↔ weather coupling

| Option | Verdict |
|---|---|
| **Biome-aware, bidirectional** ✅ | **Chosen.** Biome → weather (spawn likelihoods) *and* weather ← biome (terrain modifies passing fronts). Closes the loop the headline behavior needs. |
| **Outdoor-only, biome-agnostic** | Rejected — produces snow-in-the-desert and a flat world. |
| **Apply everywhere, indoor included** | Rejected as a default — but indoor rooms are *not* skipped; they get **indoor-variant** presentation (you hear the storm), per the builder feedback that shaped this design. |

### 3.5 Persistence & determinism

| Option | Verdict |
|---|---|
| **Persist + seeded RNG** ✅ | **Chosen.** Survives reboots and is exactly replayable. |
| **Ephemeral, re-seed on boot** | Rejected — weather "teleports" on restart; not reproducible. |
| **Persist, non-deterministic RNG** | Rejected — survives reboots but can't replay a run; loses the debugging/testing win. |

---

## 4. Architecture Overview

### 4.1 Three sub-projects

The work decomposes into three independently-specced, independently-buildable pieces. This is a many-sessions effort; each gets its own spec → plan → implementation cycle.

```
 ┌─────────────────────┐     graph      ┌──────────────────────┐    desired state   ┌───────────────────────┐
 │ 1. Geography Crawler │ ─────────────▶ │ 2. Weather Sim Core  │ ─────────────────▶ │ 3. Engine Integration │
 │  (zone adjacency)    │   (cached)     │  (pure, seeded)      │  (zone → weather)  │  + Presentation       │
 └─────────────────────┘                └──────────────────────┘                    └───────────────────────┘
        reads world                          no engine imports                          mutators / emotes /
        via adapter                          100% unit-testable                          buffs / commands
```

1. **Geography Crawler** (Section 6) — walks room exits to produce a zone-adjacency graph. Self-contained; knows nothing about weather. **Built first.**
2. **Weather Simulation Core** (Section 7) — the `sim/` package. Fronts, climate profiles, the feedback loop, the tick function. Pure Go, no engine imports, seeded RNG. **Built second.**
3. **Engine Integration & Presentation** (Sections 8–9) — the `engine/` adapter, mutator application, emote tables, buffs, persistence, config, commands. Ties the sim to the live MUD. **Built last.**

### 4.2 Ports & adapters (the portability seam)

```
modules/weather/
├── weather.go            # plugins.New("weather", ...); wiring only. Registers
│                         # event listeners, owns lifecycle, delegates to sim+engine.
├── sim/                  # PURE simulation. NO engine imports whatsoever.
│   ├── graph.go          #   Graph type (consumed from crawler output)
│   ├── front.go          #   Front: Id, Type, ZoneId, Intensity, Moisture, Age, Heading
│   ├── climate.go        #   ClimateProfile, WeatherType, WeatherInfluence
│   ├── tick.go           #   Step(state, world-readonly-view, rng) -> nextState + StateDiff
│   └── rng.go            #   seedable PRNG wrapper (deterministic)
├── crawler/              # Geography crawler. Imports the engine adapter, not sim.
│   └── crawl.go
├── engine/               # The ONLY package that imports internal/rooms, /mutators, /events.
│   ├── adapter.go        #   implements sim's WorldView + the Applier interface
│   ├── apply.go          #   StateDiff -> mutator Add/Remove on zones/rooms
│   ├── emotes.go         #   ambient emote scheduling
│   └── clock.go          #   maps NewRound/DayNightCycle -> weather ticks
├── files/
│   ├── data-overlays/
│   │   └── config.yaml   # Modules.weather.* defaults (overrides base config.yaml)
│   └── datafiles/
│       ├── climate/      # climate profiles per biome (module-owned data)
│       ├── weather/      # weather type definitions
│       ├── mutators/     # weather mutator specs (engine MutatorSpec schema)
│       ├── buffs/        # curated default buff specs (optional, toggle-able)
│       └── emotes/       # ambient emote tables
└── README.md
```

**The contract:** `sim/` defines interfaces it needs (a read-only `WorldView`: zones, adjacency, per-zone biome) and the output it produces (`StateDiff`: which zones changed to which weather). `engine/` implements `WorldView` against the live engine and consumes `StateDiff` to drive mutators. `sim/` never imports `internal/*`. **Backporting = re-point/patch `engine/` only.**

### 4.3 Data flow per tick

```
DayNightCycle/NewRound event
        │
        ▼
engine/clock.go decides "is it a weather tick?" (every N game-hours)
        │  builds read-only WorldView snapshot (zones, biomes, adjacency from cached graph)
        ▼
sim.Step(prevState, worldView, rng)  ──►  (nextState, StateDiff)
        │                                        │
        │ persist nextState (fronts + rng)       ▼
        ▼                                 engine/apply.go
   plugin.WriteBytes                      • for each changed zone: zoneConfig.Mutators.Remove(old); .Add(new)
                                          • refresh per-room indoor/biome variant where needed
                                          • (emotes scheduled separately on a faster cadence)
```

---

## 5. Engine Integration Points (verified against source)

These are the concrete APIs the `engine/` adapter binds to. Verified in the DOGMud checkout (paths relative to repo root); upstream GoMud matches today.

### 5.1 Module/plugin lifecycle
- `plugins.New("weather", version)` → `*Plugin`.
- `plugin.AttachFileSystem(embed.FS)` — embed `files/`.
- `plugin.AddUserCommand(name, handler, allowWhenDowned, adminOnly)` — admin commands.
- `plugin.Callbacks.SetOnLoad(fn)` / `SetOnSave(fn)` — lifecycle hooks.
- `plugin.WriteBytes(id, []byte)` / `plugin.ReadBytes(id) ([]byte, error)` — state persistence (`internal/plugins/plugins.go:315,341`).
- `plugin.ExportFunction(stringId, fn)` — expose `GetWeather(zone)` etc. to other modules/JS (`plugins.go:229`).

### 5.2 Events (`internal/events`)
- `events.RegisterListener(events.NewRound{}, handler[, priority])` — `NewRound{RoundNumber, TimeNow}` for cadence.
- `events.DayNightCycle{IsSunrise, Day, Month, Year, Time}` — emitted by `internal/hooks/NewRound_CheckNewDay.go`; our coarse clock + season seam.
- Listener returns `events.Continue` (weather is observational; it does not cancel events).

### 5.3 World/rooms (`internal/rooms`)
- `rooms.GetAllZoneNames() []string`, `rooms.GetAllZoneRoomsIds(zone) []int` — enumerate the world.
- `rooms.GetZoneConfig(zone) *ZoneConfig`; `ZoneConfig.Mutators` is a **zone-wide** `MutatorList` (`rooms.go:2566` merges zone + room mutators when rendering). **This is the primary application target.**
- `rooms.GetZoneBiome(zone) string`; `Room.Biome` (`rooms.go:88`, comment: *"Used for weather generation."*); `Room.GetBiome()` resolves room→zone→default.
- `Room.Exits map[string]exit.RoomExit` (`rooms.go:90`); `exit.RoomExit{RoomId, Secret, Lock}` — the crawler's traversal edges.
- `rooms.GetRoomsWithPlayers() []int` — lets us prioritize/limit live application work.

### 5.4 Mutators (`internal/mutators`)
- `MutatorSpec` fields we use: `MutatorId`, `NameModifier`/`DescriptionModifier`/`AlertModifier (*TextModifier)`, `PlayerBuffIds`/`MobBuffIds`/`NativeBuffIds ([]int)`, `LightMod (int, -2..2)`, `RegenMultiplier (float64)`, `Pvp`, `DecayRate (string)`, `RespawnRate (string)`, `DecayIntoId (string)`.
- `MutatorList` methods: `Add(name) bool`, `Remove(name) bool`, `Has(name) bool`, `GetActive()`, `Update(roundNow)`.
- `RespawnRate` supports **sun-relative** timing (`sunrise`/`sunset`/`noon`/`midnight`) — useful for diurnal weather and the season seam.

### 5.5 Game time (`internal/gametime`)
- `gametime.GetDate() GameDate` (`gametime.go:144`) → `{Day, Month, Year, Night, ...}`; `gametime.MonthName(month)` (`months.go:20`) — the **season clock** (v2) derives from `Month`.

> **Divergence note:** An earlier `internal/mutators/context.md` summary described a *different* `MutatorSpec` (with `TextModifiers map`, `SpawnChance`, `Requirements`, no buffs). The **actual code** has the richer struct above. Trust source over summaries; the adapter pins us to the real API.

---

## 6. Sub-Project 1 — Geography Crawler

### 6.1 Responsibility
Produce a **zone-adjacency graph**: nodes = zones, edges = "a room in zone A has an exit to a room in zone B." Knows nothing about weather.

### 6.2 Algorithm
1. `zones := rooms.GetAllZoneNames()`.
2. For each zone, `roomIds := rooms.GetAllZoneRoomsIds(zone)`; resolve each room and read `room.Exits`.
3. For each exit, find the **target room's zone** (load target room → `room.Zone`). If `targetZone != sourceZone`, record an undirected edge `sourceZone ↔ targetZone`, accumulating a **weight = count of distinct connecting exits** (a proxy for how "wide" the border is — later usable for movement probability).
4. Record per-zone metadata: `biome` (`GetZoneBiome`), room count, whether the zone has any outdoor rooms (for later application decisions).
5. Emit a `Graph{ Nodes: map[zoneName]ZoneNode, Edges: []Edge }` and cache it.

### 6.3 Design choices
- **Exit semantics:** Secret/locked exits still count as adjacency (weather doesn't care about locks). Configurable flag `IncludeSecretExits` (default `true`).
- **One-way exits:** Recorded as directed in raw form but the weather graph treats adjacency as **undirected** for movement (a front can move either way across a border). The directionality is retained in the cache for future use.
- **Disconnected components:** Expected and fine — islands/planes get independent weather. The crawler reports component count for sanity.
- **Ephemeral/instanced zones:** **Excluded** (they are transient copies; weather on a template zone is enough). Detected via naming/instance markers; configurable `ExcludeZonePatterns`.
- **Cost & timing:** The crawl loads many rooms, so it runs **once at boot** (after world load) and writes a cache; thereafter it loads from cache. An admin command (`weather rebuild`) re-runs it. Optional: skip the boot crawl entirely if a fresh cache exists.

### 6.4 Output / cache format
A versioned JSON/YAML file via `plugin.WriteBytes("geography.json", …)`:

```json
{
  "version": 1,
  "builtAtRound": 12345,
  "nodes": {
    "Frostfang": { "biome": "tundra", "rooms": 24, "hasOutdoor": true },
    "Saltmarsh": { "biome": "swamp",  "rooms": 18, "hasOutdoor": true }
  },
  "edges": [
    { "a": "Frostfang", "b": "Saltmarsh", "weight": 3 }
  ],
  "components": 2
}
```

### 6.5 Acceptance criteria
- Deterministic given a fixed world (no RNG in the crawler).
- Graph round-trips through cache without loss.
- `weather graph <zone>` prints neighbors + weights for spot-checking.
- Unit-tested against a synthetic room set (no live server needed) via the `WorldView` interface.

---

## 7. Sub-Project 2 — Weather Simulation Core (`sim/`)

The heart of the module. **Pure Go. No engine imports.** Everything it needs from the world arrives through a read-only interface; everything it produces leaves as plain data.

### 7.1 Interfaces (the seam)

```go
// sim/world.go — implemented by engine/ for the live MUD, and by tests with fakes.
type WorldView interface {
    Zones() []ZoneId
    Neighbors(z ZoneId) []Edge        // {To ZoneId, Weight int}
    Biome(z ZoneId) BiomeId
    HasOutdoor(z ZoneId) bool
}

// sim/tick.go — the pure step function.
func Step(prev State, world WorldView, rng *RNG, now Clock) (State, StateDiff)
```

`State` = all active fronts + per-zone current weather + RNG cursor. `StateDiff` = the set of `(zone, oldWeather→newWeather)` changes this tick (what the engine must apply).

### 7.2 Core types

```go
type WeatherType string // "clear","overcast","rain","storm","fog","snow","blizzard","dust","heatwave", …

type Front struct {
    Id        FrontId
    Type      WeatherType
    Zone      ZoneId        // where the front's center currently is
    Intensity float64       // 0..1; <=0 means death
    Moisture  float64       // 0..1; fuel for precipitation; drained by dry terrain & raining
    Age       int           // ticks alive
    MaxAge    int           // soft cap; older fronts decay faster
    History   []ZoneId      // recent path (bounded) — prevents immediate backtracking, enables "passing" emotes
}
```

### 7.3 Climate profiles (biome → weather; module-owned data)

`files/datafiles/climate/<biome>.yaml`:

```yaml
biome: tundra
# Which weather types can spawn/exist here and their base weights.
weather:
  clear:    5
  overcast: 4
  snow:     6
  blizzard: 2
  fog:      2
# Feedback loop — how THIS terrain modifies a front passing THROUGH it (Section 7.4).
influence:
  intensityDelta:   -0.05   # tundra slowly chills/weakens most systems
  moistureDelta:    -0.02
  movementResistance: 0.2   # 0..1; higher = front lingers longer
# Optional spawn pressure: how often new fronts originate here.
spawnWeight: 1.0
```

A biome with no profile falls back to a built-in `default` profile (mild, low-variance). Climate profiles are **pure data we ship and builders override** — the engine's `BiomeInfo` is *not* extended, keeping us decoupled.

### 7.4 The biome ⇄ weather feedback loop

Two directions, both data-driven:

1. **Biome → weather (spawn side).** When a new front is born (or a front's type re-rolls), the candidate weather types and weights come from the *origin zone's* climate profile. Deserts birth dust/clear; coasts birth rain/storm.
2. **Weather ← biome (front dynamics).** Each tick, the front reads the `influence` block of the **biome of the zone it currently occupies** and applies:
   - `Intensity += intensityDelta` (terrain saps or feeds the system),
   - `Moisture += moistureDelta`,
   - movement is damped by `movementResistance`.

Worked example — *a storm crosses a mountain range:*
- Plains (`intensityDelta: +0.02`, low resistance): the storm rolls fast, holds strength.
- Foothills/mountain (`intensityDelta: -0.15`, `moistureDelta: -0.10`, high resistance): each tick it loses intensity and dumps moisture (heavy precip), and it *lingers* (slow movement) so the loss compounds.
- By the far side its `Intensity <= 0` → the front dies. The leeward zones see only its weakened tail.

This is exactly the loop requested: terrain is not a passive recipient. *(Optional v1.1 touch, deferred: spike precipitation in the windward zone specifically — "orographic" rain — before the decline. Section 12.)*

### 7.5 The tick algorithm (`Step`)

Per weather tick (coarse clock; see 9.3):

1. **Age & terrain feedback.** For each active front: `Age++`; apply current zone's `influence` to `Intensity`/`Moisture`; apply age-based decay past `MaxAge`.
2. **Movement.** Each front *may* move to a neighbor. Probability and target are chosen from: edge weights (wider borders favored), `movementResistance` (high resistance ⇒ stays), an optional **prevailing-wind bias** (config: a preferred compass-ish direction expressed as a per-edge bias), and a "don't immediately backtrack" rule using `History`. Movement consumes the RNG deterministically.
3. **Type evolution.** A front's `Type` can shift toward what the *new* zone's climate profile favors and what its `Moisture`/`Intensity` support (e.g. `storm`→`overcast` as it weakens; `rain`→`snow` entering tundra). Bounded by a transition table so changes read naturally.
4. **Death.** Fronts with `Intensity <= 0` (or `Age > hardCap`) are removed.
5. **Spawning.** Maintain a global **front budget** (config `MaxActiveFronts`). If under budget, with probability scaled by total `spawnWeight`, spawn a new front in a weighted-random zone using that zone's climate profile for its type.
6. **Resolve per-zone weather.** For each zone, compute the dominant weather from the front(s) present (highest intensity wins; ties broken deterministically). Zones with no front resolve to a **calm baseline** drawn occasionally from their climate profile's low-intensity types (so calm zones aren't dead — light fog, drifting clouds).
7. **Diff.** Compare resolved per-zone weather to `prev`; emit a `StateDiff` of changes only.

All randomness flows through the injected `*RNG` (7.6), so `Step` is a pure function of `(prev, world, rng, now)`.

### 7.6 Determinism & RNG
- A single seedable PRNG (e.g. PCG/`math/rand/v2` with a fixed seed) lives in `State` and is serialized with it.
- The seed is config (`Seed`, default derived from world name so two worlds differ but each world is stable).
- Property the tests assert: *same seed + same world + same tick count ⇒ identical front history and per-zone weather.*

### 7.7 Acceptance criteria
- Pure: `sim/` compiles with **zero** `internal/*` imports (enforced by a test that greps imports / an architecture test).
- Reproducible: golden-trace test (seed → N ticks → recorded per-zone weather) is stable.
- Feedback verified: a scripted "storm over a mountain" world shows monotonic intensity decline + death (Section 7.4).
- Budget respected: never exceeds `MaxActiveFronts`.
- No NaN/!runaway: intensity/moisture clamped to `[0,1]`.

---

## 8. Sub-Project 3a — Engine Adapter (`engine/`)

The **only** package importing `internal/rooms`, `internal/mutators`, `internal/events`, `internal/gametime`.

- **`WorldView` implementation.** Wraps the cached geography graph + live `GetZoneBiome` to satisfy `sim.WorldView`. Built once per tick as an immutable snapshot so the sim sees a consistent world.
- **`StateDiff` application** (Section 9.1).
- **Clock** (Section 9.3).
- **Emote scheduler** (Section 9.4).

Because everything engine-shaped lives here, a future GoMud/DOGMud API drift is a localized patch.

---

## 9. Sub-Project 3b — Presentation & Application

### 9.1 Applying weather via mutators

Each `(weatherType)` maps to one or more **weather mutator specs** shipped in `files/datafiles/mutators/`. Application strategy:

- **Primary: zone-wide.** For a changed zone, `GetZoneConfig(zone).Mutators.Remove(oldWeatherMutator)` then `.Add(newWeatherMutator)`. One call paints the whole zone (engine merges zone mutators into every room at render time, `rooms.go:2566`).
- **Refinement: per-room variants where it matters.** Indoor rooms and biome-divergent rooms within a zone get a variant mutator so an *indoor* room reads "rain drums on the roof overhead" instead of "rain falls around you," and a cave inside a forest zone isn't rained on. Strategy:
  - Mutator naming convention: `weather_<type>` (zone default, outdoor) and `weather_<type>_indoor` (indoor variant).
  - The applier consults `room.GetBiome()` / an indoor flag and applies the variant only to rooms that diverge from the zone default. To bound cost, **per-room refinement is applied lazily** — only to rooms in zones that currently contain players (`GetRoomsWithPlayers()`); unoccupied rooms inherit the zone-wide mutator and are refined on entry. (Configurable: `PerRoomRefinement: occupied|all|off`.)

Example weather mutator spec (`files/datafiles/mutators/weather_storm.yaml`) — real `MutatorSpec` schema:

```yaml
mutatorid: weather_storm
descriptionmodifier:
  behavior: append
  text: "\nRain lashes down and thunder rolls across the sky."
  colorpattern: storm
alertmodifier:
  behavior: append
  text: "A storm rages here."
lightmod: -1               # storms darken the area
playerbuffids: [ 9101 ]    # curated default: "Drenched" — minor move/accuracy penalty (toggle-able)
mobbuffids:    [ 9101 ]
decayrate: "6 hours"       # SAFETY NET: self-clears if the orchestrator ever misses removal
decayintoid: weather_overcast   # graceful fade rather than a hard cut
```

### 9.2 Why mutator self-decay is a safety net, not the engine
The orchestrator is authoritative: it adds/removes weather mutators as fronts move. But we *also* set `DecayRate`/`DecayIntoId` so that if the module is disabled mid-run, crashes, or misses a cleanup, rooms **heal themselves** to a calm state instead of being stuck in an eternal storm. Defense in depth.

### 9.3 The weather clock (cadence)
- Weather ticks are **coarse** — they must not fire every combat round. Default: **once per in-game hour** (configurable `TickEveryGameHours`, default `1`), derived from `DayNightCycle`/`NewRound` + `gametime.GetDate()`.
- Rationale: fronts moving zone-to-zone every game-hour gives weather that visibly changes over a play session without thrashing mutators or spamming players.
- The faster **emote** cadence is separate (9.4).

### 9.4 Ambient emotes (the "something to listen to" layer)
This is the builder-facing heart of presentation, shaped directly by the design feedback that *even indoors you should hear the weather*, and that **builders should own the prose**.

- **Emote tables** live in `files/datafiles/emotes/`, keyed by `(weatherType, biome, indoor|outdoor)`, each a weighted list of messages. We ship defaults; builders override per their world's voice.
- A separate, lighter scheduler emits an ambient line into occupied rooms on a configurable interval (`EmoteEveryRounds`, default e.g. 15–30 rounds, jittered), choosing from the table that matches the room's current weather/biome/indoor state.
- **Two delivery modes** (config `EmoteMode`):
  1. `module` — the module emits the messages directly on its own schedule (batteries included).
  2. `tag-only` — the module only ensures the room carries a weather **state tag/alert** (via the mutator) and emits *nothing*; the world's own room scripts/triggers react. This honors the builder who wants the module to "just give the room something to listen to."
- Default: `module`, so it works out of the box; serious worlds switch to `tag-only`.

Example emote table (`files/datafiles/emotes/storm.yaml`):

```yaml
weather: storm
outdoor:
  default:
    - "A blinding fork of lightning splits the sky."
    - "Thunder cracks directly overhead."
  forest:
    - "Wind tears at the branches; the whole canopy roars."
indoor:
  default:
    - "Rain hammers against the windows."
    - "Thunder rattles the walls; the timbers groan."
```

### 9.5 Curated default buffs
- Shipped in `files/datafiles/buffs/` (engine buff schema), referenced by id from weather mutators.
- Default set is intentionally small and gentle, e.g.:
  - **Drenched** (rain/storm): minor movement & ranged-accuracy penalty.
  - **Blinded by snow** (blizzard): reduced perception/sight range.
  - **Chilled** (blizzard/cold): small periodic stamina drain.
  - **Sweltering** (heatwave): faster stamina drain on exertion.
- Every default is **toggle-able** (`Buffs.Enabled`, and per-type overrides). Worlds can point a weather type at their own buff ids instead. We ship *defaults*, not *opinions*.

### 9.6 Admin & observability commands
- `weather` — current weather where you stand + the front (if any) affecting your zone.
- `weather zones` — table of all zones and current weather.
- `weather fronts` — active fronts: id, type, zone, intensity, age, heading.
- `weather spawn <type> <zone> [intensity]` — force a front (admin/testing).
- `weather clear [zone]` — clear weather in a zone (or everywhere).
- `weather graph [zone]` — graph neighbors/weights for a zone (crawler spot-check).
- `weather rebuild` — re-run the geography crawl.
- All admin-gated (`adminOnly` on `AddUserCommand`).

### 9.7 Exported API (for other modules & scripts)
Via `plugin.ExportFunction`:
- `GetWeather(zone string) -> {type, intensity}` — read current weather.
- `GetFronts() -> []FrontSummary` — read active systems.
- `SpawnFront(type, zone, intensity)` — programmatic control (e.g. a quest that summons a storm).
- Exposed to JS as `modules.weather.GetWeather(...)`, mirroring how `follow`/`gmcp` export functions.

---

## 10. Configuration Reference

All keys under `Modules.weather.*`, defaults shipped in `files/data-overlays/config.yaml` (this overlay overrides the base `config.yaml`; do **not** add a `Modules.weather` block to `config-overrides.yaml` — it won't merge, per the playtest module's documented gotcha).

| Key | Default | Meaning |
|---|---|---|
| `Enabled` | `true` | Master switch. |
| `Seed` | `0` (→ derived from world name) | RNG seed for reproducibility. |
| `TickEveryGameHours` | `1` | Weather-simulation cadence. |
| `MaxActiveFronts` | `8` | Global front budget. |
| `SpawnRateScale` | `1.0` | Multiplier on spawn pressure. |
| `PrevailingWind` | `""` | Optional movement bias (e.g. `"east"`); empty = unbiased. |
| `PerRoomRefinement` | `occupied` | `occupied` \| `all` \| `off` — indoor/biome variant granularity. |
| `IncludeSecretExits` | `true` | Crawler counts secret/locked exits as adjacency. |
| `ExcludeZonePatterns` | `["instance_*","ephemeral_*"]` | Zones the crawler skips. |
| `EmoteMode` | `module` | `module` (we emit) \| `tag-only` (builders react to tags). |
| `EmoteEveryRounds` | `20` | Ambient emote cadence (jittered). |
| `Buffs.Enabled` | `true` | Apply curated default buffs. |
| `Buffs.Overrides` | `{}` | Map `weatherType -> []buffId` to replace defaults. |
| `Persist` | `true` | Save/restore fronts + RNG across reboots. |
| `RebuildGraphOnBoot` | `false` | Force a fresh crawl each boot (else use cache if present). |

**Graceful degradation:** if the geography cache is missing and a crawl fails, the module logs a warning and runs in a degraded "single-component, no-movement" mode rather than crashing — matching the playtest module's fail-soft posture.

---

## 11. v2 — Seasons (designed-for, not built)

Seasons are deliberately deferred, but v1's seams make them a *plug-in*, not a rewrite.

### 11.1 Concept
A global (optionally per-hemisphere) **season clock** shifts each biome's climate profile so the *same* terrain produces winter-ish vs. summer-ish weather.

### 11.2 The seams already in place
- **Climate profiles are data with named weight tables.** A season simply supplies a *modifier* over those weights (winter: ×3 `snow`, ×0.2 `heatwave`).
- **The clock already carries the date.** `events.DayNightCycle{Day,Month,Year}` and `gametime.GetDate()` give month → season with no new engine plumbing.
- **`RespawnRate` supports sun-relative timing**, so seasonal/diurnal effects can lean on existing mutator scheduling.

### 11.3 v2 shape
```yaml
# files/datafiles/seasons/seasons.yaml (v2)
calendar:
  winter: { months: [12,1,2] }
  spring: { months: [3,4,5] }
  summer: { months: [6,7,8] }
  autumn: { months: [9,10,11] }
modifiers:
  winter:
    weatherWeightMultipliers: { snow: 3.0, blizzard: 2.0, heatwave: 0.0 }
    influence: { intensityDelta: -0.02 }   # colder, harsher baselines
```
The sim's spawn/type-evolution steps multiply climate weights by the active season modifier before sampling. **No change to `sim.Step`'s shape** — just an extra read-only input (`season`) folded into `Clock`. Everything else (fronts, feedback, persistence, presentation) is unchanged.

### 11.4 v2 open questions
- Hemispheres: single global season vs. north/south split keyed by zone metadata?
- Season-aware emotes (snow-laden trees in winter) — extra key on emote tables.
- Transition smoothing across season boundaries (avoid a hard flip on the 1st of the month).

---

## 12. Future Enhancements (post-v2, "nice someday")

These are intentionally **not** committed; the architecture leaves room for each.

- **Orographic precipitation:** spike windward-zone precip before a front weakens crossing mountains (a richer 7.4).
- **Front interactions:** merging/colliding systems, occlusion, pressure gradients.
- **First-class wind & pressure fields** rather than a single prevailing-wind bias.
- **Clustered-region view:** an optional macro layer grouping zones into named regions over the zone graph (the rejected 3.2 option, as an *additive* feature).
- **GMCP weather package:** push structured weather state to clients for UI/map tinting (mirrors how `gmcp` pushes room/char data).
- **Neighbor-influence enrichment:** borrow the cellular-automaton idea (3.3) so fronts are *steered* toward neighbors already trending stormy — more organic fronts.
- **Weather-driven world hooks:** crops, travel hazards, mob behavior shifts, flooding exits (the `Exits` mutator field already supports temporary passages — e.g. a frozen river becoming crossable).

---

## 13. Portability — Backporting GoMud ⇄ DOGMud

### 13.1 Current state
DOGMud is a **fork** of GoMud (same module path `github.com/GoMudEngine/GoMud`, `upstream` remote → GoMud) and already ports the playtest harness. The APIs this module touches — `mutators`, `rooms`/`ZoneConfig`, `events` (`NewRound`/`DayNightCycle`), `gametime`, `plugins` — **match upstream today**. The portability risk is *future drift*, since DOGMud diverges elsewhere (combat, permadeath sunset, level-up disabled).

### 13.2 Strategy
- **Ports & adapters (Section 4.2).** `sim/` and `crawler/`'s logic are engine-agnostic; **all** engine calls live in `engine/`.
- **Backport procedure:** copy `modules/weather/` into the other engine's `modules/` (same import path → compiles as-is), run `go generate ./... && go build`. If a touched API drifted, fix only `engine/`; `sim/` is untouched.
- **Guardrail:** an architecture test asserts the `sim/` package imports no `internal/*`. (The `crawler/` legitimately reads the live world and so imports the engine; only the simulation core must stay pure.) If someone reaches into the engine from the core, CI fails.
- **Version note in README** (mirroring the playtest module): the module needs an engine with the `mutators` buff fields + `DayNightCycle` event; on older engines it fails soft.

---

## 14. Testing Strategy

| Layer | Approach |
|---|---|
| **Crawler** | Synthetic room/exit fixtures via `WorldView` fakes; assert graph shape, weights, components, cache round-trip. No live server. |
| **Sim core** | Pure unit tests. Golden-trace reproducibility (seed → N ticks). Feedback-loop scenario tests (storm-over-mountain decays & dies). Budget/clamp invariants. Architecture test: no `internal/*` imports. |
| **Engine adapter** | Table tests mapping `StateDiff` → expected mutator `Add/Remove` calls (mutator layer mocked). |
| **Presentation** | Emote-table selection tests (right table for `(weather,biome,indoor)`); `tag-only` mode emits nothing. |
| **Integration (manual / harness)** | Boot a small world, `weather spawn storm <zone>`, walk a front across a mountain chain, observe decay; verify self-heal on module disable. The **playtest harness** is the natural driver here — fitting, given the shared lineage. |

---

## 15. Risks & Open Questions

| # | Risk / Question | Mitigation / Notes |
|---|---|---|
| R1 | Crawl cost on very large worlds (loads many rooms). | One-time at boot + cache; `RebuildGraphOnBoot=false`; consider incremental/streaming crawl if needed. |
| R2 | Mutator thrash if cadence too fast or fronts jitter. | Coarse clock (game-hour default); `DecayIntoId` fades instead of hard cuts; "don't backtrack" rule. |
| R3 | Per-room refinement cost (indoor/biome variants). | Default `occupied`-only; refine on room entry; configurable. |
| R4 | Buff ids collide with a world's existing buffs. | Ship defaults in a reserved id range; document; `Buffs.Overrides` to remap. |
| R5 | Zone biome data sparse/missing in some worlds. | Fallback `default` climate profile; weather still works, just blander. |
| R6 | Open: should disconnected components share a front budget or get per-component budgets? | Leaning global budget for v1; per-component is a tuning follow-up. |
| R7 | Open: prevailing wind expressed as compass vs. per-edge bias, given zones lack coordinates. | v1: simple global bias + optional per-zone hint; full vector wind is a future field. |
| R8 | Config-overlay merge gotcha (documented in playtest module). | Ship defaults in module overlay; document "don't put `Modules.weather` in config-overrides." |

---

## 16. Milestones / Rollout

1. **M1 — Crawler.** Graph build + cache + `weather graph`/`weather rebuild` commands + tests. *Shippable alone (a "zone map" utility).*
2. **M2 — Sim core.** `sim/` package, climate profiles, feedback loop, deterministic tick, golden tests. *No engine effects yet; observable via a dump command.*
3. **M3 — Application & presentation.** Engine adapter, mutator application, emote tables, curated buffs, persistence, full command set, config. *First end-to-end weather.*
4. **M4 — Polish & docs.** Default climate/emote/buff content for common biomes, README + builder extension guide, harness-driven integration pass.
5. **v2 — Seasons.** Per Section 11.

Each milestone is its own spec → plan → implementation cycle; M1–M3 correspond to the three sub-projects.

---

## 17. Extending the Module (builder/dev guide, summary)

A world author can customize **without touching Go**:

- **New biome weather:** drop `files/datafiles/climate/<biome>.yaml` (weights + influence). Unknown biomes fall back to `default`.
- **New weather type:** add a `WeatherType` entry referenced by a climate profile, a matching `mutators/weather_<type>.yaml`, and `emotes/<type>.yaml`. (A new *type* with novel mechanics may need a buff spec.)
- **Reskin prose:** override `emotes/*.yaml` entirely; or set `EmoteMode: tag-only` and react to the weather tag/alert in your own room scripts.
- **Change mechanics:** edit/replace the buff specs, or `Buffs.Overrides` to point weather types at your buffs, or `Buffs.Enabled: false` for pure flavor.
- **Tune the feel:** `TickEveryGameHours`, `MaxActiveFronts`, `SpawnRateScale`, `PrevailingWind`.
- **Programmatic control (other modules/quests):** call exported `SpawnFront`/`GetWeather` (Go) or `modules.weather.*` (JS).

The contract for extenders: **the engine owns weather *state*; you own its *voice* and *consequences*.**

---

## Appendix A — Glossary

- **Zone:** GoMud's room-partition unit (`Room.Zone`); the graph node and primary weather granularity.
- **Front / system:** a discrete, named weather instance with a location, intensity, and trajectory.
- **Climate profile:** module-owned per-biome data: which weather is valid + how terrain modifies passing fronts.
- **Mutator:** GoMud's temporary, decaying room modification (`internal/mutators`) — our application mechanism.
- **Influence:** the biome → front-dynamics half of the feedback loop (intensity/moisture/movement deltas).
- **Tick:** one coarse weather-simulation step (default: per game-hour).
- **Emote table:** weighted ambient messages keyed by `(weather, biome, indoor/outdoor)`.
- **WorldView / StateDiff:** the read-only input / change-set output that keep `sim/` engine-agnostic.
