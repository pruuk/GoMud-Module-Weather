package sim

import (
	"encoding/json"
	"sort"
)

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

// FromJSON parses a graph from its cache format. Callers should compare the
// returned Graph's Version against GraphVersion and rebuild the graph if they
// differ, since an older cache may use an incompatible layout.
func FromJSON(b []byte) (*Graph, error) {
	var g Graph
	if err := json.Unmarshal(b, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// Zones returns all zone names in the graph, sorted for deterministic iteration.
func (g *Graph) Zones() []string {
	out := make([]string, 0, len(g.Nodes))
	for z := range g.Nodes {
		out = append(out, z)
	}
	sort.Strings(out)
	return out
}

// Neighbors returns the zones adjacent to z, each as an Edge oriented from z
// (Edge.A == z, Edge.B == the neighbor). Returns nil if z has no edges.
func (g *Graph) Neighbors(z string) []Edge {
	var out []Edge
	for _, e := range g.Edges {
		switch z {
		case e.A:
			out = append(out, e)
		case e.B:
			out = append(out, Edge{A: e.B, B: e.A, Weight: e.Weight})
		}
	}
	return out
}
