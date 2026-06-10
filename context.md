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
  seasons.Tracks`, `seasonsOn bool`, `zoneSeasons map[sim.ZoneId]seasons.ZoneSeason`
  for the seasons layer). `init()` → `plugins.New` + `AttachFileSystem` +
  `SetOnLoad`, then registers the `weather` command as a **player** command
  (not admin-only; admin subcommands are gated in-handler) and the exports.
  Command/export registration MUST happen in `init()`, not `onLoad`:
  `plugins.Load()` harvests the plugin's `userCommands` map into the engine
  registry BEFORE invoking `onLoad`, so anything registered in `onLoad` is lost.
  Behavior is gated on `cfg.Enabled`/`simReady` in-handler instead. `onLoad`:
  loads config, then (when enabled) registers `SetOnSave` and a `NewRound`
  listener. `onNewRound`: one-time startup (loadOrBuildGraph + startSim), the
  jittered ambient-emote pass, and the coarse weather tick. `loadOrBuildGraph`/
  `rebuildGraph`: cache-or-crawl; `rebuildGraph` also calls `startSim`,
  `engine.Reconcile`, and (when `seasonsOn`) recomputes `m.zoneSeasons` and
  calls `engine.ReconcileSeasons` (post-rebuild heal — prevents stale-zone
  seasons surviving a graph rebuild). `sendLine` is the SOLE `user.SendText`
  call site.
- **weather_events.go**: exports `WeatherSeasonChanged{Zone, Track, From, To}`
  — queued on the engine event bus when a zone's resolved season flips. Never
  emitted on the first (baseline) resolution after boot, so reboots do not
  replay a flood of events. Other modules listen by importing this type:
  `events.RegisterListener(weather.WeatherSeasonChanged{}, handler)`. Implements
  `events.Event` via the `Type() string` method.
- **weather_tick.go**: `startSim` (idempotent; graceful degradation — logs once
  and stays idle when no graph exists). `loadContent` (climate overrides + emote
  tables from the embedded FS, both fail-soft). `loadSeasons` — fail-soft
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
Exported functions are invoked on the same goroutine.

## Build/Testing
Compiles only inside a GoMud checkout (imports `internal/*`). `weather_config_test.go`
covers `buildConfig`; the registration/command/tick/export paths are verified by
the in-checkout build and a boot smoke test (first-round build → state persist →
reload → tick).

## DOGMud backport
Only `user.SendText` differs (DOGMud takes a message category). It is isolated in
`sendLine` — a one-line change to backport. See CONTRIBUTING.md.
