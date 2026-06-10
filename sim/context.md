# sim Package Context

## Overview
`sim` is the pure, engine-independent core of the weather module. It holds the
geography `Graph` that the crawler produces and the weather simulation will
consume. Nothing here imports the GoMud engine (`internal/*`) — a rule enforced
by `arch_test.go` — so the simulation stays unit-testable without a running
server and portable across GoMud and DOGMud.

## Key Components
### Core Files
- **graph.go**: the `Graph`, `ZoneNode`, and `Edge` types; the `GraphVersion`
  constant; JSON cache serialization (`ToJSON` / `FromJSON`); and the
  `Neighbors` adjacency query.
- **arch_test.go**: architecture guardrail — fails if any file in this package
  imports a `GoMudEngine/GoMud/internal` path.

### Key Structures
```go
type ZoneNode struct {                 // one zone = one graph node
    Zone, Biome string
    Rooms       int
    HasOutdoor  bool
}
type Edge struct {                     // undirected, canonical (A <= B)
    A, B   string
    Weight int                         // # of room-exits crossing the border
}
type Graph struct {
    Version      int
    BuiltAtRound uint64
    Nodes        map[string]ZoneNode
    Edges        []Edge
    Components   int                   // connected-component count
}
```

## Core Functions
- `(*Graph) ToJSON() ([]byte, error)` / `FromJSON([]byte) (*Graph, error)` —
  the on-disk cache format (indented JSON). `GraphVersion` lets a loader detect
  a stale cache and rebuild.
- `(*Graph) Neighbors(zone string) []Edge` — adjacent zones, each Edge oriented
  from the queried zone (`Edge.A == zone`).

## Dependencies
- Standard library only (`encoding/json`). No engine imports (enforced).

## Consumers
- `crawler.Build` returns a `*Graph`.
- (Future) the weather simulation reads adjacency via `Neighbors`; the engine
  integration loads/saves the cache via `ToJSON`/`FromJSON`.

## Testing
- `graph_test.go`: JSON round-trip and `Neighbors`.
- `arch_test.go`: purity guardrail.

## Weather simulation (M2)

Beyond the geography `Graph`, `sim` now contains the pure, deterministic weather
simulation. It consumes a `*Graph` as its read-only world and produces weather
state as plain data — no engine imports.

### Key files
- **rng.go**: `RNG`, a serializable splitmix64 PRNG (cursor = one `uint64`).
- **weather.go**: `WeatherType` (+ `Clear`), `Front`, `State`, `StateDiff`,
  `ZoneChange`, `Clock`, and `clamp01`.
- **climate.go**: `ClimateProfile` / `WeatherInfluence` / `Climate` (biome →
  weather weights + influence + spawn weight), `Climate.For` (default fallback),
  `DefaultClimate`; plus `Config` / `DefaultConfig` (front budget, spawn chance,
  history length, hard age cap).
- **tick.go**: `Step(prev, graph, climate, cfg, clock) -> (next, diff)` and its
  helpers (`ageAndFeedback`, `moveFronts`, `evolveTypes`, `removeDead`,
  `spawnFronts`, `resolveWeather`, `diffWeather`).
- **state.go**: `State.ToJSON` / `StateFromJSON` (persistence codec).

### The tick
Each `Step`: age fronts and apply the current zone's biome influence (the
weather ← biome feedback), move fronts along edges (damped by `MovementResistance`,
no immediate backtrack), evolve a moved front's type from the new zone's climate,
drop dead fronts (intensity ≤ 0 or past the hard age cap), maybe spawn one front
under budget (origin weighted by `SpawnWeight`), resolve per-zone weather with
**intensity-scaled area coverage** (a front projects onto zones within
`MaxFrontRadius` hops at `Intensity × CoverageFalloff^hops`, covered while
`>= MinProjected`; per zone the highest *effective* intensity wins; frontless
zones = `Clear`), and emit the diff.

### Determinism
All randomness flows through the `RNG` built from `State.RNGState`; the advanced
cursor is written back into the next `State`. Same seed + graph + tick count ⇒
identical fronts and per-zone weather (see `TestStep_Deterministic`).

### Deferred (later milestones)
- File/YAML climate overrides and mutator application live in the engine layer (M3).
- **Prevailing-wind direction** (a MUD owner biasing where storms originate/move,
  e.g. west→east) is a planned later chunk: it needs directional edge metadata,
  which a future crawler pass can derive from each exit's `MapDirection`.
- Calm-zone variety (occasional light fog/overcast in frontless zones) and an
  explicit windward "orographic" precip spike are noted enrichments, not built.
