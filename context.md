# weather Package Context (module root)

## Overview
The root `weather` package is the GoMud plugin entry point. It wires the full
module lifecycle: plugin registration, config, geography-graph management,
weather-simulation tick, ambient emotes, admin/player commands, and the exported
API. It imports `internal/*` for plugin infrastructure
(plugins/events/users/mudlog/util/rooms); engine-world calls live in `engine/`;
pure algorithms live in `sim`/`crawler`; data-file parsing lives in `content/`.
All fields of `weatherModule` are touched only from the single game-loop
goroutine — no synchronization needed.

## Key Components
- **weather.go**: the `files` embed.FS (`//go:embed files/*` — the config
  overlay plus `datafiles/` mutator specs and emote tables; the engine loads
  `mutators/*` from it via the plugin registry, `content` loaders read the
  rest). `weatherModule` struct (plug, cfg, graph, started, simReady,
  simCfg, climate, tables, state, nextTick, nextEmote; plus `tracks
  seasons.Tracks`, `seasonsOn bool`, `zoneSeasons map[sim.ZoneId]seasons.ZoneSeason`,
  and `seasonalTables content.SeasonalTables` (the standalone seasonal-ambience
  emote tables) for the seasons layer; and `lastAdminAction string` carrying
  the most recent admin-page action result for the snapshot). `init()` →
  `plugins.New` + `AttachFileSystem` + `SetOnLoad`, then registers the `weather`
  command as a **player** command (not admin-only; admin subcommands are gated
  in-handler), the exports, and the admin web surface (`registerAdminWeb()`).
  Command, export, and web registration MUST happen in `init()`, not `onLoad`:
  `plugins.Load()` harvests the plugin's command map and admin web surface into
  the engine registry BEFORE invoking `onLoad`, so anything registered in `onLoad`
  is lost. Behavior is gated on `cfg.Enabled`/`simReady` in-handler instead.
  `onLoad`: loads config, then (when enabled) registers `SetOnSave`, a `NewRound`
  listener, and the two admin event listeners (`WeatherAdminAction`,
  `WeatherConfigChanged`). `onNewRound`: one-time startup (loadOrBuildGraph + startSim), the
  jittered ambient-emote pass
  (`engine.EmitAmbient(m.state.Weather, m.zoneSeasons, m.tables, m.seasonalTables, util.Rand)`
  — the single arbiter; passes nil season maps harmlessly when seasons are off),
  and the coarse weather tick. `loadOrBuildGraph`/
  `rebuildGraph`: cache-or-crawl; `rebuildGraph` also calls `startSim`,
  `engine.Reconcile`, and (when `seasonsOn`) recomputes `m.zoneSeasons` and
  calls `engine.ReconcileSeasons` (post-rebuild heal — prevents stale-zone
  seasons surviving a graph rebuild). `sendLine` is the SOLE `user.SendText`
  call site.
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
  and stays idle when no graph exists). `loadContent` (climate overrides, weather
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
  `engine.Reconcile` (rather than bare Apply so engine-side `decayrate` drift
  self-corrects within one tick); then `resolveSeasons` if `seasonsOn`.
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
  `engine.Reconcile` + `persistState`. `seasons` (`printSeasons`) lists every
  loaded track with its current season and blend percentage when inside a
  transition window; reports "off" when `seasonsOn` is false.
- **weather_api.go**: `registerExports` exposes `GetWeather`, `GetFronts`,
  `SpawnFront`, `GetSeason` via `plugin.ExportFunction`. All four guard
  `simReady` so callers during boot get empty-but-valid answers. The
  MainWorker-goroutine guarantee applies to mutating exports (same as commands).
  `SpawnFront` calls `engine.Reconcile` + `persistState`. `GetSeason(zone)
  map[string]any` returns `{"track": string, "season": string, "blend":
  float64}` from `m.zoneSeasons`; empty strings when seasons are off, the zone
  is unknown, or its biome is unbound.
- **weather_admin.go**: the read-side bridge between the game loop and the HTTP
  layer. `AdminSnapshot` struct — an immutable deep-copy of module state
  (`SimReady`, `SeasonsOn`, `Round`, `NextTickRound`, graph summary, `Fronts`,
  `Zones`, `Config` rows, `LastAction`) serialized to JSON for the status
  endpoint. Package-level `adminSnapshot atomic.Pointer[AdminSnapshot]` —
  **written only from the game loop**; HTTP handlers call `loadSnapshot()` to
  read it and never touch live module fields. Snapshot refresh sites:
  `publishSnapshot()` is called at the end of `startSim`, at the end of `tick`,
  at the end of `rebuildGraph`, and at the end of every `applyAdminAction` and
  `applyConfigChange` invocation. `configKeyMeta map[string]configKeyApplier`
  — the **single source of truth** for every public config key: maps each key to
  its badge text (shown on the page via `configRows()`) and its optional
  `LiveApply` function (run on the game loop when the key changes). `configRows()`
  reads `configKeyMeta` to build the `Config` slice in the snapshot so the page
  renders exactly what the module will do. `applyAdminAction(WeatherAdminAction)`
  — executes spawn / clear / rebuild on the game loop, mirrors the in-game
  command paths, then refreshes the snapshot. `applyConfigChange(Config, key)`
  — adopts a freshly re-read config, runs the key's `LiveApply` if present and
  the sim is ready, then refreshes the snapshot. `onAdminAction` /
  `onConfigChanged` — the listener glue wiring the two event types to the
  appliers; both registered in `onLoad`, both run on the game loop.
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
  Handlers are **strictly limited** to three touches: (1) `loadSnapshot()` on
  the atomic pointer (read-only); (2) `m.plug.Config.Set` in
  `handleAdminConfig` (the engine config layer, which is internally locked); (3)
  `events.AddToQueue` in both write handlers (the event queue is thread-safe).
  Handlers never access any other `weatherModule` field.
- **weather_config.go**: `Config` struct (Enabled, IncludeSecretExits,
  RebuildGraphOnBoot, Seed, TickEveryGameHours, MaxActiveFronts, SpawnRateScale,
  EmoteMode, EmoteEveryRounds, BuffsEnabled, Persist, `SeasonsEnabled`). Keys
  are flat because plugin config lookup reads flattened scalar leaves.
  `SeasonsEnabled` defaults `true`; setting it to `false` causes `loadSeasons`
  to return immediately, leaving `seasonsOn = false` and weather running
  exactly as v1. `buildConfig(getter)` (testable, applies defaults and sanity
  clamps). `simConfig()` maps module config onto `sim.Config`.
  `loadConfig(*plugins.Plugin)`.

## Dependencies
- `internal/plugins, events, users, mudlog, util, rooms` (engine, plugin infra).
- `modules/weather/{sim,crawler,engine,content,seasons}`.

## Threading
GoMud runs a single game-loop goroutine (MainWorker) for both NewRound listeners
and command handlers, so `weatherModule` fields need no synchronization.
Exported functions are invoked on the same goroutine. **Designed exception
surface — HTTP handlers:** `handleAdminStatus`, `handleAdminConfig`, and
`handleAdminAction` in `weather_admin_api.go` run on web goroutines outside
MainWorker. They are permitted to touch exactly three things: the
`adminSnapshot` atomic pointer (read-only via `loadSnapshot()`), the engine
config layer (via `m.plug.Config.Set`, which is internally locked), and the
engine event queue (via `events.AddToQueue`, which is thread-safe). Any access
to other `weatherModule` fields from a handler is a concurrency bug.

## Build/Testing
Compiles only inside a GoMud checkout (imports `internal/*`). `weather_config_test.go`
covers `buildConfig`; the registration/command/tick/export paths are verified by
the in-checkout build and a boot smoke test (first-round build → state persist →
reload → tick).

## DOGMud backport
Only `user.SendText` differs (DOGMud takes a message category). It is isolated in
`sendLine` — a one-line change to backport. See CONTRIBUTING.md.
