package crawler

import (
	"path"
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
				if ex.Secret && !opts.IncludeSecretExits {
					continue
				}
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
