# sim Package Context

## Overview
`sim` is the pure, engine-independent core of the weather module. It holds the
geography `Graph` that the crawler produces and the weather simulation will
consume. Nothing here imports the GoMud engine (`internal/*`) — a rule enforced
by `arch_test.go` — so the simulation stays unit-testable without a running
server and portable across GoMud and DOGMud.

## Key Components
### Core Files
- **graph.go**: the `Graph`, `ZoneNode`, and `Edge` types; the `GraphVersion`
  constant; JSON cache serialization (`ToJSON` / `FromJSON`); and the
  `Neighbors` adjacency query.
- **arch_test.go**: architecture guardrail — fails if any file in this package
  imports a `GoMudEngine/GoMud/internal` path.

### Key Structures
```go
type ZoneNode struct {                 // one zone = one graph node
    Zone, Biome string
    Rooms       int
    HasOutdoor  bool
}
type Edge struct {                     // undirected, canonical (A <= B)
    A, B   string
    Weight int                         // # of room-exits crossing the border
}
type Graph struct {
    Version      int
    BuiltAtRound uint64
    Nodes        map[string]ZoneNode
    Edges        []Edge
    Components   int                   // connected-component count
}
```

## Core Functions
- `(*Graph) ToJSON() ([]byte, error)` / `FromJSON([]byte) (*Graph, error)` —
  the on-disk cache format (indented JSON). `GraphVersion` lets a loader detect
  a stale cache and rebuild.
- `(*Graph) Neighbors(zone string) []Edge` — adjacent zones, each Edge oriented
  from the queried zone (`Edge.A == zone`).

## Dependencies
- Standard library only (`encoding/json`). No engine imports (enforced).

## Consumers
- `crawler.Build` returns a `*Graph`.
- (Future) the weather simulation reads adjacency via `Neighbors`; the engine
  integration loads/saves the cache via `ToJSON`/`FromJSON`.

## Testing
- `graph_test.go`: JSON round-trip and `Neighbors`.
- `arch_test.go`: purity guardrail.
