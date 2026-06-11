# weather Package Context (module root)

## Overview
The root `weather` package is the GoMud plugin entry point. It wires the full
module lifecycle: plugin registration, config, geography-graph management,
weather-simulation tick, per-room refinement, ambient emotes, admin/player
commands, and the exported API. It imports `internal/*` for plugin
infrastructure (plugins/events/users/mudlog/util/rooms); engine-world calls
live in `engine/`; pure algorithms live in `sim`/`crawler`; data-file parsing
lives in `content/`. All fields of `weatherModule` are touched only from the
single game-loop goroutine — no synchronization needed.

## Key Components
- **weather.go**: the `files` embed.FS (`//go:embed files/*` — the
  active-defaults config overlay plus `datafiles/` mutator specs, buff
  specs, and emote tables; the
  engine loads `mutators/*` and `buffs/*` from it via the plugin registry,
  `content` loaders read the rest). `weatherModule` struct (plug, cfg, graph,
  started, simReady, simCfg, climate, tables, state, nextTick, nextEmote; plus
  `tracks seasons.Tracks`, `seasonsOn bool`,
  `zoneSeasons map[sim.ZoneId]seasons.ZoneSeason`, and
  `seasonalTables content.SeasonalTables` (the standalone seasonal-ambience
  emote tables) for the seasons layer; and `lastAdminAction string` carrying
  the most recent admin-page action result for the snapshot). `init()` →
  `plugins.New` + `AttachFileSystem` + `SetOnLoad`, then registers the `weather`
  command as a **player** command (not admin-only; admin subcommands are gated
  in-handler), the exports, and the admin web surface (`registerAdminWeb()`).
  Command, export, and web registration MUST happen in `init()`, not `onLoad`:
  `plugins.Load()` harvests the plugin's command map and admin web surface into
  the engine registry BEFORE invoking `onLoad`, so anything registered in `onLoad`
  is lost. Behavior is gated on `cfg.Enabled`/`simReady` in-handler instead.
  `onLoad`: runs `healConfigClobber` FIRST (the boot self-heal for the
  engine's overlay clobber — see weather_config.go below; ordering matters,
  the config read would otherwise adopt defaults over wiped operator values),
  then loads config, then (when enabled) registers `SetOnSave`, the
  `NewRound` listener, the **`RoomChange` listener** (`onRoomChange`, defined
  in weather_tick.go — refine-on-entry for `occupied` mode), and the two admin
  event listeners (`WeatherAdminAction`, `WeatherConfigChanged`). `onNewRound`:
  one-time startup (loadOrBuildGraph + startSim, followed by the entry point's
  single `publishSnapshot`), the jittered ambient-emote pass
  (`engine.EmitAmbient(m.state.Weather, m.zoneSeasons, m.tables, m.seasonalTables, util.Rand)`
  — the single arbiter; passes nil season maps harmlessly when seasons are off),
  and the coarse weather tick. `loadOrBuildGraph`/`rebuildGraph`:
  cache-or-crawl; `rebuildGraph` reads `cfg.ExcludeZonePatterns` into the
  crawler options, and also calls `startSim`, `applyWeather` (the mode-aware
  re-assert), and (when `seasonsOn`) recomputes `m.zoneSeasons` and calls
  `engine.ReconcileSeasons` (post-rebuild heal — prevents stale-zone seasons
  surviving a graph rebuild). Neither `startSim` nor `rebuildGraph` publishes a
  snapshot — they are helpers, not entry points (single-publish rule; see
  weather_admin.go). `sendLine` is the SOLE `user.SendText` call site.
- **weather_events.go**: exports `WeatherSeasonChanged{Zone, Track, From, To}`
  — queued on the engine event bus when a zone's resolved season flips. Never
  emitted on the first (baseline) resolution after boot, so reboots do not
  replay a flood of events. Other modules listen by importing this type:
  `events.RegisterListener(weather.WeatherSeasonChanged{}, handler)`. Also
  defines the two internal admin bridges: `WeatherAdminAction{Action, Weather,
  Zone, Intensity}` — queued by HTTP handlers, executed on the game loop through
  the same paths as the in-game admin commands (`spawn` / `clear` / `rebuild`);
  and `WeatherConfigChanged{Key}` — queued after a config write is persisted,
  causing the game loop to re-read the config and run the changed key's live
  applier. All three implement `events.Event` via `Type() string`.
- **weather_tick.go**: `startSim` (idempotent; graceful degradation — logs once
  and stays idle when no graph exists; order: simConfig → loadContent →
  loadSeasons → **applyBuffConfig** → loadOrInitState → **applyWeather** →
  schedule tick/emote). **`applyBuffConfig`** — the boot-time buff phase, in
  this order and NEVER the reverse: `engine.ApplyBuffOverrides(cfg.BuffOverrides)`
  first (when any are set), then `engine.StripBuffs()` when
  `BuffsEnabled: false` — so disabling buffs always wins, overrides included.
  Both are seamed (`applyBuffOverridesFn`/`stripBuffsFn`) because they mutate
  the global spec registry, which is empty under `go test`; an ordering test
  swaps the seams. **`applyWeather`** — the single switch between zone-scoped
  and room-scoped weather application; every path that asserts weather
  mutators (startSim, tick, rebuildGraph, spawn/clear commands, exports, admin
  actions, the PerRoomRefinement live-apply) funnels through it.
  `RefineOff` → `engine.Reconcile(m.state.Weather)` (the v1 zone-wide path);
  the room modes first `engine.StripZoneWeather(m.graph)` (rooms become the
  only carriers), then `RefineAll` walks `ZoneRoomIds` per zone calling
  `engine.RefineRoom` (force-loads by design — the documented cost of "all"),
  while `RefineOccupied` (default) calls `engine.RefineOccupiedRooms`. Seasons
  are always zone-wide and untouched here. **`onRoomChange`** — keeps
  `occupied` mode current between ticks: ignores mob moves (`UserId == 0`) and
  no-ops unless `simReady && PerRoomRefinement == occupied`; always refines the
  destination room (logins fire `RoomChange` with From==To; RefineRoom is
  idempotent, steady state is zero mutator ops) and strips the departed room
  once it has no players. Deliberately does NOT publishSnapshot — RefinedRooms
  lags until the next tick by design. `loadContent` (climate overrides, weather
  emote tables, and the seasonal-ambience tables — `content.LoadSeasonalEmotes`
  into `m.seasonalTables` — from the embedded FS, all fail-soft). `loadSeasons` — fail-soft
  ladder: `SeasonsEnabled: false` → skip; no usable calendar → skip; no/invalid
  tracks → skip; each rejection leaves `seasonsOn = false` so weather runs
  exactly as v1. On success sets `m.seasonsOn = true`, stores tracks, and
  calls `seasons.ZoneSeasons` to establish the **baseline** `zoneSeasons` map,
  immediately followed by **`engine.ReconcileSeasons(m.graph, m.zoneSeasons)`**
  (boot assert — re-asserts `season-*` mutators after a reboot since zone
  mutators do not survive reboots; no events emitted). `loadOrInitState`
  (restore from `engine.DecodeState`, or `sim.NewState`/`sim.DeriveSeed` on a
  fresh start). `tick` — when `seasonsOn`, calls `seasons.EffectiveClimate` to
  produce the climate input for `sim.Step` (the seasonsOn gate); then Step →
  `applyWeather` (reconcile-style rather than a bare diff-apply, so engine-side
  `decayrate` drift self-corrects within one tick); then `resolveSeasons` if
  `seasonsOn`; ends with the entry point's one `publishSnapshot`.
  `resolveSeasons` — re-resolves all zone seasons and queues a
  `WeatherSeasonChanged` event for each flip since the previous tick; calls
  **`engine.ReconcileSeasons(m.graph, zs)`** after storing the new map
  (per-tick assert — keeps `season-*` mutators live against the specs' `decayrate`
  safety net); **cross-track-change suppression**: season-change events are only
  emitted when `prev.Track == cur.Track && prev.Season != cur.Season`, so a zone
  whose biome was reassigned by an admin rebuild emits nothing (listeners may
  assume `From`/`To` are seasons of the same track). `persistState` (cheap;
  called per-tick, from onSave, and from every command/export mutation path).
  `onSave` (plugins.Save hook). `scheduleEmote` (±25% jitter so ambience doesn't
  metronome).
- **weather_commands.go**: bare `weather` shows local conditions (player view;
  includes the dominant front via `sim.Covering`; when `seasonsOn` also prints
  the zone's current season). Subcommands `zones`, `fronts`, `spawn <type>
  <zone> [intensity]`, `clear [zone]`, `graph [zone]`, `rebuild`, `status`,
  `seasons` are admin/mod-gated via `HasRolePermission("weather", true)`.
  `spawn` and `clear` call `sim.ForceSpawn`/`sim.ClearZones` then
  `applyWeather` + `persistState` + `publishSnapshot` (command handlers are
  entry points). `seasons` (`printSeasons`) lists every loaded track with its
  current season and blend percentage when inside a transition window; reports
  "off" when `seasonsOn` is false.
- **weather_api.go**: `registerExports` exposes `GetWeather`, `GetFronts`,
  `SpawnFront`, `GetSeason` via `plugin.ExportFunction`. All four guard
  `simReady` so callers during boot get empty-but-valid answers. The
  MainWorker-goroutine guarantee applies to mutating exports (same as commands).
  `SpawnFront` calls `applyWeather` + `persistState` + `publishSnapshot`.
  `GetSeason(zone) map[string]any` returns `{"track": string, "season": string,
  "blend": float64}` from `m.zoneSeasons`; empty strings when seasons are off,
  the zone is unknown, or its biome is unbound.
- **weather_admin.go**: the read-side bridge between the game loop and the HTTP
  layer. `AdminSnapshot` struct — an immutable deep-copy of module state
  (`SimReady`, `SeasonsOn`, `Round`, `NextTickRound`, graph summary,
  **`RefinementMode`** (the live `PerRoomRefinement` value) and
  **`RefinedRooms`** (`engine.OccupiedRoomCount()` whenever a room mode is
  active — the OCCUPIED-room count even in `all` mode, and labeled "occupied
  rooms" on the page; 0 when `off`), `Fronts`, `Zones`, `Config` rows,
  `LastAction`) serialized to JSON for the status endpoint. Package-level
  `adminSnapshot atomic.Pointer[AdminSnapshot]` — **written only from the game
  loop**; HTTP handlers call `loadSnapshot()` to read it and never touch live
  module fields. **Single-publish rule** — the canonical statement lives on
  `publishSnapshot`'s doc comment: helpers that mutate state on a caller's
  behalf (`rebuildGraph`, `startSim`, …) never publish; every game-loop ENTRY
  POINT that mutates snapshot-visible state (`onNewRound` startup, `tick`,
  command handlers, exports, `applyAdminAction`, `applyConfigChange`) publishes
  exactly once, at its end, after any `lastAdminAction` attribution is in place
  — so success and failure alike surface exactly once, correctly attributed
  (`TestAdminRebuildPublishesOnce` pins the rebuild path).
  `configKeyMeta map[string]configKeyApplier` — the **single source of truth**
  for every public config key: badge text, input `Kind` (`bool`/`int`/`float`/
  `enum`/`text`) with `Options` for enums, `ReadOnly` for synthetic rows (the
  one `BuffOverrides.*` summary row — set in the world's config-overrides.yaml,
  never via the API), a `Validate` func, and an optional `LiveApply` func (run on the game
  loop when the key changes). Validators mirror `buildConfig`'s coercion rules
  but REJECT what the loader would silently default or clamp (floors are the
  shared `min*` constants in weather_config.go), returning a normalized string
  to persist (canonical bool, lowercased enum). Notable live appliers:
  `PerRoomRefinement` calls `engine.StripOccupiedRoomWeather()` when leaving a
  room mode for `off` (unoccupied strays are left to the specs' decayrate
  safety net) and then `applyWeather()`; `BuffsEnabled` live-strips on
  true→false only (no restore path — the badge says reboot to re-enable);
  `SeasonsEnabled` tears down or re-runs `loadSeasons`. `configRows()` reads
  `configKeyMeta` to build the `Config` slice in the snapshot — every `Value`
  must be a scalar (slice/map config like `ExcludeZonePatterns` and
  `BuffOverrides` is rendered to fresh strings, the latter via
  `buffOverridesSummary`) so snapshots stay isolated.
  `applyAdminAction(WeatherAdminAction)` — executes spawn / clear / rebuild on
  the game loop, mirrors the in-game command paths, then publishes.
  `applyConfigChange(Config, key)` — adopts a freshly re-read config, runs the
  key's `LiveApply` if present and the sim is ready, then publishes.
  `onAdminAction` / `onConfigChanged` — the listener glue wiring the two event
  types to the appliers; both registered in `onLoad`, both run on the game loop.
- **weather_admin_api.go**: the HTTP registration and handler layer.
  `registerAdminWeb()` — called from `init()` (the harvest rule: the engine
  harvests admin web surface at `plugins.Load()`, before `onLoad`, same as
  commands). Wires: `AdminPage("Weather", "weather", "html/admin/weather.html",
  ...)` — the `htmlFile` argument is relative to `datafiles/` (NOT
  `datafiles/html/admin/` as the engine doc comment implies; the loader reads the
  plugin-FS key verbatim — see `plugins.go:535`); the page is served at
  `/admin/weather`. Three endpoints: `GET weather/status` (open to any admin
  session), `POST weather/config` (requires `weather.write`), `POST
  weather/action` (requires `weather.write`). `RegisterPermissions(weather.write)`.
  `handleAdminConfig` rejects with **400** any unknown key, any write to a
  `ReadOnly` row, and any value its `Validate` func refuses (the error message
  names the key); what it persists is the validator's NORMALIZED value, so the
  overrides file never accumulates values the next boot would quietly rewrite.
  Persistence goes through the `persistConfigFn` seam (Set + read-back), and
  the handler then verifies the write actually took: the engine's
  `PluginConfig.Set` DISCARDS `configs.SetVal`'s error
  (internal/plugins/pluginconfig.go:13), so a rejected write (e.g. an
  unregistered key) looks identical to success — the read-back guard compares
  `Get` against the normalized value (`configValuesEqual`) and answers **500**
  ("the engine rejected this write") without queueing the changed event.
  `handleAdminAction` shape-validates (spawn needs type + zone) and queues.
  Handlers are **strictly limited** to three touches: (1) `loadSnapshot()` on
  the atomic pointer (read-only); (2) `m.plug.Config.Set`/`Get` via
  `persistConfigFn` in `handleAdminConfig` (the engine config layer, which is
  internally locked); (3)
  `events.AddToQueue` in both write handlers (the event queue is thread-safe).
  Handlers never access any other `weatherModule` field.
- **weather_config.go**: `Config` struct (Enabled, IncludeSecretExits,
  RebuildGraphOnBoot, Seed, TickEveryGameHours, MaxActiveFronts, SpawnRateScale,
  EmoteMode, EmoteEveryRounds, BuffsEnabled, Persist, `SeasonsEnabled`,
  `PerRoomRefinement`, `BuffOverrides map[string][]int`,
  `ExcludeZonePatterns []string`). Keys are flat because plugin config lookup
  reads flattened scalar leaves. `SeasonsEnabled` defaults `true`; setting it
  to `false` causes `loadSeasons` to return immediately, leaving
  `seasonsOn = false` and weather running exactly as v1. **`PerRoomRefinement`**
  is one of the `RefineOccupied`/`RefineAll`/`RefineOff` constants (default
  `occupied`; anything else falls back to the default). **`BuffOverrides`** is
  read by `buffOverrides(get)`, which probes the flat key
  `BuffOverrides.<type>` for every entry of `sim.KnownWeatherTypes` ("clear"
  included — it has no mutator, so an override for it warns at apply time);
  `parseBuffIds` parses one value (comma-separated positive ints; an empty
  string = explicit strip = empty list; bad tokens warn-once via
  `warnConfigOnce` and are skipped; a value with NO usable ids is dropped
  entirely — fail-soft to the shipped buffs). **`ExcludeZonePatterns`** is one
  comma-separated flat key parsed by `excludePatterns`; absent/empty falls back
  to `crawler.DefaultOptions().ExcludeZonePatterns` (there is no "exclude
  nothing" sentinel). The `min*` floor constants (minSeed,
  minTickEveryGameHours, minMaxActiveFronts, minEmoteEveryRounds,
  minSpawnRateScale) are shared by `buildConfig`'s clamps and the admin
  validators so loader and write-side validation can never drift apart.
  `buildConfig(getter)` (testable, applies defaults and sanity clamps) carries
  the **code defaults** — `Enabled` defaults TRUE when absent (OOBE) — and the
  shipped data overlay (`files/data-overlays/config.yaml`) restates the SAME
  values as ACTIVE keys (pinned identical by `TestOverlayMatchesCodeDefaults`):
  active overlay keys are the only thing that registers `Modules.weather.*`
  with the engine's config layer, without which `configs.SetVal` — the admin
  page's write path — rejects every write ("invalid property name", an error
  `PluginConfig.Set` silently discards). The flip side is the engine's overlay
  clobber: `configs.AddOverlayOverrides` REPLACES the live `Modules.weather`
  block (instead of merging) whenever the overlay carries a key the operator's
  `config-overrides.yaml` block lacks — i.e. the first boot after a module
  update once the admin page ever wrote the block. **`healConfigClobber`**
  (called from `onLoad` BEFORE the config read) is the boot self-heal:
  `healClobberedConfig` (testable core; seams `readOverridesFn`/`infoConfig`,
  plus the injected get/set) reads the operator's `config-overrides.yaml`
  (path mirrors the engine's `overridePathNoLock`: `CONFIG_PATH` env var, else
  `FilePaths.DataFiles`), extracts the `Modules.weather` block
  (case-insensitive), flattens it to dotted keys, and compares each file value
  against the live config (`configValuesEqual` — canonical-string compare,
  the two sides never share a type system). Any mismatch means the clobber
  fired; ONE `plug.Config.Set` of a registered key restores everything,
  because the engine's `SetVal` re-applies its ENTIRE in-memory overrides
  union (which still holds the operator's file values) and writes the
  completed union back to the file, so the clobber never fires again. The heal
  re-verifies via Get, logs one Info on success
  ("Weather: restored operator config after engine overlay clobber"), warns
  and falls through on any IO/parse/verify failure (never blocks boot; code
  defaults keep the module functional). `registeredConfigKey`/
  `writableConfigKeys` derive the registered set from `configKeyMeta`.
  `simConfig()` maps module config onto `sim.Config`. `loadConfig(*plugins.Plugin)`.

## Dependencies
- `internal/plugins, events, users, mudlog, util, rooms, configs` (engine,
  plugin infra; `configs` only for the overrides-file path in
  `healConfigClobber`).
- `modules/weather/{sim,crawler,engine,content,seasons}`; `gopkg.in/yaml.v2`
  (parsing config-overrides.yaml in the heal — same yaml the engine uses).

## Threading
GoMud runs a single game-loop goroutine (MainWorker) for both event listeners
(NewRound, RoomChange, the admin bridges) and command handlers, so
`weatherModule` fields need no synchronization. Exported functions are invoked
on the same goroutine. **Designed exception surface — HTTP handlers:**
`handleAdminStatus`, `handleAdminConfig`, and `handleAdminAction` in
`weather_admin_api.go` run on web goroutines outside MainWorker. They are
permitted to touch exactly three things: the `adminSnapshot` atomic pointer
(read-only via `loadSnapshot()`), the engine config layer (via
`m.plug.Config.Set`/`Get` through the `persistConfigFn` seam — internally
locked), and the engine event queue
(via `events.AddToQueue`, which is thread-safe). Any access to other
`weatherModule` fields from a handler is a concurrency bug.

## Build/Testing
Compiles only inside a GoMud checkout (imports `internal/*`).
`weather_config_test.go` covers `buildConfig` (defaults, coercion, clamps, the
refinement-mode and BuffOverrides/ExcludeZonePatterns parsing), the
`applyBuffConfig` ordering (overrides before strip, via the seams), the
overlay/code-defaults pin (`TestOverlayMatchesCodeDefaults`), and
`healClobberedConfig` (the `TestHeal*` family: no file, no block, steady
state, partial clobber, nested BuffOverrides, unregistered-only mismatch,
parse garbage, rejected write, case-insensitive block lookup — all through
fabricated YAML and fake get/set; the real proof is the boot smokes).
`weather_admin_test.go` covers the snapshot builder (incl. the refinement
fields and isolation), `configKeyMeta` completeness, validators and
normalization, the handlers' 400 paths, the read-back guard
(`TestConfigHandlerReadBackGuard`: accepted/rejected/silently-unchanged
writes via the `persistConfigFn` seam), the live appliers (refinement-mode
transitions, seasons toggle), and the single-publish rule on the rebuild path.
The registration/command/tick/export paths are verified by the in-checkout
build and a boot smoke test (first-round build → state persist → reload →
tick).

## DOGMud backport
Only `user.SendText` differs (DOGMud takes a message category). It is isolated in
`sendLine` — a one-line change to backport. See CONTRIBUTING.md.
