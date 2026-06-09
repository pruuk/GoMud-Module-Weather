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
