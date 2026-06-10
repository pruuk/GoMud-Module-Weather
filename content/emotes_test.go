package content

import (
	"testing"
	"testing/fstest"
)

const stormYAML = `weather: storm
outdoor:
  default:
    - "Thunder cracks directly overhead."
    - "A blinding fork of lightning splits the sky."
  forest:
    - "Wind tears at the branches; the whole canopy roars."
indoor:
  default:
    - "Rain hammers against the windows."
seasonal:
  winter:
    outdoor:
      default:
        - "Sleet-laden thunder shakes loose flurries of ice."
    indoor:
      default:
        - "Frozen rain rattles the shutters like thrown gravel."
`

func loadTestTables(t *testing.T) Tables {
	t.Helper()
	fsys := fstest.MapFS{"emotes/storm.yaml": {Data: []byte(stormYAML)}}
	tables, err := LoadEmotes(fsys, "emotes")
	if err != nil {
		t.Fatal(err)
	}
	return tables
}

func TestPickSelectsByBiomeAndIndoor(t *testing.T) {
	tables := loadTestTables(t)
	first := func(n int) int { return 0 }

	if got := tables.Pick("storm", "forest", false, "", first); got != "Wind tears at the branches; the whole canopy roars." {
		t.Errorf("forest outdoor: %q", got)
	}
	if got := tables.Pick("storm", "desert", false, "", first); got != "Thunder cracks directly overhead." {
		t.Errorf("unknown biome should fall back to default: %q", got)
	}
	if got := tables.Pick("storm", "forest", true, "", first); got != "Rain hammers against the windows." {
		t.Errorf("indoor falls back to indoor default (never outdoor): %q", got)
	}
	if got := tables.Pick("fog", "forest", false, "", first); got != "" {
		t.Errorf("missing table must yield silence: %q", got)
	}
}

func TestPickUsesRoll(t *testing.T) {
	tables := loadTestTables(t)
	rolled := -1
	got := tables.Pick("storm", "default", false, "", func(n int) int { rolled = n; return 1 })
	if rolled != 2 {
		t.Errorf("roll should receive the line count, got %d", rolled)
	}
	if got != "A blinding fork of lightning splits the sky." {
		t.Errorf("roll result not honored: %q", got)
	}
}

func TestLoadEmotesRejectsMissingWeatherKey(t *testing.T) {
	fsys := fstest.MapFS{"emotes/bad.yaml": {Data: []byte("outdoor:\n  default: [\"x\"]\n")}}
	if _, err := LoadEmotes(fsys, "emotes"); err == nil {
		t.Fatal("emote table without 'weather' must be rejected")
	}
}

func TestLoadEmotesMissingDir(t *testing.T) {
	tables, err := LoadEmotes(fstest.MapFS{}, "emotes")
	if err != nil || len(tables) != 0 {
		t.Fatalf("missing dir should be empty tables, nil error: %v %v", tables, err)
	}
}

func TestPickClampsOutOfRangeRoll(t *testing.T) {
	tables := loadTestTables(t)
	if got := tables.Pick("storm", "default", false, "", func(n int) int { return n }); got != "Thunder cracks directly overhead." {
		t.Errorf("out-of-range roll should clamp to first line: %q", got)
	}
	if got := tables.Pick("storm", "default", false, "", func(n int) int { return -3 }); got != "Thunder cracks directly overhead." {
		t.Errorf("negative roll should clamp to first line: %q", got)
	}
}

const winterAmbienceYAML = `track: temperate
season: winter
outdoor:
  default:
    - "A skin of ice creaks at the edges of still water."
  forest:
    - "Snow slides from a burdened bough with a soft thump."
indoor:
  default:
    - "Cold radiates from the walls despite the shelter."
`

func TestLoadSeasonalEmotes(t *testing.T) {
	fsys := fstest.MapFS{"emotes/seasons/temperate_winter.yaml": {Data: []byte(winterAmbienceYAML)}}
	st, err := LoadSeasonalEmotes(fsys, "emotes/seasons")
	if err != nil {
		t.Fatal(err)
	}
	first := func(n int) int { return 0 }
	if got := st.Pick("temperate", "winter", "forest", false, first); got != "Snow slides from a burdened bough with a soft thump." {
		t.Errorf("forest outdoor: %q", got)
	}
	if got := st.Pick("temperate", "winter", "desert", false, first); got != "A skin of ice creaks at the edges of still water." {
		t.Errorf("biome fallback: %q", got)
	}
	if got := st.Pick("temperate", "winter", "default", true, first); got != "Cold radiates from the walls despite the shelter." {
		t.Errorf("indoor: %q", got)
	}
	if got := st.Pick("temperate", "summer", "default", false, first); got != "" {
		t.Errorf("missing season must be silent: %q", got)
	}
	if got := st.Pick("monsoon", "winter", "default", false, first); got != "" {
		t.Errorf("missing track must be silent: %q", got)
	}
}

func TestLoadSeasonalEmotesValidation(t *testing.T) {
	bad := fstest.MapFS{"emotes/seasons/x.yaml": {Data: []byte("season: winter\noutdoor:\n  default: [\"x\"]\n")}}
	if _, err := LoadSeasonalEmotes(bad, "emotes/seasons"); err == nil {
		t.Fatal("missing track must be rejected")
	}
	empty, err := LoadSeasonalEmotes(fstest.MapFS{}, "emotes/seasons")
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing dir: want empty/nil, got %v %v", empty, err)
	}
}

func TestPickSeasonalVariant(t *testing.T) {
	tables := loadTestTables(t)
	first := func(n int) int { return 0 }

	// Season with a variant: seasonal section wins.
	if got := tables.Pick("storm", "default", false, "winter", first); got != "Sleet-laden thunder shakes loose flurries of ice." {
		t.Errorf("winter outdoor variant: %q", got)
	}
	if got := tables.Pick("storm", "default", true, "winter", first); got != "Frozen rain rattles the shutters like thrown gravel." {
		t.Errorf("winter indoor variant: %q", got)
	}
	// Variant section misses the biome -> variant default (not base biome).
	if got := tables.Pick("storm", "forest", false, "winter", first); got != "Sleet-laden thunder shakes loose flurries of ice." {
		t.Errorf("variant default should win over base biome: %q", got)
	}
	// Season without a variant: base lines.
	if got := tables.Pick("storm", "forest", false, "summer", first); got != "Wind tears at the branches; the whole canopy roars." {
		t.Errorf("no-variant season falls to base: %q", got)
	}
	// No season (seasons off / unbound zone): base lines.
	if got := tables.Pick("storm", "default", false, "", first); got != "Thunder cracks directly overhead." {
		t.Errorf("empty season falls to base: %q", got)
	}
	// Variant exists but its sections are empty for indoor+biome: falls to base.
	if got := tables.Pick("storm", "default", true, "summer", first); got != "Rain hammers against the windows." {
		t.Errorf("missing variant indoor falls to base indoor: %q", got)
	}
}
