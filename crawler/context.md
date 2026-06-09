# crawler Package Context

## Overview
`crawler` builds a zone-adjacency `sim.Graph` from a read-only view of the
world. The traversal/aggregation logic is pure and engine-independent: it
reaches the world only through the `WorldReader` interface, so it is unit-tested
with an in-memory fake and contains no engine imports. The live `WorldReader`
that wraps `internal/rooms` lives in a separate, checkout-only package (added in
milestone M1b).

## Key Components
### Core Files
- **reader.go**: the `WorldReader` interface plus the `RoomView` / `ExitView`
  value types it returns.
- **build.go**: `Build`, `Options`, `DefaultOptions`, and the unexported
  helpers `includedZones`, `indexRoomZones`, `buildNodes`, `buildEdges`,
  `countComponents`, `canonicalPair`, `isExcluded`.

### WorldReader interface
```go
type WorldReader interface {
    ZoneNames() []string
    ZoneBiome(zone string) string
    RoomIDs(zone string) []int
    Room(id int) (RoomView, bool)
}
```

## Algorithm (Build)
1. **includedZones** — the zones to crawl, minus any matching an
   `ExcludeZonePatterns` glob (`path.Match`, e.g. `instance_*`).
2. **indexRoomZones** — `roomId -> zone`, so an exit (which carries only a
   destination room id) can be resolved to a zone.
3. **buildNodes** — per-zone metadata: biome, room count, and whether any room
   is outdoors.
4. **buildEdges** — undirected, weighted adjacency from every cross-zone exit.
   Secret exits are honored via `Options.IncludeSecretExits`; intra-zone exits
   and exits whose target resolves to no included zone are skipped. Edges are
   canonicalized (`A <= B`) and sorted for stable output.
5. **countComponents** — union-find over zones + edges; isolated zones each
   count as their own component.

## Options
- `IncludeSecretExits bool`, `ExcludeZonePatterns []string`,
  `BuiltAtRound uint64`. `DefaultOptions()` enables secret exits and excludes
  `instance_*` / `ephemeral_*`.

## Dependencies
- `github.com/GoMudEngine/GoMud/modules/weather/sim` (the `Graph` types).
- Standard library (`sort`, `path`). No engine imports.

## Testing
- `build_test.go` drives `Build` through `fake_reader_test.go`'s in-memory
  `fakeReader`: adjacency, weights, secret-exit option, zone exclusion, node
  metadata, components, and an end-to-end cache round-trip.

## Next (M1b)
A live `WorldReader` over `internal/rooms` (`GetAllZoneNames`,
`GetAllZoneRoomsIds`, `LoadRoom`, `GetZoneBiome`), plus the
`weather graph` / `weather rebuild` admin commands and cache persistence.
