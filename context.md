# weather Package Context (module root)

## Overview
The root `weather` package is the GoMud plugin entry point. It registers the
plugin, loads config, builds/loads the geography graph, and serves the admin
`weather` command. It imports `internal/*` for plugin infrastructure
(plugins/events/users/mudlog/util/rooms); the engine-world reads and cache codec
live in the `engine` package, and the pure graph/algorithm live in `sim`/`crawler`.

## Key Components
- **weather.go**: `init()` → `plugins.New` + `AttachFileSystem` + `SetOnLoad`.
  `onLoad` loads config, registers the `weather` admin command and a `NewRound`
  listener. `onNewRound` builds the graph ONCE on the first round (guarded by
  `started`) — deferred from onLoad because onLoad's timing vs world-load is
  engine-specific. `loadOrBuildGraph`/`rebuildGraph` use `engine.DecodeCache`/
  `CacheIdentifier` + `plugin.ReadBytes`/`WriteBytes` to load-or-crawl-and-persist.
  `cmdWeather` dispatches `rebuild` / `graph [zone]` / summary. `sendLine` is the
  SOLE `user.SendText` call site — the one upstream-vs-DOGMud divergence.
- **weather_config.go**: `Config` (Enabled / IncludeSecretExits / RebuildGraphOnBoot),
  `buildConfig(getter)` (testable), `loadConfig(*plugins.Plugin)`.

## Dependencies
- `internal/plugins, events, users, mudlog, util, rooms` (engine, plugin infra).
- `modules/weather/{sim,crawler,engine}`.

## Threading
GoMud runs a single game-loop goroutine (MainWorker) for both NewRound listeners
and command handlers, so `graph`/`started` need no synchronization.

## Build/Testing
Compiles only inside a GoMud checkout (imports `internal/*`). `weather_config_test.go`
covers `buildConfig`; the registration/command/build paths are verified by the
in-checkout build and a boot smoke test (first-round build → cache persist →
reload).

## DOGMud backport
Only `user.SendText` differs (DOGMud takes a message category). It is isolated in
`sendLine` — a one-line change to backport. See CONTRIBUTING.md.
