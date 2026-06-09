# engine Package Context

## Overview
`engine` is the weather module's adapter to the GoMud engine. It is the ONLY
package in the module that imports the engine (`internal/*`). Keeping every
engine call here is what makes the rest of the module (`sim`, `crawler`) pure
and portable across GoMud and DOGMud.

## Key Components
### Core Files
- **worldreader.go**: `WorldReader` implements `crawler.WorldReader` over
  `internal/rooms` (`GetAllZoneNames`, `GetZoneBiome`, `GetAllZoneRoomsIds`,
  `LoadRoom`). `NewWorldReader()` returns it as the interface. `isOutdoorBiome`
  derives a room's outdoor flag from its biome id (GoMud has no explicit
  indoor/outdoor flag), using the `indoorBiomes` heuristic set.
- **cache.go**: `CacheIdentifier` (the plugin-storage key) and `DecodeCache`,
  a pure, version-checked decoder that returns ok=false for absent/empty/
  unparseable/stale data so the caller knows to rebuild.

## Dependencies
- `internal/rooms` (engine) — the live world.
- `github.com/GoMudEngine/GoMud/modules/weather/{sim,crawler}` — pure types.

## Consumers
- The module root (`weather.go`) uses `NewWorldReader()` to crawl and
  `DecodeCache`/`CacheIdentifier` to load/save the graph cache.

## Testing
- `cache_test.go` covers `DecodeCache` (pure). `worldreader_test.go` covers
  `isOutdoorBiome`. The `WorldReader` engine methods are thin glue verified by
  the module's first-round build and the `weather` command smoke test. These
  tests compile only inside a GoMud checkout (engine imports).

## Build note
This package compiles only inside a checkout (it imports `internal/*`). In the
standalone repo, test the pure core with `go test ./sim/... ./crawler/...`.
