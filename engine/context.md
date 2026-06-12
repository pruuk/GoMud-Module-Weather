# engine Package Context

## Overview
`engine` concentrates all direct engine-world calls (room/zone/biome reads,
mutator application — zone-scoped and room-scoped, game clock, ambient emotes,
cache codec) and implements `crawler.WorldReader`. Together with the root
`weather` package — which imports `internal/*` for plugin infrastructure —
these are the only packages that touch the engine; `sim`, `crawler`, `content`,
and `seasons` stay pure. That split keeps the module portable across GoMud and
DOGMud.

## Key Components
### Core Files
- **worldreader.go**: `WorldReader` implements `crawler.WorldReader` over
  `internal/rooms` (`GetAllZoneNames`, `GetZoneBiome`, `GetAllZoneRoomsIds`,
  `LoadRoom`). `NewWorldReader()` returns it as the interface. `isOutdoorBiome`
  derives a room's outdoor flag from its biome id (GoMud has no explicit
  indoor/outdoor flag), using the `indoorBiomes` heuristic set (unknown/empty
  ids count as outdoors). Also used by `EmitAmbient` (indoor emote sections)
  and by `RefineRoom` (outdoor spec vs `-indoor` variant — the heuristic now
  picks mutators, not just prose).
- **cache.go**: `CacheIdentifier` (the plugin-storage key) and `DecodeCache`,
  a pure, version-checked decoder that returns ok=false for absent/empty/
  unparseable/stale data so the caller knows to rebuild.
- **state.go**: `StateIdentifier` (plugin-storage key "simstate"),
  `StateVersion` (versioned envelope), `EncodeState`, `DecodeState`. Version
  mismatch → ok=false → caller re-seeds (discard, don't migrate).
- **apply.go**: `mutatorSet` interface (`Add`/`Remove` — the seam for test
  fakes: the real `MutatorList.Add` consults the global spec registry, so unit
  tests inject a fake at this interface). `MutatorIdFor` maps a weather type to
  its `weather-*` mutator id ("" for calm).
  **`reconcileList(ms, current []string, want string) bool`** — the
  prefix-agnostic core shared by every reconcile layer (zone weather, zone
  seasons, room weather): removes every id in `current` except `want`, then
  adds `want` if absent (`""` = remove all; steady state is ZERO ops — a
  re-add would reset spawn timing and re-fire the entry message). Each caller
  gathers only its own prefix (`weatherIds` in refine.go, the season loop in
  `ReconcileSeasons`), so the namespaces never touch each other's ids. Returns
  false when the want spec id is unknown; callers warn once via
  `warnUnknownMutatorId` / `warnUnknownSeasonMutator`. (The former low-level
  `Apply(diff)`/`applyChange` pair was RETIRED in M4 — it never gained a
  production caller; per-room refinement reuses `reconcileList` directly.)
  **`Reconcile(weather map)`** forces every zone in the map to match the
  resolved weather — the zone-scoped path, used only when
  `PerRoomRefinement: off` (the module root's `applyWeather` is the switch).
  Reconcile-style (rather than diff-apply) because specs carry `decayrate`:
  engine-side decay drift must self-correct within one tick.
  **`SeasonMutatorPrefix`** (`"season-"`) namespaces seasonal mutators;
  independent of `WeatherMutatorPrefix`. **`SeasonMutatorId(track, season)`**
  maps a zone's resolved season to its mutator id; `""` when either part is
  empty. **`ReconcileSeasons(g, zoneSeasons)`** forces every zone's `season-*`
  mutators to match its resolved season; zones **absent from the map** (unbound
  biomes) get their `season-*` mutators removed, so a zone whose biome loses
  its track binding heals automatically. Seasons are zone-scoped in EVERY
  refinement mode. `StripBuffs()` clears buff id lists on all loaded
  **`weather-*` and `season-*`** specs — covers both namespaces; boot-time
  only, no restore path. **`ApplyBuffOverrides(map[type][]ids)`** replaces
  `PlayerBuffIds` on OUTDOOR `weather-<type>` specs (empty list = strip; indoor
  variants and Mob/Native lists untouched; ids copied, never aliased — the
  spec must not alias the config map's backing arrays). Same boot-time
  spec-mutation mechanism as `StripBuffs`; the module always runs it BEFORE
  StripBuffs so `BuffsEnabled: false` wins over any override. The core
  `applyBuffOverrides` takes the registry lookup as a seam for tests; returns
  the count of specs changed; `warnedOverrides` warn-once for entries naming no
  loaded spec (a typo, or "clear" — calm is the absence of a mutator).
  `warnedMutators` warn-once map (safe on single goroutine).
- **refine.go**: the M4 per-room refinement core. In room modes, weather
  mutators live on individual room `MutatorList`s instead of the zone list;
  the list is the isolation boundary and `reconcileList` does the work.
  `roomWantId(w, indoor)` (unexported) maps zone weather + the room's indoor
  flag to `weather-<type>` / `weather-<type>-indoor` / `""` for calm.
  **`RefineRoom(roomId, weather)`** reconciles one live room: loads it
  (`rooms.LoadRoom`), derives indoor from its biome, gathers the room's
  `weather-*` ids, and reconciles to the want id (missing variant specs warn
  once via `warnUnknownMutatorId`). Idempotent; steady state is zero ops.
  **`RefineOccupiedRooms(weather)`** refines every room in
  `rooms.GetRoomsWithPlayers()` — never force-loads. **`ZoneRoomIds(zone)`**
  is the thin wrapper over `rooms.GetAllZoneRoomsIds` the root's `all` mode
  walks (refining unloaded rooms force-loads them — the documented cost of
  `all`). **`RoomHasPlayers(roomId)`** — the departure check for
  refine-on-entry; a room a player just left is necessarily still in the room
  cache, so it never force-loads in practice. **`OccupiedRoomCount()`** — the
  snapshot's "occupied rooms" stat (cheap; the room manager keeps the set
  incrementally). **`StripOccupiedRoomWeather()`** / **`StripRoomWeather(roomId)`**
  — the transition OUT of a room mode (the `PerRoomRefinement` live-apply) and
  the empty-room cleanup on `RoomChange`; strays in unoccupied rooms are left
  to the specs' `decayrate` safety net — the engine runs the mutator lifecycle
  on room load (`Prepare`) and each `RoundTick`, so stale persisted room
  mutators heal lazily without a world-wide pass.
  **`StripZoneWeather(g)`** — the transition INTO a room mode: removes
  zone-level `weather-*` mutators from every zone so rooms are the only
  carriers (gathers the weather- prefix only — seasons untouched by
  construction). `weatherIds(ml)` (unexported) gathers live `weather-*` ids
  from one `mutators.MutatorList` (room or zone — same type).
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
  `ApplyBuffOverrides`, `CalendarShape`/`CalendarNow` (the seasons glue); its
  `applyWeather` switch calls `Reconcile` (mode `off`) or
  `StripZoneWeather` + `RefineOccupiedRooms`/`ZoneRoomIds`+`RefineRoom`
  (room modes); its `onRoomChange` calls `RefineRoom`, `RoomHasPlayers`, and
  `StripRoomWeather`.
- The module root (`weather_commands.go`, `weather_api.go`) funnels every state
  mutation through `applyWeather` (which lands here) and reads `CurrentRound`.
- The module root (`weather_admin.go`) reads `OccupiedRoomCount` for the
  snapshot's RefinedRooms stat, and the `PerRoomRefinement` live-apply calls
  `StripOccupiedRoomWeather`.

## Testing
- `cache_test.go` covers `DecodeCache` (pure).
- `worldreader_test.go` covers `isOutdoorBiome`.
- `state_test.go` covers `EncodeState`/`DecodeState` (pure, in-checkout).
- `apply_test.go` covers `MutatorIdFor`, `reconcileList` (incl. the
  steady-state zero-op pin), `SeasonMutatorId`, season-namespace isolation, and
  `applyBuffOverrides` via a fake registry lookup (in-checkout unit tests).
- `refine_test.go` covers `roomWantId` and the `reconcileList` ∘ `roomWantId`
  composition `RefineRoom` uses in production (outdoor type change, stale
  outdoor id healing to the indoor variant, clear-strips-all, steady-state
  zero ops for both variants) via the fake `mutatorSet`.
- `clock_test.go` covers `TickPeriod` (pure, in-checkout).
- `WorldReader`, `Reconcile`, `ReconcileSeasons`, the room loaders in
  refine.go, `EmitAmbient`, and `CalendarShape`/`CalendarNow` are thin
  engine-glue verified by the in-checkout build and boot smoke test.
- These tests compile only inside a GoMud checkout (engine imports).

## Build note
This package compiles only inside a checkout (it imports `internal/*`). In the
standalone repo, test the pure core with
`go test ./sim/... ./crawler/... ./content/... ./seasons/...`.
