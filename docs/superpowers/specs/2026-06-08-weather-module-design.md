# GoMud Weather Module — Design Spec

- **Status:** Draft for community review
- **Date:** 2026-06-08
- **Target engines:** GoMud (upstream) and DOGMud (fork) — see [Portability](#13-portability--backporting-gomud--dogmud)
- **Module name:** `weather` (registry id), compiled in under `modules/weather/`
- **Authors:** Calabe Davis + GoMud community
- **Companion lineage:** Built in the same spirit as the [GoMud Module Playtest Harness](https://github.com/GoMudEngine/GoMud-Module-Playtest-Harness) — engine-native, compiled-in, data-driven, testable in isolation.
- **Engine-author input:** Incorporates early guidance from Volte6 (GoMud) on tick scheduling via the `gametime` package and on the existing default-world weather mutators we build on (see §5.5, §9.1, §9.3).

---

## 1. Purpose & Vision

Give a GoMud world a living atmosphere: weather that is *spatially coherent* (a storm is somewhere, not everywhere), *moves* across the world over time, *responds to the terrain it crosses*, and *expresses itself* through the engine's existing room-mutation machinery — without hard-coding a single line of prose into the engine.

The headline behavior we are building toward:

> A storm forms over the coast, rolls inland across the plains gathering strength, climbs into the mountains where the terrain bleeds it dry, and dissipates on the far side — and players standing in each zone along the way *feel it arrive, pass, and leave*, with flavor and mechanics appropriate to where they are.

We achieve this without inventing a new effects system. GoMud already has **mutators** — temporary, decaying room modifications that can change descriptions, alerts, light level, PvP state, and apply buffs. The weather module is, at its core, an **orchestrator**: it decides *which weather is where*, and translates that into *which mutator is applied to which zone*. The engine does the rest.

Critically, every integration point this design needs **already exists in GoMud today** — so **v1 requires no changes to the GoMud engine** (verified, §5.6). That keeps the module entirely in our ownership and off the engine maintainer's review queue, save for a one-time module-registry onboarding.

### What makes this design worth the effort

- **Spatially coherent & emergent.** Weather is modeled as discrete, named **systems (fronts)** that walk a graph of the world's geography. Storms have a location and a trajectory, not just a per-room dice roll.
- **A real biome feedback loop.** Terrain doesn't just *receive* weather; it *shapes* it. Mountains weaken systems; oceans and plains can feed them. (Section 7.4.)
- **Zero prose in the engine.** The module sets *state*; presentation is a fully overridable data layer of emote tables keyed by `(biome, weather type, indoor/outdoor)`. A MUD's builders write their own ambiance; we ship sensible defaults.
- **Reproducible.** The simulation core is a pure function driven by a seedable RNG, so a given seed + world produces the same weather history. Invaluable for debugging, for tests, and for the AI-tester lineage this project comes from.
- **Portable by construction.** A ports-and-adapters split keeps all engine-specific calls in one thin file, so the same module drops into both GoMud and DOGMud.
- **Works out of the box.** Install, set `Enabled: true`, and a stock world has believable weather immediately — no required data authoring, no world prep, default content for the standard biomes. A hard requirement, not a nicety (§2.3).

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
| **Out-of-the-box experience** | Install + one config flag → working weather on a stock world, **no data authoring required**. Default content shipped for the standard biomes. Hard requirement (§2.3). |
| **Zero engine changes** | v1 is implemented entirely against existing GoMud APIs; no PRs to the engine are required to ship it (§5.6). |

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

### 2.3 Out-of-the-box experience (hard requirement)

A direct lesson from the playtest harness: a module that needs hand-holding to start won't get adopted. **Installing this module and enabling it must produce working weather on a stock GoMud world with no further authoring.** This is a first-class requirement, not polish.

The acceptance bar:

1. **Install via the registry** — `go run . module install weather` → `go generate ./... && go build` → run.
2. **One switch.** With `Modules.weather.Enabled: true` (the shipped default), weather is live. No other required config.
3. **No required data authoring.** The module ships default climate profiles, weather mutators, emote tables, and buffs covering the **standard GoMud biomes** (e.g. city, forest, swamp, mountain, desert, tundra, ocean, plains, cave/underground, and `default`). A world with stock biomes gets believable weather immediately.
4. **No world prep.** No builder must tag rooms, define regions, or edit zones. The crawler discovers geography from existing exits; missing biome data falls back to the `default` climate profile.
5. **Graceful degradation, never a crash.** Missing optional deps, a sparse world, or a failed crawl drop features, log a warning, and keep the server healthy — mirroring the playtest module's fail-soft posture.
6. **Clean off switch.** `Enabled: false` (or uninstall) leaves no residue: weather mutators self-heal via `DecayRate`/`DecayIntoId` (§9.2), so rooms return to calm with no manual cleanup.

Tracked as explicit acceptance criteria on milestones M3–M4 (§16) and as an OOBE smoke test (§14).

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
├── crawler/              # geography crawler (zone adjacency) — pure; consumes a WorldReader, imports sim
│   └── crawl.go
├── engine/               # The ONLY package that imports internal/rooms, /mutators, /events (direct engine-world calls; the root weather package also imports internal/* for plugin infrastructure).
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
│       ├── buffs/        # bespoke buff specs — only if module buff overlays land upstream (§5.6); v1 reuses existing engine buff ids
│       └── emotes/       # ambient emote tables
└── README.md
```

> **Repo realization (M1):** source lives at the repo root (root == the
> in-checkout `modules/weather/` dir); `go.mod` uses the path
> `github.com/GoMudEngine/GoMud/modules/weather` so pure-package import paths
> match standalone and in-checkout. The `go.mod` is a dev/test convenience and
> is not copied into a checkout (in-checkout modules have no `go.mod`).

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

### 5.5 Game time & scheduling (`internal/gametime`, `internal/util`)
- `gametime.GetDate() GameDate` (`gametime.go:144`) → `{Day, Month, Year, Night, ...}`; `gametime.MonthName(month)` (`months.go:20`) — the **season clock** (v2) derives from `Month`.
- **Scheduling primitives (per engine-author guidance — see §9.3):** `GameDate.AddPeriod(periodStr string) uint64` (`gametime.go:277`) returns the **round number** at which a period from now elapses; `GameDate.Add(hours, days, years) GameDate` (`:237`); `GetLastPeriod(name, round) uint64` (`:470`); `util.GetRoundCount() uint64` (`util.go:123`). These let us schedule the next weather tick as a target round rather than counting rounds by hand.

> **Divergence note:** An earlier `internal/mutators/context.md` summary described a *different* `MutatorSpec` (with `TextModifiers map`, `SpawnChance`, `Requirements`, no buffs). The **actual code** has the richer struct above. Trust source over summaries; the adapter pins us to the real API.

### 5.6 GoMud core impact & module boundary

**This is the most governance-sensitive part of the plan.** We own the module; Volte6 owns the GoMud engine. Anything that changes the engine requires his code review and lands on his schedule, and the *initial registry onboarding* is a one-time coordination step regardless. So the design's explicit goal is to **need zero engine changes for v1** — and the verification below says we hit that bar.

#### What lives entirely in the module (we own; no external review)
- The geography crawler, simulation core, and engine adapter (all of §6–§9).
- All data: climate profiles, weather types, weather **mutator specs**, default **buff specs**, emote tables — shipped via the module's `files/` overlay.
- Config, persistence, admin commands, exported API.

#### What touches GoMud — classification

| Capability we need | Mechanism | In GoMud today? | Verdict |
|---|---|---|---|
| Add/remove a mutator on a zone at runtime | `GetZoneConfig(z).Mutators.Add/Remove` | **Yes** — `admin.room.dispatcher.go:276` already calls `room.Mutators.Add` live; `admin.zone.go` manipulates zones. | No core change. |
| Zone mutators get lifecycle/decay each round | `NewRound_UpdateZoneMutators.go` → `GetZonesWithMutators()` → `Mutators.Update(round)` | **Yes** — engine already ticks zone mutators every round. | No core change. |
| Players see weather without re-`look` quirks | `Room.ActiveMutators` merges room+zone `GetActive()` **live** at render (`rooms.go:2562`) | **Yes** — read live each render. | No core change. |
| Enumerate zones/rooms/biomes/exits | `GetAllZoneNames`, `GetAllZoneRoomsIds`, `GetZoneBiome`, `Room.Exits` | **Yes.** | No core change. |
| Coarse clock + calendar date | `events.NewRound`, `events.DayNightCycle`, `gametime.GetDate` | **Yes.** | No core change. |
| Mutator carries buffs / light / chain / decay | `MutatorSpec.{PlayerBuffIds,LightMod,DecayIntoId,DecayRate,RespawnRate}` | **Yes** (verified in source). | No core change. |
| Ship **mutator** + custom data via module | data-overlay merge (`files/datafiles/`) | **Yes** (how all modules ship data). | No core change. |
| Ship **buff specs** via module overlay | data-overlay merge | **No** (confirmed by engine author, 2026-06-08) | v1 needs **no core change** — reuse existing buff ids (fallback a). Module-supplied buffs are an optional upstream contingency (R-core-1). |
| Persist module state | `plugin.WriteBytes/ReadBytes` | **Yes.** | No core change. |
| Expose API to other modules/JS | `plugin.ExportFunction` | **Yes.** | No core change. |
| Registry entry (`module install weather`) | registry listing | Process, not engine code | **One-time onboarding** with maintainers (§13.3). |

**Bottom line: v1 requires no changes to the GoMud engine.** The only maintainer-side step is the one-time registry onboarding.

#### Verification items (could, if they surprise us, become a small upstream PR)
- **R-core-1 — buff-spec overlay merge.** **Resolved (engine author, 2026-06-08): modules cannot currently ship their own `BuffSpec`s via the data overlay** — but Volte6 considers it "easy enough to add." Our plan:
  - **v1 ships on fallback (a), no core change:** map curated default weather effects onto *existing* engine buff ids. This is concretely available today — the default world's weather mutators already reference real buff ids (**31 Freezing**, **33 Thirsty**, **22 wildfire burn**) we can reuse directly (see §9.1, *Prior art*). So R-core-1 **can never block shipping**.
  - **Contingency (b), upstream, author-receptive:** add module-supplied buff overlays to the engine. If/when this lands, our richer/bespoke weather buffs move from "reuse existing ids" to "ship our own," with no change to the module's architecture (buffs are referenced by id from mutators either way). **Important caveat surfaced by the author:** numeric ids for items/buffs already risk collisions between the engine and modules. So a module-buff-overlay feature should ideally arrive with an **id-namespacing/allocation scheme** (e.g. reserved ranges or string-keyed ids for module content) rather than raw integers — this is a broader engine concern we'd want coordinated, not a weather-only patch. The module is designed so we benefit from such a scheme if it appears but depend on none of it.
- **R-core-2 — runtime room refresh.** Confirm that when weather changes for a room a player occupies, the next render reflects it acceptably (evidence: live merge at render says yes). If a *push* refresh is ever wanted, emit an existing event rather than change the engine.

#### Optional upstream contributions (explicitly NOT v1 requirements)
Ideas we might *offer* the maintainer later as additive, backward-compatible engine niceties — never blockers, all decoupled behind the `engine/` adapter so adopting one is a local swap:
- **Module-supplied buff overlays** (R-core-1) — the author is receptive. Lets us ship bespoke weather buffs instead of reusing existing ids. Best paired with an **id-namespacing/allocation scheme** for module content (the author's noted collision concern); we'd advocate that be part of the change.
- A first-class `WeatherChanged` event in `internal/events` (until then: a module-defined event + `ExportFunction`).
- Climate fields on `BiomeInfo` so biomes can *suggest* weather natively (we keep climate as module data regardless).
- A weather GMCP package in the `gmcp` module (a module-to-module contribution, not an engine change).

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

> **Status (2026-06-09):** the pure core (graph types, cache round-trip, and
> the `Build` algorithm with adjacency/weights/options/components/metadata) is
> implemented and unit-tested standalone. Remaining for M1b: the live
> engine-backed `WorldReader`, the `weather graph`/`weather rebuild` commands,
> and on-disk cache persistence.

> **Status (2026-06-09, M1b):** engine-backed WorldReader, versioned cache persistence, first-round build, and the `weather` admin command (summary/graph/rebuild) are implemented and smoke-tested on upstream GoMud's default world (15 zones; build → persist → reload verified). §6 is complete. The only DOGMud backport delta is the one-line sendLine helper.

> **Status (2026-06-09, M3):** §8–§10 are implemented at the code level — zone-wide
> mutator application driven by the sim's per-zone weather (with `engine.Reconcile` as
> the single application path), module-shipped weather mutator specs loaded via the
> engine's plugin-FS support (`mutators.LoadDataFiles() loadedCount=18` on the stock
> world — 10 disk + 8 plugin — with no `duplicate mutator id`/`filepath mismatch`
> errors; upstream now supports module mutator AND buff overlays — R-core-1's
> contingency landed; v1 still reuses engine buff ids 31/33), gametime-scheduled ticks,
> state persistence with reconcile-on-boot, indoor-aware ambient emotes, the player/admin
> command handler, and the exported GetWeather/GetFronts/SpawnFront API. Found & fixed
> during execution: upstream `MutatorList.Remove` instantly resurrects mutators whose
> spec has `decayintoid` (no liveness guard in the decay branch), so weather specs carry
> `decayrate` only. Deferred to M4: per-room indoor/biome mutator variants,
> `Buffs.Overrides`, bespoke module-shipped buffs, full per-biome default content, OOBE
> smoke test in CI.

> **Boot-smoke finding, RESOLVED (2026-06-09, M3):** the first smoke run found the
> `weather` command did not register at runtime. Root cause: upstream
> `internal/plugins/plugins.go::Load()` harvests each plugin's `userCommands` map into
> the global command registry *before* invoking that plugin's `onLoad()`, so a command
> registered in `onLoad` is added too late and never exists. Fix: the module registers
> its command and exported functions in `init()` (as the engine's own modules do);
> `cfg.Enabled` now gates *behavior* in the handler, not registration. §5.1's
> `AddUserCommand` guidance should be read with this constraint. After the fix the full
> interactive smoke checklist passed on the stock world: player `weather` view, `status`,
> `zones` (15 zones), `spawn storm` → `(storm-wracked)` room title + description +
> alert, `fronts`, `clear` → mutator removed cleanly (no `decayintoid` resurrection),
> and a spawn → clean `/shutdown` → reboot cycle showing `restored simulation state
> fronts=1` with the room mutator re-asserted by reconcile-on-boot, no re-spawn needed.

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

The primary package for direct engine-world calls (`internal/rooms`, `internal/mutators`, `internal/events`, `internal/gametime`); the root `weather` package also imports `internal/*` for plugin infrastructure (plugins, events, users, mudlog).

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
  - Mutator naming convention follows the engine's existing style — lowercase, hyphenated ids (the default world uses `forest-mist`, `desert-sun`, `freezing-cold`). Ours are namespaced: `weather-<type>` (zone default, outdoor) and `weather-<type>-indoor` (indoor variant).
  - The applier consults `room.GetBiome()` / an indoor flag and applies the variant only to rooms that diverge from the zone default. To bound cost, **per-room refinement is applied lazily** — only to rooms in zones that currently contain players (`GetRoomsWithPlayers()`); unoccupied rooms inherit the zone-wide mutator and are refined on entry. (Configurable: `PerRoomRefinement: occupied|all|off`.)

**Prior art — the engine already ships weather-style mutators.** The default world includes `forest-mist`, `freezing-cold`, `desert-sun`, and `wildfire` mutators (in `_datafiles/world/default/mutators/`, surfaced by the engine author). They establish the exact conventions we follow and prove the pattern end-to-end:
- `namemodifier` *append* a short parenthetical tag (`(misty)`, `(scorching)`, `(freezing)`) with a `colorpattern`.
- `descriptionmodifier` *append* a sentence of flavor.
- `alertmodifier` is **append-only** (no prepend/replace) — used for the loud "!!! a wildfire is burning here !!!" banner.
- Sun-relative `respawnrate`/`decayrate` (`midnight`→`sunrise`, `noon`→`sunset`) for the *static, diurnal* effects those ship with.
- Real, reusable buff ids already exist: **31 Freezing**, **33 Thirsty**, **22 (wildfire burn)**.

The crucial difference: those defaults are *static and diurnal* (they respawn/decay on the clock, in place). **Ours are orchestrator-driven and traveling** — the weather engine adds/removes them as fronts move, so our specs set **no `respawnrate`** (we don't want auto-respawn fighting the orchestrator) and use `decayrate` purely as a self-heal safety net (§9.2). We can directly reuse the existing buff ids for our curated defaults, which is exactly the no-core-change buff path in §5.6 (R-core-1, fallback a).

Example weather mutator spec (`files/datafiles/mutators/weather-storm.yaml`) — real `MutatorSpec` schema, matching engine conventions:

```yaml
mutatorid: weather-storm
namemodifier:
  behavior: append
  text: (storm-wracked)
  colorpattern: storm
descriptionmodifier:
  behavior: append
  text: Rain lashes down and thunder rolls across the sky.
  colorpattern: storm
alertmodifier:                 # append-only by engine design
  text: A storm rages overhead.
  colorpattern: storm
lightmod: -1                   # storms darken the area
playerbuffids: [ 33 ]          # reuse an existing engine buff for v1 (e.g. exposure); override-able
mobbuffids:    [ 33 ]
decayrate: 6 hours             # SAFETY NET only — orchestrator normally removes it first; no respawnrate
decayintoid: weather-overcast  # graceful fade rather than a hard cut
```

### 9.2 Why mutator self-decay is a safety net, not the engine
The orchestrator is authoritative: it adds/removes weather mutators as fronts move. But we *also* set `DecayRate`/`DecayIntoId` so that if the module is disabled mid-run, crashes, or misses a cleanup, rooms **heal themselves** to a calm state instead of being stuck in an eternal storm. Defense in depth.

### 9.3 The weather clock (cadence)
- Weather ticks are **coarse** — they must not fire every combat round. Default: **once per in-game hour** (configurable `TickEveryGameHours`, default `1`).
- **Scheduling (per engine-author guidance).** Rather than hand-counting rounds, we schedule the next tick as a **target round number** using the `gametime` API, then on each `NewRound` simply check whether we've reached it:
  ```go
  // schedule the next weather tick
  next := gametime.GetDate().AddPeriod("1 hour")   // -> round number, uint64
  // each NewRound:
  if util.GetRoundCount() >= next { tick(); next = gametime.GetDate().AddPeriod("1 hour") }
  ```
  To align ticks to the **top of the hour** (optional, trickier): seed from `GetLastPeriod("hour", util.GetRoundCount())` and `.Add(0, 1, 0)`. The cadence string is configurable so `TickEveryGameHours` maps straight onto `AddPeriod`.
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
- **v1 references *existing* engine buff ids** from weather mutators (e.g. 31 Freezing, 33 Thirsty, 22 burn) — modules cannot yet ship their own buff specs via overlay (§5.6, R-core-1), and reusing existing ids needs no core change and risks no id collisions. The default set is mapped to the closest existing buffs.
- Conceptually the default set is intentionally small and gentle — e.g. cold weather → a chill/freeze effect, storms → an exposure/soaked penalty, heat → thirst. The exact mapping to engine buff ids is finalized in M3 once we audit the available buffs.
- **If/when module buff overlays land upstream** (§5.6 contingency), bespoke weather buffs (`files/datafiles/buffs/`) drop in by id with no architectural change.
- Every default is **toggle-able** (`Buffs.Enabled`, plus `Buffs.Overrides` mapping each weather type to a world's own buff ids). We ship *defaults*, not *opinions*.

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
| `Seed` | `0` (→ derived from the world's zone names) | RNG seed for reproducibility. |
| `TickEveryGameHours` | `1` | Weather-simulation cadence. |
| `MaxActiveFronts` | `8` | Global front budget (minimum 1). |
| `SpawnRateScale` | `1.0` | Multiplier on spawn pressure (0 stops new fronts). |
| `PrevailingWind` | `""` | *(deferred — needs directional edge metadata; not yet a config key)* Optional movement bias. |
| `PerRoomRefinement` | `occupied` | *(M4 — not yet a config key)* indoor/biome variant granularity. |
| `IncludeSecretExits` | `true` | Crawler counts secret/locked exits as adjacency. |
| `ExcludeZonePatterns` | `["instance_*","ephemeral_*"]` | *(crawler-internal default; not yet a config key)* Zones the crawler skips. |
| `EmoteMode` | `module` | `module` (we emit) \| `tag-only` (builders react to tags). |
| `EmoteEveryRounds` | `20` | Ambient emote cadence (jittered ±25%, minimum 5). |
| `BuffsEnabled` | `true` | Apply curated default buffs. *(Flat key — M3 finding: plugin config reads flattened scalar leaves, so nested `Buffs.Enabled` became `BuffsEnabled`.)* |
| `Buffs.Overrides` | `{}` | *(M4 — not yet a config key)* Map `weatherType -> []buffId` to replace defaults. |
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

## 13. Portability, Distribution & Project Governance

### 13.1 Current state
DOGMud is a **fork** of GoMud (same module path `github.com/GoMudEngine/GoMud`, `upstream` remote → GoMud) and already ports the playtest harness. The APIs this module touches — `mutators`, `rooms`/`ZoneConfig`, `events` (`NewRound`/`DayNightCycle`), `gametime`, `plugins` — **match upstream today**. The portability risk is *future drift*, since DOGMud diverges elsewhere (combat, permadeath sunset, level-up disabled).

### 13.2 Strategy
- **Ports & adapters (Section 4.2).** `sim/` and `crawler/`'s logic are engine-agnostic; **all** engine calls live in `engine/`.
- **Backport procedure:** copy `modules/weather/` into the other engine's `modules/` (same import path → compiles as-is), run `go generate ./... && go build`. If a touched API drifted, fix only `engine/`; `sim/` is untouched.
- **Guardrail:** an architecture test asserts the `sim/` package imports no `internal/*`. The `crawler/` is also pure — it reads the world through the `WorldReader` interface; the engine-backed `WorldReader` implementation lives in `engine/`. If someone reaches into the engine from `sim/` or `crawler/`, CI fails.
- **Version note in README** (mirroring the playtest module): the module needs an engine with the `mutators` buff fields + `DayNightCycle` event; on older engines it fails soft.

### 13.3 Module registry onboarding (one-time, maintainer-coordinated)
Distribution targets GoMud's module registry so operators can `go run . module install weather`. The registry has no automated dependency field (per the playtest module's README), so:
- The module **README states engine prerequisites explicitly** (mutator buff fields + `DayNightCycle`; fails soft on older engines).
- Onboarding the `weather` entry is a **one-time coordination step with the GoMud maintainers** (Volte6). It is process, not an engine code change.
- Until/if listed, the module installs by dropping `module/weather/` into a GoMud checkout's `modules/weather/` and rebuilding — the same dev path the playtest harness uses.

### 13.4 Review & ownership workflow

| Change type | Owner | Review path |
|---|---|---|
| Module code & data (`modules/weather/**`) | Us | Our repo's normal review. |
| Any GoMud engine change (`internal/**`) | Volte6 / GoMud | Upstream PR + maintainer review; kept to **zero for v1** by design (§5.6). |
| Registry listing | GoMud maintainers | One-time onboarding (§13.3). |
| DOGMud-side adapter fixes (`engine/` on drift) | Us | DOGMud repo review. |

**Guiding principle: keep the engine PR queue empty.** If a feature seems to need a core change, first try to express it through existing mutator/event/overlay mechanisms behind the `engine/` adapter; only escalate to an upstream proposal when there is no module-side path, and present it as optional and backward-compatible.

### 13.5 Licensing
GoMud is **GPLv3**; this module compiles into a GPLv3 binary and ships under **GPLv3** to match the engine and the broader module ecosystem.

---

## 14. Testing Strategy

| Layer | Approach |
|---|---|
| **Crawler** | Synthetic room/exit fixtures via `WorldView` fakes; assert graph shape, weights, components, cache round-trip. No live server. |
| **Sim core** | Pure unit tests. Golden-trace reproducibility (seed → N ticks). Feedback-loop scenario tests (storm-over-mountain decays & dies). Budget/clamp invariants. Architecture test: no `internal/*` imports. |
| **Engine adapter** | Table tests mapping `StateDiff` → expected mutator `Add/Remove` calls (mutator layer mocked). |
| **Presentation** | Emote-table selection tests (right table for `(weather,biome,indoor)`); `tag-only` mode emits nothing. |
| **Integration (manual / harness)** | Boot a small world, `weather spawn storm <zone>`, walk a front across a mountain chain, observe decay; verify self-heal on module disable. The **playtest harness** is the natural driver here — fitting, given the shared lineage. |
| **OOBE smoke test** | On a **stock** GoMud world with only `Enabled: true`: boot → crawler builds a graph → fronts spawn → standard biomes get weather + emotes → no errors/warnings beyond expected. Then `Enabled: false` → rooms self-heal to calm. Directly encodes the §2.3 bar; runs in CI against a fixture world where feasible. |

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
3. **M3 — Application & presentation.** Engine adapter, mutator application, emote tables, curated buffs, persistence, full command set, config. Resolves verification items R-core-1/2 (§5.6). *First end-to-end weather; OOBE acceptance (§2.3) begins here.*
4. **M4 — Polish, default content & docs.** Default climate/emote/buff content for **all standard biomes** (the OOBE requirement), README + builder extension guide, OOBE smoke test green (§14), harness-driven integration pass. *Module is "clone → flip flag → works."*
5. **Registry onboarding** — coordinate the `weather` listing with GoMud maintainers (§13.3). Can begin once M3 is stable; gated only on process, not engine code.
6. **v2 — Seasons.** Per Section 11.

Each milestone is its own spec → plan → implementation cycle; M1–M3 correspond to the three sub-projects. **No milestone depends on a GoMud engine change** (§5.6).

> **Status (2026-06-11, M4):** implemented on branch `worktree-m4-polish` and
> smoke-verified end-to-end on the stock world (full evidence in the
> [M4 polish spec](2026-06-10-m4-polish-design.md) status note): per-room
> refinement (`PerRoomRefinement: occupied|all|off` with indoor `-indoor`
> variants and refine-on-entry), bespoke module buffs 59001–59003 plus
> `BuffOverrides.<type>` remapping, `ExcludeZonePatterns`, per-biome emote
> variants, README builder guide, the four AP1 polish items (validation 400s,
> typed inputs, badges, read-back-verified writes), and the CI workflow
> (authored; first run happens on push to the org repo). The OOBE smoke
> surfaced two engine config-layer defects — `Config.OverlayOverrides`
> replaces (not merges) the inner `Modules.<module>` map, and
> `PluginConfig.Set` swallows `SetVal` errors — the module ships a boot
> self-heal plus verified admin writes as mitigations; engine fixes are queued
> for a separate upstream PR.

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
