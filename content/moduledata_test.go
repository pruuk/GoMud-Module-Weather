package content

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// fileNameFor mirrors the engine's util.ConvertForFilename: lowercase,
// apostrophes dropped, any rune outside [a-z0-9] becomes '_'. The plugin
// mutator loader requires each file to be named fileNameFor(mutatorid)+".yaml".
func fileNameFor(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		switch {
		case r == '\'':
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String() + ".yaml"
}

// TestShippedEmoteTables validates the default emote tables: parseable, the
// weather key matches the filename stem, and every type has at least one
// outdoor-default and one indoor-default line.
func TestShippedEmoteTables(t *testing.T) {
	dir := "../files/datafiles/emotes"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("emote tables missing: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no emote tables shipped")
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		table, err := ParseEmoteTable(b)
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		if want := table.Weather + ".yaml"; e.Name() != want {
			t.Errorf("%s: filename should match weather key (%s)", e.Name(), want)
		}
		if len(table.Outdoor["default"]) == 0 {
			t.Errorf("%s: needs at least one outdoor default line", e.Name())
		}
		if len(table.Indoor["default"]) == 0 {
			t.Errorf("%s: needs at least one indoor default line", e.Name())
		}
		// Seasonal variant keys must be seasons of a shipped track, and every
		// variant needs at least one line somewhere.
		shippedSeasons := map[string]bool{"winter": true, "spring": true, "summer": true,
			"autumn": true, "wet": true, "dry": true}
		for season, sec := range table.Seasonal {
			if !shippedSeasons[season] {
				t.Errorf("%s: seasonal variant %q is not a season of any shipped track", e.Name(), season)
			}
			if len(sec.Outdoor) == 0 && len(sec.Indoor) == 0 {
				t.Errorf("%s: seasonal variant %q is empty", e.Name(), season)
			}
		}
	}
}

// TestShippedSeasonalAmbience validates the seasonal ambience tables: they
// load, cover exactly the shipped tracks' seasons, use <track>_<season>.yaml
// filenames, and each has outdoor+indoor default lines.
func TestShippedSeasonalAmbience(t *testing.T) {
	st, err := LoadSeasonalEmotes(os.DirFS("../files/datafiles"), "emotes/seasons")
	if err != nil {
		t.Fatalf("seasonal ambience failed to load: %v", err)
	}
	want := []SeasonalKey{
		{"temperate", "winter"}, {"temperate", "spring"},
		{"temperate", "summer"}, {"temperate", "autumn"},
		{"monsoon", "wet"}, {"monsoon", "dry"},
	}
	if len(st) != len(want) {
		t.Errorf("expected %d ambience tables, got %d", len(want), len(st))
	}
	for _, k := range want {
		sec, ok := st[k]
		if !ok {
			t.Errorf("missing ambience table for %v", k)
			continue
		}
		if len(sec.Outdoor["default"]) == 0 || len(sec.Indoor["default"]) == 0 {
			t.Errorf("%v: needs outdoor and indoor default lines", k)
		}
	}
	entries, _ := os.ReadDir("../files/datafiles/emotes/seasons")
	for _, e := range entries {
		// Filename rule (ours, not the engine's): <track>_<season>.yaml.
		var f struct{ Track, Season string }
		b, _ := os.ReadFile("../files/datafiles/emotes/seasons/" + e.Name())
		_ = yaml.Unmarshal(b, &f)
		if wantName := f.Track + "_" + f.Season + ".yaml"; e.Name() != wantName {
			t.Errorf("%s: filename should be %s", e.Name(), wantName)
		}
	}
}

// TestShippedMutatorSpecs validates the data files the engine will load:
// parseable YAML, weather- or season- namespaced ids, loader-compatible
// filenames, no respawnrate (it would fight the orchestrator and block
// purge-on-remove).
func TestShippedMutatorSpecs(t *testing.T) {
	dir := "../files/datafiles/mutators"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("mutator specs missing: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no mutator specs shipped")
	}
	for _, e := range entries {
		b, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var spec map[string]any
		if err := yaml.Unmarshal(b, &spec); err != nil {
			t.Errorf("%s: bad YAML: %v", e.Name(), err)
			continue
		}
		id, _ := spec["mutatorid"].(string)
		if !strings.HasPrefix(id, "weather-") && !strings.HasPrefix(id, "season-") {
			t.Errorf("%s: mutatorid %q must be weather- or season- namespaced", e.Name(), id)
		}
		if want := fileNameFor(id); e.Name() != want {
			t.Errorf("%s: engine loader requires filename %q for id %q", e.Name(), want, id)
		}
		if _, has := spec["respawnrate"]; has {
			t.Errorf("%s: weather mutators must not set respawnrate", e.Name())
		}
		if _, has := spec["decayintoid"]; has {
			t.Errorf("%s: decayintoid is forbidden — upstream MutatorList.Remove instantly resurrects the entry as the decay target (no liveness guard in Mutator.Update), corrupting orchestrator-driven transitions", e.Name())
		}
		if _, has := spec["decayrate"]; !has {
			t.Errorf("%s: weather mutators must set decayrate (self-heal safety net)", e.Name())
		}
	}
	if len(entries) < 14 { // 8 weather + 6 season specs
		t.Errorf("expected at least 14 shipped mutator specs, found %d", len(entries))
	}
}
