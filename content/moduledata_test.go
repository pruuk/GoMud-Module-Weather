package content

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
	"gopkg.in/yaml.v2"
)

// fileNameFor mirrors util.ConvertForFilename exactly (byte-level, not rune-level).
// Buff names must be ASCII; non-ASCII would produce different byte counts here vs.
// in the engine and cause the loader to reject the file at startup.
func fileNameFor(id string) string {
	s := []byte(strings.ToLower(id))
	pos := 0
	for _, b := range s {
		if b == '\'' {
			continue
		} else if ('a' <= b && b <= 'z') || ('0' <= b && b <= '9') {
			s[pos] = b
		} else {
			s[pos] = '_'
		}
		pos++
	}
	return string(s[:pos]) + ".yaml"
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

// TestShippedBuffSpecs validates the bespoke weather buffs the engine's
// plugin buff loader (internal/buffs/plugin.go) will merge at startup:
// parseable YAML, ids in the module's documented 59001-59099 range, no
// duplicates, and filenames exactly matching the engine's computed name
// `<BuffId>-<ConvertForFilename(Name)>.yaml` (buffspec.go Filename(); the
// loader rejects any file whose path doesn't end in that suffix). The
// conversion rule is replicated by fileNameFor above (engine
// internal/util/util.go ConvertForFilename). The buffs must stay gentle
// nuisances (small statmods, no scripts), and every playerbuffid referenced
// by a shipped mutator must resolve to a shipped buff.
func TestShippedBuffSpecs(t *testing.T) {
	dir := "../files/datafiles/buffs"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("buff specs missing: %v", err)
	}
	// Mirrors the engine's full BuffSpec YAML schema (buffspec.go) so the
	// strict unmarshal below only rejects genuinely unknown fields (typos).
	type buffSpec struct {
		BuffId        int            `yaml:"buffid"`
		Name          string         `yaml:"name"`
		Description   string         `yaml:"description"`
		Secret        bool           `yaml:"secret"`
		TriggerNow    bool           `yaml:"triggernow"`
		TriggerRate   string         `yaml:"triggerrate"`
		RoundInterval int            `yaml:"roundinterval"`
		TriggerCount  int            `yaml:"triggercount"`
		StatMods      map[string]int `yaml:"statmods"`
		Flags         []string       `yaml:"flags"`
	}
	shipped := make(map[int]string) // id -> filename
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var spec buffSpec
		if err := yaml.UnmarshalStrict(b, &spec); err != nil {
			t.Errorf("%s: bad YAML: %v", e.Name(), err)
			continue
		}
		if spec.BuffId < 59001 || spec.BuffId > 59099 {
			t.Errorf("%s: buffid %d outside the module's 59001-59099 range", e.Name(), spec.BuffId)
		}
		if prev, dup := shipped[spec.BuffId]; dup {
			t.Errorf("%s: duplicate buffid %d (also in %s)", e.Name(), spec.BuffId, prev)
		}
		shipped[spec.BuffId] = e.Name()
		// Engine filename rule: <id>-<ConvertForFilename(name)>.yaml.
		// fileNameFor already appends ".yaml" to the converted name.
		if want := strconv.Itoa(spec.BuffId) + "-" + fileNameFor(spec.Name); e.Name() != want {
			t.Errorf("%s: engine buff loader requires filename %q for id %d / name %q",
				e.Name(), want, spec.BuffId, spec.Name)
		}
		if spec.Description == "" {
			t.Errorf("%s: needs a player-visible description", e.Name())
		}
		// Engine BuffSpec.Validate rejects TriggerCount < 1 or an unparseable
		// TriggerRate (RoundInterval < 1).
		if spec.TriggerCount < 1 {
			t.Errorf("%s: triggercount must be >= 1", e.Name())
		}
		if spec.TriggerRate == "" {
			t.Errorf("%s: triggerrate must be set", e.Name())
		}
		// The mutator re-applies the buff every round while the weather holds;
		// a long triggercount would linger after the player leaves the weather.
		if spec.TriggerCount > 5 {
			t.Errorf("%s: triggercount %d too long; buff should fade shortly after leaving the weather", e.Name(), spec.TriggerCount)
		}
		// Gentleness guard: markedly softer than engine 31/33 (which hit -20
		// across five stats / dealt damage). Keep each statmod a small penalty.
		for stat, v := range spec.StatMods {
			// Weather buffs in this module are intentionally minor penalties, not bonuses.
			// A positive statmod would indicate a design change that needs explicit review;
			// if a positive-mod buff is ever added intentionally, update this bound.
			if v < -10 || v > 0 {
				t.Errorf("%s: statmod %s: %d — weather buffs must be gentle penalties in [-10, -1]", e.Name(), stat, v)
			}
		}
	}
	if len(shipped) < 3 {
		t.Fatalf("expected at least 3 shipped buff specs, found %d", len(shipped))
	}
	// Cross-check: every playerbuffid a shipped mutator applies must be one of
	// our shipped buffs (no more borrowing engine ids like 31/33, which vary
	// by world and are harsher than weather warrants).
	mutEntries, err := os.ReadDir("../files/datafiles/mutators")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range mutEntries {
		b, err := os.ReadFile("../files/datafiles/mutators/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var spec struct {
			PlayerBuffIds []int `yaml:"playerbuffids"`
		}
		if err := yaml.Unmarshal(b, &spec); err != nil {
			continue // mutator YAML validity is TestShippedMutatorSpecs' job
		}
		for _, id := range spec.PlayerBuffIds {
			if _, ok := shipped[id]; !ok {
				t.Errorf("%s: playerbuffid %d is not a shipped weather buff", e.Name(), id)
			}
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
	allIDs := make(map[string]bool)
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
		// Indoor variants are sheltered: no light penalty, no buffs, no alert spam.
		if strings.HasSuffix(id, "-indoor") {
			for _, forbidden := range []string{"lightmod", "playerbuffids", "mobbuffids", "alertmodifier"} {
				if _, has := spec[forbidden]; has {
					t.Errorf("%s: indoor variants must not set %s", e.Name(), forbidden)
				}
			}
		}
		allIDs[id] = true
	}
	if len(entries) < 22 { // 8 weather + 8 indoor + 6 season specs
		t.Errorf("expected at least 22 shipped mutator specs, found %d", len(entries))
	}
	// Pairing completeness: every outdoor weather-<type> must have a matching
	// weather-<type>-indoor so a future 9th weather type can't ship half-finished.
	for id := range allIDs {
		if strings.HasPrefix(id, "weather-") && !strings.HasSuffix(id, "-indoor") {
			indoorID := id + "-indoor"
			if !allIDs[indoorID] {
				t.Errorf("outdoor spec %q has no matching indoor variant %q", id, indoorID)
			}
		}
	}

	// Bidirectional type-list drift guard: the set of shipped outdoor
	// weather-<type> mutator ids must equal sim.KnownWeatherTypes minus "clear"
	// — both shipped-but-unlisted AND listed-but-unshipped are failures.
	// season-* and -indoor ids are excluded from the comparison.
	knownSet := make(map[string]bool, len(sim.KnownWeatherTypes))
	for _, wt := range sim.KnownWeatherTypes {
		if wt == sim.Clear {
			continue
		}
		knownSet["weather-"+string(wt)] = true
	}
	shippedOutdoor := make(map[string]bool)
	for id := range allIDs {
		if strings.HasPrefix(id, "weather-") && !strings.HasSuffix(id, "-indoor") {
			shippedOutdoor[id] = true
		}
	}
	for id := range shippedOutdoor {
		if !knownSet[id] {
			t.Errorf("shipped outdoor mutator %q is not listed in sim.KnownWeatherTypes", id)
		}
	}
	for id := range knownSet {
		if !shippedOutdoor[id] {
			t.Errorf("sim.KnownWeatherTypes entry %q has no shipped outdoor mutator spec", id)
		}
	}
}
