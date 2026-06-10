package sim

import "testing"

func TestClimateForFallsBackToDefault(t *testing.T) {
	c := DefaultClimate()
	if _, ok := c["default"]; !ok {
		t.Fatal("DefaultClimate must include a 'default' profile")
	}
	// A biome with no profile returns the default profile.
	got := c.For("no-such-biome")
	if got.SpawnWeight != c["default"].SpawnWeight || len(got.Weather) != len(c["default"].Weather) {
		t.Error("For(unknown) should return the default profile")
	}
	// A known biome returns its own profile.
	if _, ok := c["tundra"]; ok {
		if _, hasSnow := c.For("tundra").Weather["snow"]; !hasSnow {
			t.Error("tundra profile should include snow")
		}
	}
}

func TestDefaultClimateTrackBindings(t *testing.T) {
	c := DefaultClimate()
	for _, biome := range []string{"plains", "forest", "mountain", "tundra", "swamp", "ocean"} {
		if c[biome].Track != "temperate" {
			t.Errorf("%s should bind to temperate, got %q", biome, c[biome].Track)
		}
	}
	for _, biome := range []string{"desert", "default"} {
		if c[biome].Track != "" {
			t.Errorf("%s should be unbound, got %q", biome, c[biome].Track)
		}
	}
}

func TestDefaultClimateCoversStockBiomes(t *testing.T) {
	c := DefaultClimate()
	// The stock GoMud world's outdoor biome ids (S1 smoke finding: these
	// previously fell through to the bland "default" profile).
	stock := []string{"mountains", "cliffs", "snow", "shore", "water",
		"farmland", "land", "road", "city", "fort", "slums"}
	for _, b := range stock {
		p, ok := c[b]
		if !ok {
			t.Errorf("stock biome %q has no climate profile", b)
			continue
		}
		if len(p.Weather) == 0 {
			t.Errorf("stock biome %q has empty weather table", b)
		}
		if p.Track != "temperate" {
			t.Errorf("stock biome %q should bind to temperate, got %q", b, p.Track)
		}
	}
	// Indoor-ish stock ids deliberately stay unbound/absent (default profile).
	for _, b := range []string{"cave", "dungeon", "house", "spiderweb"} {
		if _, ok := c[b]; ok {
			t.Errorf("indoor biome %q should not have a profile", b)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxActiveFronts <= 0 {
		t.Error("MaxActiveFronts must be positive")
	}
	if cfg.HistoryLen <= 0 {
		t.Error("HistoryLen must be positive")
	}
	if cfg.SpawnChance < 0 || cfg.SpawnChance > 1 {
		t.Error("SpawnChance must be in [0,1]")
	}
	if cfg.CoverageFalloff <= 0 || cfg.CoverageFalloff > 1 {
		t.Error("CoverageFalloff must be in (0,1]")
	}
	if cfg.MinProjected <= 0 || cfg.MinProjected > 1 {
		t.Error("MinProjected must be in (0,1]")
	}
	if cfg.MaxFrontRadius < 0 {
		t.Error("MaxFrontRadius must be >= 0")
	}
}
