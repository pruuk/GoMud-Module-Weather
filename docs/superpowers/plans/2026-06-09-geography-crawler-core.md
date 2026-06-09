# Geography Crawler Core — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the pure, engine-independent core of the geography crawler — the zone-adjacency `Graph` type (with JSON cache format) and the `Build` algorithm that turns a read-only world view into that graph — fully unit-tested with no GoMud checkout required.

**Architecture:** Two pure Go packages with **zero** engine imports: `sim/` holds the `Graph` data type and its serialization; `crawler/` defines a minimal `WorldReader` interface and the `Build` function that walks rooms/exits to produce a `sim.Graph`. A fake `WorldReader` drives the tests. An architecture test enforces that `sim/` never imports the engine. The live engine-backed `WorldReader` and the plugin command come in the **next** plan (M1b), where they compile inside a GoMud/DOGMud checkout.

**Tech Stack:** Go 1.25 (stdlib only — `encoding/json`, `sort`, `path`, `go/parser`, `testing`). No third-party dependencies.

**Spec:** Implements §6 (Geography Crawler) of `docs/superpowers/specs/2026-06-08-weather-module-design.md`. The output `Graph` is what spec §7's simulation core consumes via `sim.WorldView`.

**Scope boundary (read this):** This plan delivers the **pure core only**. It explicitly does NOT include: the engine-backed `WorldReader` (imports `internal/rooms`), the `weather` plugin registration, the `weather rebuild` / `weather graph` admin commands, or on-disk cache persistence via `plugin.WriteBytes`. Those require a GoMud checkout to compile and are the subject of the follow-up plan **M1b**. The `Graph.ToJSON`/`FromJSON` methods built here ARE the cache format M1b will read/write.

---

## Module path & repo layout note

This repo's `go.mod` declares the module path **`github.com/GoMudEngine/GoMud/modules/weather`** — deliberately matching where the module lives inside a GoMud checkout (`modules/weather/`). This makes the pure packages' import paths (`github.com/GoMudEngine/GoMud/modules/weather/sim`, `.../crawler`) **identical** standalone and in-checkout, so the same files compile in both places with no import rewrites. Source lives at the repo root (root == the module directory). Standalone, only the pure packages exist, so `go test ./...` works here today; the engine-importing packages added in M1b won't compile standalone (by design) and M1b will document the in-checkout build path.

---

## File Structure

| File | Responsibility |
|---|---|
| `go.mod` | Module definition (`github.com/GoMudEngine/GoMud/modules/weather`, Go 1.25). |
| `sim/doc.go` | Package doc for `sim` (the pure simulation/data package). |
| `sim/graph.go` | `Graph`, `ZoneNode`, `Edge` types; `GraphVersion`; `ToJSON`/`FromJSON`; `Neighbors`. |
| `sim/graph_test.go` | JSON round-trip and `Neighbors` tests. |
| `sim/arch_test.go` | Architecture guardrail: `sim` must not import `internal/*`. |
| `crawler/doc.go` | Package doc for `crawler`. |
| `crawler/reader.go` | `WorldReader` interface + `RoomView` / `ExitView` value types. |
| `crawler/build.go` | `Build`, `Options`, `DefaultOptions`, and unexported helpers. |
| `crawler/build_test.go` | Behavior tests for `Build` (adjacency, weights, options, components, metadata). |
| `crawler/fake_reader_test.go` | In-memory `fakeReader` test helper + interface-satisfaction test. |

---

## Task 1: Scaffold the Go module and the architecture guardrail

**Files:**
- Create: `go.mod`
- Create: `sim/doc.go`
- Create: `crawler/doc.go`
- Create: `sim/arch_test.go`

- [ ] **Step 1: Create `go.mod`**

```
module github.com/GoMudEngine/GoMud/modules/weather

go 1.25.0
```

- [ ] **Step 2: Create `sim/doc.go`**

```go
// Package sim holds the pure, engine-independent core of the weather module:
// the geography Graph that the crawler produces and the weather simulation
// consumes. Nothing in this package may import the GoMud engine
// (internal/*); that rule is enforced by arch_test.go.
package sim
```

- [ ] **Step 3: Create `crawler/doc.go`**

```go
// Package crawler builds a zone-adjacency Graph from a read-only view of the
// world (the WorldReader interface). The traversal logic here is pure and
// engine-independent; the live WorldReader implementation that wraps
// internal/rooms lives in a separate package built only inside a GoMud checkout.
package crawler
```

- [ ] **Step 4: Write the architecture guardrail test in `sim/arch_test.go`**

```go
package sim

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestSimPackageStaysPure enforces the design spec's guardrail: the sim
// package must never import the GoMud engine. If this fails, move the
// engine-touching code into the engine/ adapter package instead.
func TestSimPackageStaysPure(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, "GoMudEngine/GoMud/internal") {
				t.Errorf("%s imports forbidden engine package %q (sim must stay pure)", e.Name(), p)
			}
		}
	}
}
```

- [ ] **Step 5: Run the guardrail test to verify the module compiles and the test passes**

Run: `go test ./sim/...`
Expected: PASS (`ok  github.com/GoMudEngine/GoMud/modules/weather/sim`). The package currently has no forbidden imports, so the guardrail passes.

- [ ] **Step 6: Commit**

```bash
git add go.mod sim/doc.go crawler/doc.go sim/arch_test.go
git commit -m "feat(crawler): scaffold sim/crawler packages + sim purity guardrail"
```

---

## Task 2: `Graph` types and JSON cache format

**Files:**
- Create: `sim/graph.go`
- Test: `sim/graph_test.go`

- [ ] **Step 1: Write the failing round-trip test in `sim/graph_test.go`**

```go
package sim

import (
	"reflect"
	"testing"
)

func TestGraphJSONRoundTrip(t *testing.T) {
	g := &Graph{
		Version:      GraphVersion,
		BuiltAtRound: 12345,
		Nodes: map[string]ZoneNode{
			"Frostfang": {Zone: "Frostfang", Biome: "tundra", Rooms: 24, HasOutdoor: true},
			"Saltmarsh": {Zone: "Saltmarsh", Biome: "swamp", Rooms: 18, HasOutdoor: true},
		},
		Edges:      []Edge{{A: "Frostfang", B: "Saltmarsh", Weight: 3}},
		Components: 1,
	}

	b, err := g.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}

	got, err := FromJSON(b)
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}

	if !reflect.DeepEqual(g, got) {
		t.Errorf("round trip mismatch:\n want %+v\n got  %+v", g, got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./sim/ -run TestGraphJSONRoundTrip`
Expected: FAIL — compile error (`undefined: Graph`, `undefined: GraphVersion`, etc.).

- [ ] **Step 3: Implement `sim/graph.go`**

```go
package sim

import "encoding/json"

// GraphVersion is bumped whenever the on-disk cache format changes, so a
// loader can detect and rebuild a stale cache.
const GraphVersion = 1

// ZoneNode is one node in the geography graph: a single GoMud zone, plus the
// metadata the weather simulation needs about it.
type ZoneNode struct {
	Zone       string `json:"zone"`
	Biome      string `json:"biome"`
	Rooms      int    `json:"rooms"`
	HasOutdoor bool   `json:"hasOutdoor"`
}

// Edge is an undirected adjacency between two zones, weighted by the number of
// distinct room-exits that cross the border between them. A and B are stored in
// a canonical order (A <= B) so an edge has one representation.
type Edge struct {
	A      string `json:"a"`
	B      string `json:"b"`
	Weight int    `json:"weight"`
}

// Graph is the zone-adjacency graph: the crawler's output and the weather
// simulation's input. It is pure data and carries no engine types.
type Graph struct {
	Version      int                 `json:"version"`
	BuiltAtRound uint64              `json:"builtAtRound"`
	Nodes        map[string]ZoneNode `json:"nodes"`
	Edges        []Edge              `json:"edges"`
	Components   int                 `json:"components"`
}

// ToJSON serializes the graph to the on-disk cache format (indented for
// human inspection).
func (g *Graph) ToJSON() ([]byte, error) {
	return json.MarshalIndent(g, "", "  ")
}

// FromJSON parses a graph from its cache format.
func FromJSON(b []byte) (*Graph, error) {
	var g Graph
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, err
	}
	return &g, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./sim/ -run TestGraphJSONRoundTrip`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/graph.go sim/graph_test.go
git commit -m "feat(sim): add Graph types and JSON cache format"
```

---

## Task 3: `Graph.Neighbors` query helper

**Files:**
- Modify: `sim/graph.go`
- Test: `sim/graph_test.go`

- [ ] **Step 1: Add the failing test to `sim/graph_test.go`**

```go
func TestGraphNeighbors(t *testing.T) {
	g := &Graph{
		Edges: []Edge{
			{A: "A", B: "B", Weight: 2},
			{A: "B", B: "C", Weight: 1},
		},
	}

	got := map[string]int{}
	for _, e := range g.Neighbors("B") {
		if e.A != "B" {
			t.Errorf("neighbor edge should be oriented from the queried zone; got A=%q", e.A)
		}
		got[e.B] = e.Weight
	}

	want := map[string]int{"A": 2, "C": 1}
	if len(got) != len(want) {
		t.Fatalf("want %d neighbors, got %d (%v)", len(want), len(got), got)
	}
	for z, w := range want {
		if got[z] != w {
			t.Errorf("neighbor %q: want weight %d, got %d", z, w, got[z])
		}
	}

	if n := g.Neighbors("Z"); len(n) != 0 {
		t.Errorf("unknown zone should have no neighbors, got %v", n)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./sim/ -run TestGraphNeighbors`
Expected: FAIL — `g.Neighbors undefined`.

- [ ] **Step 3: Add `Neighbors` to `sim/graph.go`**

```go
// Neighbors returns the zones adjacent to z, each as an Edge oriented from z
// (Edge.A == z, Edge.B == the neighbor). Returns nil if z has no edges.
func (g *Graph) Neighbors(z string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		switch z {
		case e.A:
			out = append(out, Edge{A: e.A, B: e.B, Weight: e.Weight})
		case e.B:
			out = append(out, Edge{A: e.B, B: e.A, Weight: e.Weight})
		}
	}
	return out
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./sim/ -run TestGraphNeighbors`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/graph.go sim/graph_test.go
git commit -m "feat(sim): add Graph.Neighbors query helper"
```

---

## Task 4: `WorldReader` interface and the fake test reader

**Files:**
- Create: `crawler/reader.go`
- Create: `crawler/fake_reader_test.go`

- [ ] **Step 1: Create the interface and value types in `crawler/reader.go`**

```go
package crawler

// WorldReader is the minimal, engine-agnostic view of the world the crawler
// needs to build the geography graph. The live implementation (in a separate,
// checkout-only package) wraps internal/rooms; tests use an in-memory fake.
type WorldReader interface {
	// ZoneNames returns every zone in the world.
	ZoneNames() []string
	// ZoneBiome returns the default biome for a zone, or "" if unknown.
	ZoneBiome(zone string) string
	// RoomIDs returns the ids of the rooms belonging to a zone.
	RoomIDs(zone string) []int
	// Room returns a read-only snapshot of a room, or ok=false if it can't be
	// loaded (e.g. a dangling exit target).
	Room(id int) (RoomView, bool)
}

// RoomView is a read-only snapshot of the room facts the crawler uses.
type RoomView struct {
	ID      int
	Zone    string
	Outdoor bool
	Exits   []ExitView
}

// ExitView is a single exit from a room to a destination room id. Secret
// records whether the exit is hidden; the crawler decides whether to count it
// via Options.IncludeSecretExits.
type ExitView struct {
	ToRoom int
	Secret bool
}
```

- [ ] **Step 2: Create the fake reader and interface-satisfaction test in `crawler/fake_reader_test.go`**

```go
package crawler

import (
	"sort"
	"testing"
)

// fakeReader is an in-memory WorldReader for tests.
type fakeReader struct {
	biomes    map[string]string // zone -> biome
	rooms     map[int]RoomView  // id -> room
	zoneRooms map[string][]int  // zone -> room ids (insertion order)
}

func newFakeReader() *fakeReader {
	return &fakeReader{
		biomes:    map[string]string{},
		rooms:     map[int]RoomView{},
		zoneRooms: map[string][]int{},
	}
}

// addRoom registers a room in a zone with the given biome, outdoor flag, and
// exits. The first biome seen for a zone wins.
func (f *fakeReader) addRoom(zone, biome string, id int, outdoor bool, exits ...ExitView) {
	if _, ok := f.biomes[zone]; !ok {
		f.biomes[zone] = biome
	}
	f.rooms[id] = RoomView{ID: id, Zone: zone, Outdoor: outdoor, Exits: exits}
	f.zoneRooms[zone] = append(f.zoneRooms[zone], id)
}

func (f *fakeReader) ZoneNames() []string {
	out := make([]string, 0, len(f.biomes))
	for z := range f.biomes {
		out = append(out, z)
	}
	sort.Strings(out)
	return out
}

func (f *fakeReader) ZoneBiome(zone string) string { return f.biomes[zone] }
func (f *fakeReader) RoomIDs(zone string) []int    { return f.zoneRooms[zone] }
func (f *fakeReader) Room(id int) (RoomView, bool)  { r, ok := f.rooms[id]; return r, ok }

func TestFakeReaderSatisfiesInterface(t *testing.T) {
	var _ WorldReader = newFakeReader()
}
```

- [ ] **Step 3: Run the test to verify it compiles and passes**

Run: `go test ./crawler/ -run TestFakeReaderSatisfiesInterface`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add crawler/reader.go crawler/fake_reader_test.go
git commit -m "feat(crawler): add WorldReader interface and fake test reader"
```

---

## Task 5: `Build` — basic zone adjacency and edge weights

**Files:**
- Create: `crawler/build.go`
- Test: `crawler/build_test.go`

- [ ] **Step 1: Write the failing test in `crawler/build_test.go`**

```go
package crawler

import "testing"

func TestBuild_BasicAdjacency(t *testing.T) {
	f := newFakeReader()
	// Zone A: room 1 (1->2 internal), room 2 (2->1 internal, 2->3 crosses to B)
	f.addRoom("A", "plains", 1, true, ExitView{ToRoom: 2})
	f.addRoom("A", "plains", 2, true, ExitView{ToRoom: 1}, ExitView{ToRoom: 3})
	// Zone B: room 3 (3->2 crosses back to A)
	f.addRoom("B", "forest", 3, true, ExitView{ToRoom: 2})

	g, err := Build(f, Options{IncludeSecretExits: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(g.Nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 1 {
		t.Fatalf("want 1 edge, got %d: %+v", len(g.Edges), g.Edges)
	}
	e := g.Edges[0]
	if e.A != "A" || e.B != "B" {
		t.Errorf("want canonical edge A-B, got %s-%s", e.A, e.B)
	}
	// 2->3 (A->B) and 3->2 (B->A) are two distinct crossing exits.
	if e.Weight != 2 {
		t.Errorf("want weight 2, got %d", e.Weight)
	}
}

func TestBuild_UnknownExitTargetIgnored(t *testing.T) {
	f := newFakeReader()
	// Room 1 in A exits to room 99, which belongs to no known zone.
	f.addRoom("A", "plains", 1, true, ExitView{ToRoom: 99})

	g, err := Build(f, Options{IncludeSecretExits: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != 0 {
		t.Errorf("dangling exit target should produce no edge, got %+v", g.Edges)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./crawler/ -run TestBuild_`
Expected: FAIL — compile error (`undefined: Build`, `undefined: Options`).

- [ ] **Step 3: Implement `crawler/build.go`**

```go
package crawler

import (
	"sort"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// Options controls how the crawler interprets the world.
type Options struct {
	// IncludeSecretExits counts secret/hidden exits as adjacency.
	IncludeSecretExits bool
	// ExcludeZonePatterns lists glob patterns (path.Match syntax) for zones to
	// skip entirely, e.g. "instance_*".
	ExcludeZonePatterns []string
	// BuiltAtRound stamps the resulting graph with the round it was built on.
	BuiltAtRound uint64
}

// Build walks the world via r and produces a zone-adjacency Graph.
func Build(r WorldReader, opts Options) (*sim.Graph, error) {
	zones := includedZones(r, opts)
	roomZone := indexRoomZones(r, zones)
	nodes := buildNodes(r, zones)
	edges := buildEdges(r, zones, roomZone, opts)
	components := countComponents(zones, edges)

	return &sim.Graph{
		Version:      sim.GraphVersion,
		BuiltAtRound: opts.BuiltAtRound,
		Nodes:        nodes,
		Edges:        edges,
		Components:   components,
	}, nil
}

// includedZones returns the set of zones to crawl. (Exclusion patterns are
// added in a later task; for now every zone is included.)
func includedZones(r WorldReader, opts Options) map[string]bool {
	out := map[string]bool{}
	for _, z := range r.ZoneNames() {
		out[z] = true
	}
	return out
}

// indexRoomZones maps every room id in the included zones to its zone, so an
// exit (which only carries a destination room id) can be resolved to a zone.
func indexRoomZones(r WorldReader, zones map[string]bool) map[int]string {
	idx := map[int]string{}
	for zone := range zones {
		for _, id := range r.RoomIDs(zone) {
			idx[id] = zone
		}
	}
	return idx
}

// buildNodes creates a ZoneNode per included zone. (Metadata is filled in a
// later task; for now only the zone name is set.)
func buildNodes(r WorldReader, zones map[string]bool) map[string]sim.ZoneNode {
	nodes := map[string]sim.ZoneNode{}
	for zone := range zones {
		nodes[zone] = sim.ZoneNode{Zone: zone}
	}
	return nodes
}

// buildEdges accumulates undirected, weighted adjacency from every cross-zone
// exit. Intra-zone exits and exits whose target resolves to no included zone
// are skipped.
func buildEdges(r WorldReader, zones map[string]bool, roomZone map[int]string, opts Options) []sim.Edge {
	weights := map[[2]string]int{}
	for zone := range zones {
		for _, id := range r.RoomIDs(zone) {
			room, ok := r.Room(id)
			if !ok {
				continue
			}
			for _, ex := range room.Exits {
				dstZone, known := roomZone[ex.ToRoom]
				if !known || dstZone == zone {
					continue
				}
				weights[canonicalPair(zone, dstZone)]++
			}
		}
	}

	edges := make([]sim.Edge, 0, len(weights))
	for k, w := range weights {
		edges = append(edges, sim.Edge{A: k[0], B: k[1], Weight: w})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].A != edges[j].A {
			return edges[i].A < edges[j].A
		}
		return edges[i].B < edges[j].B
	})
	return edges
}

// canonicalPair orders two zone names so an edge has a single representation.
func canonicalPair(a, b string) [2]string {
	if a <= b {
		return [2]string{a, b}
	}
	return [2]string{b, a}
}

// countComponents returns the number of connected components. (Implemented in
// a later task; stubbed to 0 for now.)
func countComponents(zones map[string]bool, edges []sim.Edge) int {
	return 0
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./crawler/ -run TestBuild_`
Expected: PASS (both `TestBuild_BasicAdjacency` and `TestBuild_UnknownExitTargetIgnored`).

- [ ] **Step 5: Commit**

```bash
git add crawler/build.go crawler/build_test.go
git commit -m "feat(crawler): Build basic zone adjacency with weighted edges"
```

---

## Task 6: `Build` — secret-exit option

**Files:**
- Modify: `crawler/build.go` (the `buildEdges` helper)
- Test: `crawler/build_test.go`

- [ ] **Step 1: Add the failing test to `crawler/build_test.go`**

```go
func TestBuild_SecretExitsOption(t *testing.T) {
	makeWorld := func() *fakeReader {
		f := newFakeReader()
		// One normal crossing exit A(1)->B(2) and one secret crossing exit
		// A(1)->B(3).
		f.addRoom("A", "plains", 1, true,
			ExitView{ToRoom: 2},
			ExitView{ToRoom: 3, Secret: true},
		)
		f.addRoom("B", "forest", 2, true)
		f.addRoom("B", "forest", 3, true)
		return f
	}

	// Included: both exits count -> weight 2.
	g, _ := Build(makeWorld(), Options{IncludeSecretExits: true})
	if len(g.Edges) != 1 || g.Edges[0].Weight != 2 {
		t.Fatalf("with secrets included want one edge weight 2, got %+v", g.Edges)
	}

	// Excluded: only the normal exit counts -> weight 1.
	g, _ = Build(makeWorld(), Options{IncludeSecretExits: false})
	if len(g.Edges) != 1 || g.Edges[0].Weight != 1 {
		t.Fatalf("with secrets excluded want one edge weight 1, got %+v", g.Edges)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./crawler/ -run TestBuild_SecretExitsOption`
Expected: FAIL — with `IncludeSecretExits: false` the weight is still 2 (secret exits are not yet filtered).

- [ ] **Step 3: Add the secret-exit guard inside `buildEdges` in `crawler/build.go`**

Add the guard at the top of the inner `for _, ex := range room.Exits` loop, so the loop body becomes:

```go
			for _, ex := range room.Exits {
				if ex.Secret && !opts.IncludeSecretExits {
					continue
				}
				dstZone, known := roomZone[ex.ToRoom]
				if !known || dstZone == zone {
					continue
				}
				weights[canonicalPair(zone, dstZone)]++
			}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./crawler/ -run TestBuild_`
Expected: PASS (all Build tests).

- [ ] **Step 5: Commit**

```bash
git add crawler/build.go crawler/build_test.go
git commit -m "feat(crawler): honor IncludeSecretExits option in edge building"
```

---

## Task 7: `Build` — zone exclusion patterns

**Files:**
- Modify: `crawler/build.go` (the `includedZones` helper + imports)
- Test: `crawler/build_test.go`

- [ ] **Step 1: Add the failing test to `crawler/build_test.go`**

```go
func TestBuild_ExcludeZonePatterns(t *testing.T) {
	f := newFakeReader()
	f.addRoom("Town", "city", 1, true, ExitView{ToRoom: 2})
	f.addRoom("instance_jail", "city", 2, false, ExitView{ToRoom: 1})

	g, err := Build(f, Options{
		IncludeSecretExits:  true,
		ExcludeZonePatterns: []string{"instance_*"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := g.Nodes["instance_jail"]; ok {
		t.Errorf("excluded zone should not appear as a node")
	}
	if len(g.Nodes) != 1 {
		t.Errorf("want 1 node after exclusion, got %d", len(g.Nodes))
	}
	if len(g.Edges) != 0 {
		t.Errorf("edge to an excluded zone should be dropped, got %+v", g.Edges)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./crawler/ -run TestBuild_ExcludeZonePatterns`
Expected: FAIL — `instance_jail` is still present (no exclusion logic yet).

- [ ] **Step 3: Implement exclusion in `crawler/build.go`**

Add `"path"` to the import block:

```go
import (
	"path"
	"sort"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)
```

Replace the `includedZones` helper with:

```go
// includedZones returns the set of zones to crawl, skipping any whose name
// matches an ExcludeZonePatterns glob (path.Match syntax).
func includedZones(r WorldReader, opts Options) map[string]bool {
	out := map[string]bool{}
	for _, z := range r.ZoneNames() {
		if isExcluded(z, opts.ExcludeZonePatterns) {
			continue
		}
		out[z] = true
	}
	return out
}

// isExcluded reports whether zone matches any of the glob patterns.
func isExcluded(zone string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, zone); ok {
			return true
		}
	}
	return false
}
```

Because `indexRoomZones`, `buildNodes`, and `buildEdges` all iterate the
`zones` set produced by `includedZones`, excluded zones are dropped from rooms,
nodes, and edges automatically — an exit pointing into an excluded zone
resolves to an unknown target and is skipped.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./crawler/ -run TestBuild_`
Expected: PASS (all Build tests).

- [ ] **Step 5: Commit**

```bash
git add crawler/build.go crawler/build_test.go
git commit -m "feat(crawler): support ExcludeZonePatterns zone filtering"
```

---

## Task 8: `Build` — node metadata (biome, room count, outdoor)

**Files:**
- Modify: `crawler/build.go` (the `buildNodes` helper)
- Test: `crawler/build_test.go`

- [ ] **Step 1: Add the failing test to `crawler/build_test.go`**

```go
func TestBuild_NodeMetadata(t *testing.T) {
	f := newFakeReader()
	// Zone "Cavern": 2 rooms, both indoor -> hasOutdoor false.
	f.addRoom("Cavern", "cave", 1, false)
	f.addRoom("Cavern", "cave", 2, false)
	// Zone "Glade": 1 indoor + 1 outdoor -> hasOutdoor true.
	f.addRoom("Glade", "forest", 3, false)
	f.addRoom("Glade", "forest", 4, true)

	g, err := Build(f, Options{IncludeSecretExits: true})
	if err != nil {
		t.Fatal(err)
	}

	cav := g.Nodes["Cavern"]
	if cav.Biome != "cave" || cav.Rooms != 2 || cav.HasOutdoor {
		t.Errorf("Cavern node wrong: %+v", cav)
	}
	gld := g.Nodes["Glade"]
	if gld.Biome != "forest" || gld.Rooms != 2 || !gld.HasOutdoor {
		t.Errorf("Glade node wrong: %+v", gld)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./crawler/ -run TestBuild_NodeMetadata`
Expected: FAIL — nodes currently carry only `Zone`; `Biome`/`Rooms`/`HasOutdoor` are zero values.

- [ ] **Step 3: Replace the `buildNodes` helper in `crawler/build.go`**

```go
// buildNodes creates a ZoneNode per included zone, populated with its biome,
// room count, and whether any of its rooms is outdoors.
func buildNodes(r WorldReader, zones map[string]bool) map[string]sim.ZoneNode {
	nodes := map[string]sim.ZoneNode{}
	for zone := range zones {
		ids := r.RoomIDs(zone)
		hasOutdoor := false
		for _, id := range ids {
			if room, ok := r.Room(id); ok && room.Outdoor {
				hasOutdoor = true
				break
			}
		}
		nodes[zone] = sim.ZoneNode{
			Zone:       zone,
			Biome:      r.ZoneBiome(zone),
			Rooms:      len(ids),
			HasOutdoor: hasOutdoor,
		}
	}
	return nodes
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./crawler/ -run TestBuild_`
Expected: PASS (all Build tests).

- [ ] **Step 5: Commit**

```bash
git add crawler/build.go crawler/build_test.go
git commit -m "feat(crawler): populate zone node metadata (biome, rooms, outdoor)"
```

---

## Task 9: `Build` — connected-component count

**Files:**
- Modify: `crawler/build.go` (the `countComponents` helper)
- Test: `crawler/build_test.go`

- [ ] **Step 1: Add the failing test to `crawler/build_test.go`**

```go
func TestBuild_Components(t *testing.T) {
	f := newFakeReader()
	// Component 1: A <-> B connected.
	f.addRoom("A", "plains", 1, true, ExitView{ToRoom: 2})
	f.addRoom("B", "forest", 2, true, ExitView{ToRoom: 1})
	// Component 2: C connected to D.
	f.addRoom("C", "swamp", 3, true, ExitView{ToRoom: 4})
	f.addRoom("D", "swamp", 4, true, ExitView{ToRoom: 3})
	// Component 3: Island, isolated (no exits).
	f.addRoom("Island", "ocean", 5, true)

	g, err := Build(f, Options{IncludeSecretExits: true})
	if err != nil {
		t.Fatal(err)
	}
	if g.Components != 3 {
		t.Errorf("want 3 components, got %d", g.Components)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./crawler/ -run TestBuild_Components`
Expected: FAIL — `countComponents` is stubbed to return 0.

- [ ] **Step 3: Replace the `countComponents` helper in `crawler/build.go`**

```go
// countComponents returns the number of connected components in the zone graph
// using union-find. Every included zone is its own component until edges merge
// them, so isolated zones each count once.
func countComponents(zones map[string]bool, edges []sim.Edge) int {
	parent := make(map[string]string, len(zones))
	for z := range zones {
		parent[z] = z
	}

	var find func(string) string
	find = func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]] // path halving
			x = parent[x]
		}
		return x
	}

	for _, e := range edges {
		ra, rb := find(e.A), find(e.B)
		if ra != rb {
			parent[ra] = rb
		}
	}

	roots := map[string]bool{}
	for z := range zones {
		roots[find(z)] = true
	}
	return len(roots)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./crawler/ -run TestBuild_`
Expected: PASS (all Build tests).

- [ ] **Step 5: Commit**

```bash
git add crawler/build.go crawler/build_test.go
git commit -m "feat(crawler): count connected components via union-find"
```

---

## Task 10: `DefaultOptions`, `BuiltAtRound`, and an end-to-end graph test

**Files:**
- Modify: `crawler/build.go` (add `DefaultOptions`)
- Test: `crawler/build_test.go`

- [ ] **Step 1: Add the failing test to `crawler/build_test.go`**

```go
func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()
	if !o.IncludeSecretExits {
		t.Error("DefaultOptions should include secret exits")
	}
	want := map[string]bool{"instance_*": true, "ephemeral_*": true}
	if len(o.ExcludeZonePatterns) != len(want) {
		t.Fatalf("want %d default exclude patterns, got %v", len(want), o.ExcludeZonePatterns)
	}
	for _, p := range o.ExcludeZonePatterns {
		if !want[p] {
			t.Errorf("unexpected default exclude pattern %q", p)
		}
	}
}

func TestBuild_EndToEnd(t *testing.T) {
	f := newFakeReader()
	f.addRoom("Frostfang", "tundra", 1, true, ExitView{ToRoom: 2})
	f.addRoom("Frostfang", "tundra", 2, true, ExitView{ToRoom: 1}, ExitView{ToRoom: 3})
	f.addRoom("Saltmarsh", "swamp", 3, true, ExitView{ToRoom: 2})

	opts := DefaultOptions()
	opts.BuiltAtRound = 777
	g, err := Build(f, opts)
	if err != nil {
		t.Fatal(err)
	}

	if g.Version != 1 {
		t.Errorf("want version 1, got %d", g.Version)
	}
	if g.BuiltAtRound != 777 {
		t.Errorf("want BuiltAtRound 777, got %d", g.BuiltAtRound)
	}
	if g.Components != 1 {
		t.Errorf("want 1 component, got %d", g.Components)
	}

	// The whole graph must survive the cache round-trip unchanged.
	b, err := g.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := sim.FromJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, got) {
		t.Errorf("graph changed across JSON round trip")
	}
}
```

- [ ] **Step 2: Add the imports the new test needs to `crawler/build_test.go`**

Change the test file's import block to:

```go
import (
	"reflect"
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./crawler/ -run 'TestDefaultOptions|TestBuild_EndToEnd'`
Expected: FAIL — `undefined: DefaultOptions`.

- [ ] **Step 4: Add `DefaultOptions` to `crawler/build.go`**

```go
// DefaultOptions returns the recommended crawler options: secret exits count
// toward adjacency, and transient instance/ephemeral zones are skipped.
func DefaultOptions() Options {
	return Options{
		IncludeSecretExits:  true,
		ExcludeZonePatterns: []string{"instance_*", "ephemeral_*"},
	}
}
```

- [ ] **Step 5: Run the full test suite to verify everything passes**

Run: `go test ./...`
Expected: PASS for both packages:
```
ok  github.com/GoMudEngine/GoMud/modules/weather/sim
ok  github.com/GoMudEngine/GoMud/modules/weather/crawler
```

- [ ] **Step 6: Commit**

```bash
git add crawler/build.go crawler/build_test.go
git commit -m "feat(crawler): add DefaultOptions and end-to-end build/cache test"
```

---

## Task 11: Reconcile docs with the chosen repo layout

**Files:**
- Modify: `README.md` (the "Planned layout" and "Development" sections)
- Modify: `docs/superpowers/specs/2026-06-08-weather-module-design.md` (§4.2 layout note + §6.5 acceptance status)

- [ ] **Step 1: Update the README "Planned layout" section**

Replace the fenced layout block under "## Planned layout" with the current reality — source at repo root, module path matching the in-checkout location:

```
sim/        # PURE simulation/data core — no engine imports (Graph, etc.)
crawler/    # geography crawler (zone adjacency) — pure, engine-agnostic Build
engine/     # (next chunk) the ONLY package importing internal/rooms,/mutators,/events
weather.go  # (next chunk) plugin registration + wiring
files/      # (next chunk) config overlay + climate/weather/mutator/buff/emote data
```

And replace the sentence above it ("The source of truth lives here under `module/weather/`; ...") with:

```
The module source lives at the repo root, with `go.mod` declaring the path
`github.com/GoMudEngine/GoMud/modules/weather` so the pure packages compile
identically here and inside a GoMud checkout's `modules/weather/`. The pure
packages (`sim/`, `crawler/`) are unit-tested standalone with `go test ./...`;
the engine-backed packages compile only inside a checkout.
```

- [ ] **Step 2: Update the README "Development" section**

Replace the body of "## Development" with:

```
The pure core (`sim/`, `crawler/`) is developed and tested standalone in this
repo: `go test ./...`. The engine-backed reader, plugin registration, and admin
commands (next milestone) compile only inside a GoMud checkout — develop those
by syncing the module source into a checkout's `modules/weather/` (without this
repo's `go.mod`, which must not travel), then `go generate ./... && go build`.
See [CONTRIBUTING.md](CONTRIBUTING.md) for the module/engine boundary.
```

- [ ] **Step 3: Add a layout note to spec §4.2**

In `docs/superpowers/specs/2026-06-08-weather-module-design.md`, immediately after the directory-layout code block in §4.2, add:

```
> **Repo realization (M1):** source lives at the repo root (root == the
> in-checkout `modules/weather/` dir); `go.mod` uses the path
> `github.com/GoMudEngine/GoMud/modules/weather` so pure-package import paths
> match standalone and in-checkout. The `go.mod` is a dev/test convenience and
> is not copied into a checkout (in-checkout modules have no `go.mod`).
```

- [ ] **Step 4: Mark the crawler acceptance criteria status in spec §6.5**

In §6.5, append a line noting partial completion:

```
> **Status (2026-06-09):** the pure core (graph types, cache round-trip, and
> the `Build` algorithm with adjacency/weights/options/components/metadata) is
> implemented and unit-tested standalone. Remaining for M1b: the live
> engine-backed `WorldReader`, the `weather graph`/`weather rebuild` commands,
> and on-disk cache persistence.
```

- [ ] **Step 5: Run the full suite once more and commit**

Run: `go test ./...`
Expected: PASS (both packages).

```bash
git add README.md docs/superpowers/specs/2026-06-08-weather-module-design.md
git commit -m "docs: reconcile repo layout with crawler core implementation"
```

---

## Self-Review Notes (author)

**Spec coverage (§6):** §6.2 algorithm steps 1–5 → Tasks 5/8 (zones, room index, exits→edges, metadata); §6.3 secret exits → Task 6; one-way exits treated undirected → `canonicalPair` (Task 5); disconnected components → Task 9; ephemeral/instanced exclusion → Task 7; §6.4 cache format → Task 2 (`ToJSON`/`FromJSON`); §6.5 acceptance (deterministic, cache round-trip, unit-tested via interface) → Tasks 2/10 + the fake reader. **Deferred to M1b (documented in scope + Task 11):** §6.3 cost/timing (boot crawl + cache write), and the `weather graph` spot-check command — both require the engine-backed reader/plugin.

**Type consistency:** `WorldReader`, `RoomView`, `ExitView` (Task 4) are used unchanged in `Build`/fake (Tasks 5–10). `sim.Graph`/`ZoneNode`/`Edge`/`GraphVersion`/`ToJSON`/`FromJSON`/`Neighbors` (Tasks 2–3) are referenced consistently by `crawler` (Tasks 5–10). Helper names (`includedZones`, `indexRoomZones`, `buildNodes`, `buildEdges`, `countComponents`, `canonicalPair`, `isExcluded`) are stable across the tasks that introduce and later replace them.

**Placeholders:** none — every step shows complete code or an exact command + expected output. The `countComponents`/`buildNodes`/`includedZones` "stub then replace" steps show the full helper body at each stage (genuine red-green), not a "TODO".
