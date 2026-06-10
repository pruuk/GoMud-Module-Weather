package sim

import "testing"

// twoZoneGraph: A(plains) <-> B(mountain), one edge weight 1.
func twoZoneGraph() *Graph {
	return &Graph{
		Nodes: map[string]ZoneNode{
			"A": {Zone: "A", Biome: "plains", Rooms: 3, HasOutdoor: true},
			"B": {Zone: "B", Biome: "mountain", Rooms: 3, HasOutdoor: true},
		},
		Edges: []Edge{{A: "A", B: "B", Weight: 1}},
	}
}

// These tests exercise the tick HELPERS directly (not the full Step), so they
// stay valid as later tasks add movement/spawning to Step.

func TestAgeAndFeedback_DrainsAndClamps(t *testing.T) {
	g := twoZoneGraph()
	// Front in mountain zone B (influence intensityDelta -0.15).
	fronts := []Front{{Id: 1, Type: "storm", Zone: "B", Intensity: 0.1, Moisture: 0.5, MaxAge: 24}}
	out := ageAndFeedback(fronts, g, DefaultClimate())
	if out[0].Intensity != 0 {
		t.Errorf("mountain feedback should drain 0.1 to 0 (clamped), got %v", out[0].Intensity)
	}
	if out[0].Age != 1 {
		t.Errorf("age should increment to 1, got %d", out[0].Age)
	}
}

func TestRemoveDead_DropsZeroIntensityAndAged(t *testing.T) {
	cfg := DefaultConfig()
	fronts := []Front{
		{Id: 1, Intensity: 0},                              // dead: no intensity
		{Id: 2, Intensity: 0.5, Age: cfg.FrontHardAge + 1}, // dead: too old
		{Id: 3, Intensity: 0.5, Age: 1},                    // alive
	}
	out := removeDead(fronts, cfg)
	if len(out) != 1 || out[0].Id != 3 {
		t.Errorf("only front 3 should survive, got %+v", out)
	}
}

func TestResolveWeather_AreaCoverageScalesWithIntensity(t *testing.T) {
	// Line A-B-C-D. Defaults: falloff 0.5, minProjected 0.15, maxRadius 2.
	g := &Graph{
		Nodes: map[string]ZoneNode{"A": {Zone: "A"}, "B": {Zone: "B"}, "C": {Zone: "C"}, "D": {Zone: "D"}},
		Edges: []Edge{{A: "A", B: "B", Weight: 1}, {A: "B", B: "C", Weight: 1}, {A: "C", B: "D", Weight: 1}},
	}
	cfg := DefaultConfig()

	// Strong storm at B (0.9): B=0.9, A&C=0.45, D=0.225 — all four covered (<= 2 hops).
	strong := resolveWeather(g, []Front{{Id: 1, Type: "storm", Zone: "B", Intensity: 0.9}}, cfg)
	for _, z := range []ZoneId{"A", "B", "C", "D"} {
		if strong[z] != "storm" {
			t.Errorf("strong storm should cover %s, got %q", z, strong[z])
		}
	}

	// Weak front at B (0.2): neighbors project 0.1 < 0.15 — only B covered.
	weak := resolveWeather(g, []Front{{Id: 1, Type: "fog", Zone: "B", Intensity: 0.2}}, cfg)
	if weak["B"] != "fog" {
		t.Errorf("weak front should hold B, got %q", weak["B"])
	}
	if weak["A"] != Clear || weak["C"] != Clear {
		t.Errorf("weak front (0.2) should NOT spread; A=%q C=%q", weak["A"], weak["C"])
	}
}

func TestResolveWeather_StrongerEffectiveIntensityWins(t *testing.T) {
	g := &Graph{Nodes: map[string]ZoneNode{"A": {Zone: "A"}, "B": {Zone: "B"}}, Edges: []Edge{{A: "A", B: "B", Weight: 1}}}
	cfg := DefaultConfig()
	// storm centered at A (0.9 -> projects 0.45 onto B); fog local at B (0.5).
	fronts := []Front{{Id: 1, Type: "storm", Zone: "A", Intensity: 0.9}, {Id: 2, Type: "fog", Zone: "B", Intensity: 0.5}}
	w := resolveWeather(g, fronts, cfg)
	if w["A"] != "storm" {
		t.Errorf("A should be storm (0.9), got %q", w["A"])
	}
	if w["B"] != "fog" {
		t.Errorf("B: local fog 0.5 should beat storm's projected 0.45, got %q", w["B"])
	}
}

func TestResolveWeather_FrontlessZonesAreClear(t *testing.T) {
	g := twoZoneGraph()
	w := resolveWeather(g, nil, DefaultConfig())
	if w["A"] != Clear || w["B"] != Clear {
		t.Errorf("no fronts -> all zones Clear, got %+v", w)
	}
}

func TestDiffWeather_ReportsOnlyChanges(t *testing.T) {
	prev := map[ZoneId]WeatherType{"A": Clear, "B": "rain"}
	next := map[ZoneId]WeatherType{"A": "storm", "B": "rain"}
	diff := diffWeather(prev, next)
	if len(diff.Changes) != 1 || diff.Changes[0].Zone != "A" ||
		diff.Changes[0].From != Clear || diff.Changes[0].To != "storm" {
		t.Errorf("expected one A: clear->storm change, got %+v", diff.Changes)
	}
}

func TestStep_DeterministicAndCoversAllZones(t *testing.T) {
	g := twoZoneGraph()
	cfg := DefaultConfig()
	st := State{RNGState: 7, NextID: 1, Weather: map[ZoneId]WeatherType{}}
	a, _ := Step(st, g, DefaultClimate(), cfg, Clock{Round: 1})
	b, _ := Step(st, g, DefaultClimate(), cfg, Clock{Round: 1})
	if a.RNGState != b.RNGState || len(a.Fronts) != len(b.Fronts) {
		t.Fatal("Step must be deterministic for identical inputs")
	}
	for _, z := range g.Zones() {
		if _, ok := a.Weather[z]; !ok {
			t.Errorf("zone %q missing from resolved weather", z)
		}
	}
}
