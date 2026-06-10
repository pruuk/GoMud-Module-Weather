# content Package Context

## Overview
`content` is the pure data-loading layer for the weather module. It parses two
kinds of module data files from an `fs.FS` (typically the module's embedded
`files/` tree): climate profiles (YAML → `sim.Climate` merged over
`sim.DefaultClimate`) and ambient emote tables (YAML → `Tables` + `Pick`). No
engine imports — purity enforced by `arch_test.go`. The only non-stdlib
dependency is `gopkg.in/yaml.v2`, which the GoMud engine itself uses.

## Key Components
### Core Files
- **climate.go**: `ParseClimate` (one file → biome id + `sim.ClimateProfile`);
  `LoadClimate` (walks a directory, returns `DefaultClimate` overlaid with every
  `*.yaml` found; a missing directory is not an error — pure defaults are
  returned; the first malformed file aborts with an error).
- **emotes.go**: `TableSection` (one outdoor/indoor pair of biome-keyed line
  lists — shared by the seasonal variant blocks and the standalone ambience
  tables); `Table` (per-weather-type ambient lines keyed by biome, split
  outdoor/indoor, plus an optional `Seasonal map[string]TableSection`);
  `Tables` (weather type → `Table`); `ParseEmoteTable`; `LoadEmotes`
  (walks a directory, empty tables for a missing directory); `(Tables).Pick`
  (now season-aware — see Core Functions); `sectionLines` (shared
  biome → "default" resolver). Plus the standalone seasonal-ambience layer:
  `SeasonalKey{Track, Season}`, `SeasonalTables map[SeasonalKey]TableSection`,
  `LoadSeasonalEmotes`, and `(SeasonalTables).Pick`.
  - **Season-aware weather `Pick` order** (spec §6): when `season != ""`, try the
    table's `Seasonal[season]` section first (biome → "default"); fall through to
    the base section on any miss (no variant for that season, or the variant
    lacks lines for that biome/indoor combination). `season == ""` skips the
    variant layer entirely (seasons off / unbound zone). The season key matches
    by NAME across tracks by design ("winter" is temperate's winter).
  - Indoor NEVER falls back to outdoor — silence beats wrong prose;
    out-of-range roll result clamped to index 0. Both `Pick`s share this contract.
  - **Seasonal ambience** lives in `emotes/seasons/` named `<track>_<season>.yaml`
    — a subdirectory so the non-recursive `LoadEmotes` never sees it; the
    filename rule is OURS (the content loader), not the engine's. Files require
    `track:` and `season:` keys; `LoadSeasonalEmotes` keys them by
    `SeasonalKey`. `(SeasonalTables).Pick(track, season, biome, indoor, roll)`
    looks up the exact (track, season) — no cross-season fallback — then applies
    the same biome/indoor/roll contract.
- **arch_test.go**: purity guardrail — fails if any file imports a
  `GoMudEngine/GoMud/internal` path.
- **moduledata_test.go**: validates the SHIPPED YAML files under
  `files/datafiles/`. For emote tables: parseable, `filename == weather+".yaml"`,
  at least one outdoor-default and one indoor-default line. For mutator specs:
  parseable, `mutatorid` is **`weather-` or `season-`** namespaced (both
  namespaces validated under the same rules), filename matches
  `fileNameFor(mutatorid)` (mirrors `util.ConvertForFilename`), `respawnrate`
  forbidden (would fight the orchestrator), `decayintoid` forbidden (upstream
  `MutatorList.Remove` resets `SpawnedRound` and runs `Update` whose decay
  branch has no liveness guard — the decay target is instantly resurrected),
  `decayrate` required (self-heal safety net). A count assertion requires at
  least **14** shipped mutator specs (8 weather + 6 season) so a sync/copy
  mistake is loud. For emote tables it also validates `seasonal:` variant keys
  (each must be a season of a shipped track and carry at least one line), and a
  separate check (`TestShippedSeasonalAmbience`) loads `emotes/seasons/`,
  asserts the six (track, season) ambience tables are present with outdoor +
  indoor default lines, and that each file obeys the `<track>_<season>.yaml`
  filename rule.
- **doc.go**: package-level comment.

### Key Types
```go
type TableSection struct {
    Outdoor map[string][]string // biome -> lines (outdoor section)
    Indoor  map[string][]string // biome -> lines (indoor section)
}
type Table struct {
    Weather  string                  // weather type this table covers
    Outdoor  map[string][]string     // biome -> lines (outdoor section)
    Indoor   map[string][]string     // biome -> lines (indoor section)
    Seasonal map[string]TableSection // optional per-season variant overrides
}
type Tables map[sim.WeatherType]Table

type SeasonalKey struct{ Track, Season string }
type SeasonalTables map[SeasonalKey]TableSection // standalone seasonal ambience
```

## Core Functions
- `ParseClimate([]byte) (string, sim.ClimateProfile, error)` — parse one climate
  YAML into its biome id and profile. The optional `track:` key is passed
  through as `ClimateProfile.Track`; omitting it leaves `Track` empty
  (= unbound, no seasonal adjustment for this biome).
- `LoadClimate(fs.FS, dir string) (sim.Climate, error)` — `sim.DefaultClimate()`
  overlaid with every `*.yaml` under `dir`.
- `ParseEmoteTable([]byte) (Table, error)` — parse one emote table YAML.
- `LoadEmotes(fs.FS, dir string) (Tables, error)` — all emote tables under `dir`.
- `(Tables).Pick(weather sim.WeatherType, biome string, indoor bool, season string, roll func(int) int) string`
  — select one weather ambient line, season-variant-aware (variant section →
  base section; `season == ""` skips variants). `roll(n)` must return `[0,n)`;
  pass the engine's `util.Rand` (or a stub in tests) — NEVER the sim RNG, which
  must stay isolated from presentation randomness.
- `LoadSeasonalEmotes(fs.FS, dir string) (SeasonalTables, error)` — all
  `<track>_<season>.yaml` ambience files under `dir` (typically
  `emotes/seasons`); missing dir → empty (silence); a malformed file or a missing
  `track`/`season` key aborts with an error (caller fails soft).
- `(SeasonalTables).Pick(track, season, biome string, indoor bool, roll func(int) int) string`
  — select one standalone seasonal-ambience line for the exact (track, season);
  same biome/indoor/roll contract.

## Dependencies
- `github.com/GoMudEngine/GoMud/modules/weather/sim` (types only).
- `gopkg.in/yaml.v2` — the engine's own dependency; the standalone `go.mod`
  carries it for tests; `go.mod`/`go.sum` never travel to checkouts.
- Standard library (`io/fs`, `path`, `strings`, `fmt`). No engine imports.

## Consumers
- Module root (`weather_tick.go`): calls `LoadClimate`, `LoadEmotes`, and
  `LoadSeasonalEmotes` at startup; results are stored on `weatherModule`
  (`tables`, `seasonalTables`) and re-used each tick.
- `engine.EmitAmbient`: receives a `content.Tables` and a
  `content.SeasonalTables` and calls both `Pick`s (weather first; seasonal only
  in calm zones) with the engine's `util.Rand` as the roll function.

## Testing
- `climate_test.go`: `ParseClimate`, reject-missing-biome, `LoadClimate` merges
  override over defaults, missing dir returns pure defaults.
- `emotes_test.go`: `ParseEmoteTable`, `LoadEmotes` missing dir, `Pick` biome
  selection, indoor-never-falls-back-to-outdoor, roll forwarding, out-of-range
  clamp, season-variant lookup order (`TestPickSeasonalVariant`), and
  `LoadSeasonalEmotes` load + `Pick` + validation (missing track/season,
  missing dir).
- `moduledata_test.go`: validates shipped YAML (see Key Components above).
- `arch_test.go`: engine-import purity guardrail.

All tests run standalone: `go test ./content/...` (no checkout required).
