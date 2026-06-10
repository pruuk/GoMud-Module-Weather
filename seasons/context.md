# seasons Package Context

## Overview
`seasons` resolves data-defined season tracks against the game calendar and
applies them as a **pure transform** over `sim.Climate`. It never modifies
`sim.Step` — the simulation receives the adjusted climate as an input, but its
core logic is untouched. This is the architecture's regression guarantee for
v2: all existing `sim` tests remain valid, and setting `SeasonsEnabled: false`
reverts to v1 behavior exactly. No engine imports (enforced by `arch_test.go`);
season state is never persisted because it is always derivable from the
calendar position.

## Key Components
### Core Files
- **track.go**: `Season` (one phase of a cycle), `Track` (seasons list plus the
  validated calendar shape), `Tracks` (name → `Track`); `trackFile` (the
  on-disk YAML schema mirror); `Load(fsys, dir, monthsPerYear, daysPerYear)
  (Tracks, []error)` — walks a directory of `*.yaml` files, validates each
  against the given calendar shape, and returns valid tracks plus per-file
  errors (fail-soft: invalid tracks are dropped, the valid remainder is
  returned). `parseTrack` applies all validation rules. `contiguousStart`
  detects whether a set of month numbers forms one run modulo the year (wrap
  example: [12, 1, 2]).
- **resolve.go**: `monthOfDay` (mirrors the engine formula exactly — see Core
  Functions); `firstDayOfMonth` (the smallest day-of-year whose month equals
  the given value); `(Track).Resolve(dayOfYear) (current, previous string,
  blend float64)` — the central query that delivers the blend contract;
  `(Track).season(name) Season` (internal load-time lookup with zero-safe
  default fallback).
- **apply.go**: `CalendarPos{DayOfYear, DaysPerYear}` (the engine-facing
  calendar position type); `ZoneSeason{Track, Season, Blend}` (per-zone
  resolved result); `EffectiveClimate(base, tracks, pos) sim.Climate` — the
  pure transform that season-adjusts every biome profile; `ZoneSeasons(g,
  base, tracks, pos) map[sim.ZoneId]ZoneSeason` — per-zone convenience that
  maps each zone's biome to its current season.
- **doc.go**: package-level comment summarising the pure-transform contract.
- **arch_test.go**: purity guardrail — fails if any file imports a
  `GoMudEngine/GoMud/internal` path.

### Key Types
```go
type Season struct {
    Name                     string
    Months                   []int     // 1-based; one contiguous run (may wrap)
    TransitionDays           int       // blend window at season START; 0 = hard flip
    WeatherWeightMultipliers map[sim.WeatherType]float64
    WeatherWeightAdditions   map[sim.WeatherType]float64 // added after scaling; may introduce absent types
    BaseWeightScale          float64   // scales ALL base weights; default 1.0; 0 = suppress
    SpawnWeightMultiplier    float64   // default 1.0
    Influence                sim.WeatherInfluence // deltas ADDED to the biome's own values
    // startMonth — derived at load; unexported
}
type Track struct {
    Name    string
    Seasons []Season
    // monthsPerYear, daysPerYear — validated calendar shape; unexported
}
type Tracks map[string]Track

type CalendarPos struct {
    DayOfYear   int
    DaysPerYear int
}
type ZoneSeason struct {
    Track  string
    Season string
    Blend  float64
}
```

## Core Functions

### Track file schema and validation rules
Each `*.yaml` under the seasons directory mirrors this structure:

```yaml
track: <name>                          # required
seasons:
  - name: <string>                     # required
    months: [...]                      # 1-based month numbers
    transitionDays: 0                  # optional; 0 = hard flip
    weatherWeightMultipliers: { <type>: <float> }
    weatherWeightAdditions:   { <type>: <float> }
    baseWeightScale: 1.0               # optional; default 1.0; must be >= 0
    spawnWeightMultiplier: 1.0         # optional; default 1.0
    influence: { intensityDelta: 0, moistureDelta: 0, movementResistance: 0 }
```

Validation rules enforced by `parseTrack` before a track is accepted:
1. `track:` key must be present and non-empty.
2. Every month 1..N (N = `monthsPerYear`) must be claimed by exactly one season
   — no gaps, no overlaps.
3. All month values must be in the range 1..N.
4. Each season's `months` list must form one **contiguous run modulo the year**
   (e.g. [12, 1, 2] is valid; [1, 3, 5] is not).
5. `baseWeightScale` must be >= 0; all multiplier and addition values must be >= 0.
6. A duplicate track name across files is rejected (first-wins, error on the
   second). A missing directory returns empty tracks with no errors.

### monthOfDay — engine formula mirror
`monthOfDay(dayOfYear, daysPerYear, monthsPerYear) int` computes the calendar
month exactly as GoMud's `internal/gametime` does:

```
hoursPerMonth = daysPerYear × 24 / monthsPerYear
month = 1 + floor(dayOfYear × 24 / hoursPerMonth), clamped to monthsPerYear
```

Mirroring this formula precisely is required so that a season's month claims
always agree with what the engine's `time` command reports. **Exact-divisor
subtlety:** when `daysPerYear` is an exact multiple of `monthsPerYear` (e.g.
100 days / 4 months = 25 days per month), the boundary day is the first day of
the new month — day 25 resolves to month 2: `1 + floor(25×24/600) = 2`. The
boundary is sharp.

### Resolve — the blend contract
`(Track).Resolve(dayOfYear) (current, previous string, blend float64)`:

- `blend = clamp(daysInto / transitionDays, 0, 1)` where `daysInto` is the
  day-of-year distance from the first day of the current season (with
  year-wrap correction for seasons that span the calendar boundary).
- At the **boundary day** (daysInto = 0): `blend = 0`. The reported season
  has already flipped to the new one, but the effective climate is still fully
  the previous season's. This makes the blend **continuous across the flip**:
  the last day of the outgoing season and the first day of the incoming season
  both land at blend = 0 with identical effective parameters.
- `transitionDays <= 0`: `blend = 1` immediately on the boundary day (hard
  flip, no transition window).
- Single-season tracks always return `(name, name, 1.0)`.

### EffectiveClimate — the transform
For each biome with a bound, known track:

```
eff(w) = base(w) × lerp(prevScale×prevMult(w), curScale×curMult(w), blend)
         + lerp(prevAdd(w), curAdd(w), blend)
```

Key semantics:
- **Missing multiplier = 1.0** (the `mult` helper). Missing addition = 0.
- **`BaseWeightScale`** multiplies ALL of a biome's base weights before
  additions. `0.0` suppresses all normal weather for that season, leaving only
  what additions introduce. Default 1.0 (neutral).
- **`WeatherWeightAdditions`** are absolute weights added after the scale/
  multiply pass — they may introduce weather types entirely absent from the
  base climate (esoteric seasons, spec §3.1a). During the blend window they
  lerp from/to 0 when the neighboring season lacks them.
- **Influence deltas** are blended then **added** to the biome's own values.
  `MovementResistance` is subsequently clamped to [0, 1] (the sim treats it
  as a probability).
- **`SpawnWeight`** is multiplied by the blended seasonal `SpawnWeightMultiplier`.
- **Unbound biomes** (`p.Track == ""` or unknown track name) are passed through
  unchanged; their map pointers are shared with the input, not copied (the
  function never writes to an unbound profile).
- **Deep-copy guarantee**: each bound profile's `Weather` map is freshly
  allocated; the input `base` climate is never mutated.

### ZoneSeasons
`ZoneSeasons(g, base, tracks, pos) map[sim.ZoneId]ZoneSeason` maps every zone
to its current season via its biome's track. Zones whose biome resolves to an
unbound profile — no track or an unrecognised track name — are **absent** from
the returned map. Biome resolution uses `base.For(biome)`, which includes the
`"default"` profile fallback, matching how `sim` resolves climate profiles.

## Dependencies
- `github.com/GoMudEngine/GoMud/modules/weather/sim` (types only).
- `gopkg.in/yaml.v2` — the engine's own dependency; same pin as `content`; the
  standalone `go.mod` carries it for tests; never travels to checkouts.
- Standard library (`fmt`, `io/fs`, `path`, `strings`). No engine imports.

## Consumers
- Module root (`weather_tick.go`): `Load` at startup; `EffectiveClimate` at
  every tick (only when `seasonsOn`); `ZoneSeasons` at startup for the
  baseline resolution and in `resolveSeasons` after each tick for event diffing.
- Module root (`weather_commands.go`): `(Track).Resolve` in `printSeasons`.
- `engine/calendar.go`: provides the `CalendarPos` that the root tick passes
  here (the engine glue lives there, not in this package).

## Testing
- `track_test.go`: `Load` — full temperate track parse (multipliers, influence,
  defaults), esoteric fields (`baseWeightScale`, `weatherWeightAdditions`),
  validation error cases (gap, overlap, out-of-range, non-contiguous, missing
  name, missing track key, negative values), missing dir, duplicate track names.
- `resolve_test.go`: `monthOfDay` boundary cases including exact-divisor
  calendars; `Resolve` — mid-season, blend window start (blend = 0 at boundary),
  mid-window (blend 0.5), post-window (blend 1.0), year-wrap-around, spring
  start, hard flip (`transitionDays 0`), single-season track; non-12-month
  calendar.
- `apply_test.go`: `EffectiveClimate` — full-season multipliers (winter snow,
  summer storm), blend window (halved snow at 0.5), unbound pass-through and
  base-immutability (deep copy), unknown-track pass-through, esoteric
  `shattering` season (`baseWeightScale=0` suppresses normal weather; additions
  lerp in and out), additions present in both seasons (dedup branch),
  `ZoneSeasons` binding and absence.
- `moduledata_test.go`: validates the shipped `temperate` and `monsoon` tracks
  against the stock 12-month/365-day calendar; checks that all multiplier and
  addition keys reference known shipped weather types (typo guard).
- `arch_test.go`: engine-import purity guardrail.

All tests run standalone: `go test ./seasons/...` (no checkout required).

## No persistence by design
Season state is never written to plugin storage. The current season is fully
determined by the calendar position: given the same `DayOfYear` and track files
the result is always identical. Persisting it would add no value and create a
source of stale state when calendar settings or track files change.
