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
