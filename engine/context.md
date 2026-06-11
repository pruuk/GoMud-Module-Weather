# engine Package Context

## Overview
`engine` concentrates all direct engine-world calls (room/zone/biome reads,
mutator application, game clock, ambient emotes, cache codec) and implements
`crawler.WorldReader`. Together with the root `weather` package — which imports
`internal/*` for plugin infrastructure — these are the only packages that touch
the engine; `sim`, `crawler`, and `content` stay pure. That split keeps the
module portable across GoMud and DOGMud.

## Key Components
### Core Files
- **worldreader.go**: `WorldReader` implements `crawler.WorldReader` over
  `internal/rooms` (`GetAllZoneNames`, `GetZoneBiome`, `GetAllZoneRoomsIds`,
  `LoadRoom`). `NewWorldReader()` returns it as the interface. `isOutdoorBiome`
  derives a room's outdoor flag from its biome id (GoMud has no explicit
  indoor/outdoor flag), using the `indoorBiomes` heuristic set. Also used by
  `EmitAmbient` for the indoor-detection heuristic.
- **cache.go**: `CacheIdentifier` (the plugin-storage key) and `DecodeCache`,
  a pure, version-checked decoder that returns ok=false for absent/empty/
  unparseable/stale data so the caller knows to rebuild.
- **state.go**: `StateIdentifier` (plugin-storage key "simstate"),
  `StateVersion` (versioned envelope), `EncodeState`, `DecodeState`. Version
  mismatch → ok=false → caller re-seeds (discard, don't migrate).
- **apply.go**: `mutatorSet` interface (seam for test fakes — the real
  `MutatorList.Add` consults the global spec registry, so unit tests inject a
  fake at this interface). `MutatorIdFor` maps a weather type to its
  `weather-*` mutator id ("" for calm). `applyChange` (Has-guard because
  `MutatorList.Add` appends duplicates when the mutator is already live).
  **`reconcileZone(ms, current []string, want string) bool`** — prefix-agnostic
  core: removes every id in `current` except `want`, then adds `want` if absent
  (`""` = remove all). Both reconcile layers call it; each caller gathers only
  its own prefix so the two namespaces never touch each other's ids.
  `Apply(diff)` walks a `StateDiff` and calls `applyChange` for
  each zone — an exported low-level primitive with **no production caller**
  today (M4's per-room refinement is its likely consumer).
  **`Reconcile(weather map)`** forces every zone in the map to match
  the resolved weather — used at boot, after state restore, and after a graph
  rebuild. `Reconcile` is the single path by which WEATHER state reaches
  engine mutators (tick, commands, exports, post-rebuild) — `ReconcileSeasons`
  below is its counterpart for the season namespace: because specs carry
  `decayrate`, a bare diff-apply would let engine-side decay drift persist.
  **`SeasonMutatorPrefix`** (`"season-"`) namespaces seasonal-ambience mutators;
  independent of `WeatherMutatorPrefix` — the two reconcile layers never touch
  each other's ids. **`SeasonMutatorId(track, season string) string`** maps a
  zone's resolved `(track, season)` to its mutator id; `""` when either part is
  empty (no seasonal mutator). **`ReconcileSeasons(g, zoneSeasons)`** forces
  every zone's `season-*` mutators to match its resolved season; zones **absent
  from the map** (unbound biomes) get their `season-*` mutators removed, so a
  zone whose biome loses its track binding heals automatically.
  `warnUnknownSeasonMutator` is a warn-once guard (same pattern as
  `warnedMutators`) for missing season specs.
  `StripBuffs()` clears buff id lists on all loaded **`weather-*` and
  `season-*`** specs — covers both namespaces; boot-time only, no restore path.
  `warnedMutators` warn-once map (safe on single goroutine).
- **calendar.go**: `CalendarShape() (monthsPerYear, daysPerYear int)` — reads
  the active calendar name from `gametime.GetDate().Calendar`, resolves its
  shape via `gametime.GetCalendar`, and falls back to the `"default"` calendar
  if the named one is absent or invalid. Returns `(0, 0)` when no usable
  calendar exists — the caller (`loadSeasons`) treats a zero shape as "seasons
  off". `CalendarNow() seasons.CalendarPos` — the current day-of-year for
  season resolution (`GameDate.Day` is 1-based and the engine subtracts whole
  years, so it is already the day-of-year). `DaysPerYear` is sourced from the
  same calendar shape lookup rather than from `GameDate.DaysPerYear` directly,
  ensuring consistency with `CalendarShape`.
- **clock.go**: `TickPeriod(hours int) string` — renders game-hour count as a
  `gametime.AddPeriod` period string; values < 1 clamp to 1. `NextTickRound`
  returns the round number one period from now. `CurrentRound` exposes
  `util.GetRoundCount`.
- **emotes.go**: `EmitAmbient(weather, zoneSeasons, tables, seasonal, roll)` —
  the single ambient-emote **arbiter**. Sends at most ONE line per occupied room
  per pass. Room biome drives the table variant; `isOutdoorBiome` determines the
  indoor/outdoor section. The arbiter (spec S-R1):
  1. If the room's zone has non-calm weather, send a **weather** line —
     season-variant-aware: it passes the zone's season (when `zoneSeasons` has
     one) into `tables.Pick`, so the season's variant lines win when present.
     Weather always wins; the room is done for this pass.
  2. Otherwise (calm zone) — only when the zone is season-bound **and** a
     1-in-`seasonalEmoteOneIn` roll passes — send a **seasonal-ambience** line
     from `seasonal.Pick(track, season, …)`. This is strictly quieter than
     weather: roughly one seasonal line per occupied calm room per few passes.
  `seasonalEmoteOneIn` is a package `const` (currently `3`); promote to config
  only if play feel demands. `zoneSeasons`/`seasonal` may be nil/empty when
  seasons are off (both layers stay silent; weather falls back to base lines via
  `season == ""`). `roll` is the presentation RNG (pass `util.Rand`) — NEVER the
  sim RNG. Returns lines sent.

## Dependencies
- `internal/rooms`, `internal/mutators`, `internal/gametime`, `internal/util`,
  `internal/mudlog` (engine).
- `github.com/GoMudEngine/GoMud/modules/weather/{sim,crawler,content,seasons}` — pure types.

## Consumers
- The module root (`weather.go`) uses `NewWorldReader()`, `DecodeCache`/`CacheIdentifier`.
- The module root (`weather_tick.go`) uses `EncodeState`/`DecodeState`,
  `TickPeriod`/`NextTickRound`/`CurrentRound`, `EmitAmbient`, `StripBuffs`,
  `CalendarShape`/`CalendarNow` (the seasons glue).
- The module root (`weather_commands.go`, `weather_api.go`) calls
  `Reconcile`/`CurrentRound` after any state mutation.

## Testing
- `cache_test.go` covers `DecodeCache` (pure).
- `worldreader_test.go` covers `isOutdoorBiome`.
- `state_test.go` covers `EncodeState`/`DecodeState` (pure, in-checkout).
- `apply_test.go` covers `MutatorIdFor`, `applyChange`, `reconcileZone`,
  `SeasonMutatorId`, and `ReconcileSeasons` (season namespace isolation) via
  a fake `mutatorSet` (in-checkout unit test).
- `clock_test.go` covers `TickPeriod` (pure, in-checkout).
- `WorldReader`, `Reconcile`, `EmitAmbient`, and `CalendarShape`/`CalendarNow`
  are thin engine-glue verified by the in-checkout build and boot smoke test.
  `Apply` itself is exercised only through `applyChange`'s unit tests (no
  production caller).
- These tests compile only inside a GoMud checkout (engine imports).

## Build note
This package compiles only inside a checkout (it imports `internal/*`). In the
standalone repo, test the pure core with `go test ./sim/... ./crawler/... ./content/...`.
