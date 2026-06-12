package weather

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/crawler"
	"gopkg.in/yaml.v2"
)

func TestBuildConfig(t *testing.T) {
	vals := map[string]any{
		"Enabled":            true,
		"IncludeSecretExits": true,
		"RebuildGraphOnBoot": false,
	}
	c := buildConfig(func(k string) any { return vals[k] })

	if !c.Enabled {
		t.Error("Enabled should be true")
	}
	if !c.IncludeSecretExits {
		t.Error("IncludeSecretExits should be true")
	}
	if c.RebuildGraphOnBoot {
		t.Error("RebuildGraphOnBoot should be false")
	}
}

func TestBuildConfigDefaults(t *testing.T) {
	// A getter with no values: every knob falls back to its code default.
	// The shipped data overlay restates these values as ACTIVE keys (the
	// engine only registers keys it sees in an overlay — see the overlay
	// header and healConfigClobber); TestOverlayMatchesCodeDefaults pins the
	// two sides equal, and this test pins the code side key by key.
	cfg := buildConfig(func(string) any { return nil })
	if !cfg.Enabled {
		t.Error("Enabled must default TRUE when absent (OOBE: fresh install boots working)")
	}
	if cfg.TickEveryGameHours != 1 || cfg.MaxActiveFronts != 8 || cfg.SpawnRateScale != 1.0 {
		t.Errorf("sim defaults wrong: %+v", cfg)
	}
	if cfg.EmoteMode != "module" || cfg.EmoteEveryRounds != 20 {
		t.Errorf("emote defaults wrong: %+v", cfg)
	}
	if !cfg.BuffsEnabled || !cfg.Persist || cfg.Seed != 0 {
		t.Errorf("buff/persist/seed defaults wrong: %+v", cfg)
	}
	if !cfg.IncludeSecretExits {
		t.Error("IncludeSecretExits must default true")
	}
	if cfg.RebuildGraphOnBoot {
		t.Error("RebuildGraphOnBoot must default false")
	}
	if !cfg.SeasonsEnabled {
		t.Error("SeasonsEnabled must default true")
	}
	if cfg.PerRoomRefinement != RefineOccupied {
		t.Errorf("PerRoomRefinement must default occupied: %q", cfg.PerRoomRefinement)
	}
	if want := []string{"instance_*", "ephemeral_*"}; !reflect.DeepEqual(cfg.ExcludeZonePatterns, want) {
		t.Errorf("ExcludeZonePatterns default = %v, want %v", cfg.ExcludeZonePatterns, want)
	}
	if cfg.BuffOverrides != nil {
		t.Errorf("BuffOverrides must default nil: %v", cfg.BuffOverrides)
	}
}

func TestEnabledDefaultsTrueExplicitFalseWins(t *testing.T) {
	// Absent → true (covered above too, but this is the regression the
	// OverlayOverrides incident exposed, so pin it by name)...
	if cfg := buildConfig(func(string) any { return nil }); !cfg.Enabled {
		t.Error("absent Enabled must resolve true")
	}
	// ...while an explicit false still disables the module.
	off := buildConfig(func(k string) any {
		return map[string]any{"Enabled": false}[k]
	})
	if off.Enabled {
		t.Error("explicit Enabled: false must resolve false")
	}
}

func TestBuildConfigCoercionAndClamps(t *testing.T) {
	vals := map[string]any{
		"Enabled":            true,
		"Seed":               7,          // yaml int
		"TickEveryGameHours": 0,          // clamps to 1
		"MaxActiveFronts":    "12",       // string coercion
		"SpawnRateScale":     2.5,        // float
		"EmoteMode":          "TAG-ONLY", // case-insensitive
		"EmoteEveryRounds":   2,          // clamps to 5
		"BuffsEnabled":       false,
		"Persist":            false,
	}
	cfg := buildConfig(func(k string) any { return vals[k] })
	if cfg.Seed != 7 || cfg.TickEveryGameHours != 1 || cfg.MaxActiveFronts != 12 {
		t.Errorf("coercion wrong: %+v", cfg)
	}
	if cfg.SpawnRateScale != 2.5 || cfg.EmoteMode != "tag-only" || cfg.EmoteEveryRounds != 5 {
		t.Errorf("clamps wrong: %+v", cfg)
	}
	if cfg.BuffsEnabled || cfg.Persist {
		t.Errorf("bool overrides ignored: %+v", cfg)
	}
}

func TestBuildConfigBadEmoteModeFallsBack(t *testing.T) {
	cfg := buildConfig(func(k string) any {
		if k == "EmoteMode" {
			return "shouty"
		}
		return nil
	})
	if cfg.EmoteMode != "module" {
		t.Errorf("invalid EmoteMode should fall back to module: %q", cfg.EmoteMode)
	}
}

func TestPerRoomRefinementConfig(t *testing.T) {
	if cfg := buildConfig(func(string) any { return nil }); cfg.PerRoomRefinement != RefineOccupied {
		t.Errorf("PerRoomRefinement must default occupied: %q", cfg.PerRoomRefinement)
	}
	for _, mode := range []string{RefineOccupied, RefineAll, RefineOff} {
		cfg := buildConfig(func(k string) any {
			return map[string]any{"PerRoomRefinement": mode}[k]
		})
		if cfg.PerRoomRefinement != mode {
			t.Errorf("PerRoomRefinement %q not adopted: %q", mode, cfg.PerRoomRefinement)
		}
	}
	upper := buildConfig(func(k string) any {
		return map[string]any{"PerRoomRefinement": "ALL"}[k]
	})
	if upper.PerRoomRefinement != RefineAll {
		t.Errorf("PerRoomRefinement should be case-insensitive: %q", upper.PerRoomRefinement)
	}
}

func TestBuildConfigBadPerRoomRefinementFallsBack(t *testing.T) {
	cfg := buildConfig(func(k string) any {
		if k == "PerRoomRefinement" {
			return "everywhere"
		}
		return nil
	})
	if cfg.PerRoomRefinement != RefineOccupied {
		t.Errorf("invalid PerRoomRefinement should fall back to occupied: %q", cfg.PerRoomRefinement)
	}
}

func TestBuffOverridesParsing(t *testing.T) {
	vals := map[string]any{
		"BuffOverrides.storm":    "59002, 59003", // comma-separated, whitespace ok
		"BuffOverrides.blizzard": "",             // key PRESENT + empty = explicit strip
		"BuffOverrides.heatwave": 60001,          // yaml unquoted int leaf
		// rain (et al.) absent = no override, no map entry
	}
	cfg := buildConfig(func(k string) any { return vals[k] })
	want := map[string][]int{"storm": {59002, 59003}, "blizzard": {}, "heatwave": {60001}}
	if !reflect.DeepEqual(cfg.BuffOverrides, want) {
		t.Errorf("BuffOverrides = %v, want %v", cfg.BuffOverrides, want)
	}
	if _, ok := cfg.BuffOverrides["rain"]; ok {
		t.Error("absent key must produce no entry")
	}

	// No keys set at all: nil map (no overrides).
	if cfg := buildConfig(func(string) any { return nil }); cfg.BuffOverrides != nil {
		t.Errorf("BuffOverrides must default nil: %v", cfg.BuffOverrides)
	}
}

func TestBuffOverridesGarbageFailsSoft(t *testing.T) {
	origWarn := warnConfig
	defer func() { warnConfig = origWarn }()
	var warns []string
	warnConfig = func(msg string, _ ...any) { warns = append(warns, msg) }
	for k := range warnedConfigKeys { // tests share the warn-once memory
		delete(warnedConfigKeys, k)
	}
	t.Cleanup(func() {
		for k := range warnedConfigKeys {
			delete(warnedConfigKeys, k)
		}
	})

	vals := map[string]any{
		"BuffOverrides.storm": "59002, zap", // bad token skipped, good ids kept
		"BuffOverrides.snow":  "junk",       // no usable ids: override dropped
		"BuffOverrides.fog":   "-5",         // ids must be positive
	}
	get := func(k string) any { return vals[k] }
	cfg := buildConfig(get)
	want := map[string][]int{"storm": {59002}}
	if !reflect.DeepEqual(cfg.BuffOverrides, want) {
		t.Errorf("BuffOverrides = %v, want %v", cfg.BuffOverrides, want)
	}
	if len(warns) != 3 {
		t.Errorf("want 3 warns (one per bad key), got %d: %v", len(warns), warns)
	}
	// Re-reading config must not warn again (once per bad key).
	buildConfig(get)
	if len(warns) != 3 {
		t.Errorf("warn-once violated: %d warns after reload", len(warns))
	}
}

func TestExcludeZonePatternsConfig(t *testing.T) {
	// Absent and empty both keep the crawler's stock default — identical
	// behavior for existing installs (there is no "exclude nothing" value;
	// a never-matching token effectively disables exclusion).
	def := crawler.DefaultOptions().ExcludeZonePatterns
	for _, v := range []any{nil, "", "   ", " , "} {
		cfg := buildConfig(func(k string) any {
			return map[string]any{"ExcludeZonePatterns": v}[k]
		})
		if !reflect.DeepEqual(cfg.ExcludeZonePatterns, def) {
			t.Errorf("value %q: got %v, want crawler default %v", v, cfg.ExcludeZonePatterns, def)
		}
	}
	custom := buildConfig(func(k string) any {
		return map[string]any{"ExcludeZonePatterns": " inst_* , arena_*,zzz-none "}[k]
	})
	if want := []string{"inst_*", "arena_*", "zzz-none"}; !reflect.DeepEqual(custom.ExcludeZonePatterns, want) {
		t.Errorf("custom patterns = %v, want %v", custom.ExcludeZonePatterns, want)
	}
}

// TestApplyBuffConfigOrdering pins the spec §3 rule: overrides are applied
// BEFORE the BuffsEnabled strip, never the reverse, so BuffsEnabled=false
// still strips everything — overrides included. startSim runs the buff phase
// through applyBuffConfig; the seams record the call order.
func TestApplyBuffConfigOrdering(t *testing.T) {
	origOverrides, origStrip := applyBuffOverridesFn, stripBuffsFn
	defer func() { applyBuffOverridesFn, stripBuffsFn = origOverrides, origStrip }()

	var calls []string
	applyBuffOverridesFn = func(map[string][]int) int { calls = append(calls, "overrides"); return 1 }
	stripBuffsFn = func() int { calls = append(calls, "strip"); return 1 }

	m := &weatherModule{cfg: Config{
		BuffOverrides: map[string][]int{"storm": {59002}},
		BuffsEnabled:  false,
	}}
	m.applyBuffConfig()
	if want := []string{"overrides", "strip"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("buff phase order = %v, want %v", calls, want)
	}

	calls = nil
	m.cfg.BuffsEnabled = true
	m.applyBuffConfig()
	if want := []string{"overrides"}; !reflect.DeepEqual(calls, want) {
		t.Errorf("buffs enabled: calls = %v, want %v", calls, want)
	}

	calls = nil
	m.cfg.BuffOverrides = nil
	m.applyBuffConfig()
	if len(calls) != 0 {
		t.Errorf("no overrides + buffs enabled must touch nothing: %v", calls)
	}
}

func TestSimConfig(t *testing.T) {
	cfg := buildConfig(func(string) any { return nil })
	cfg.MaxActiveFronts = 3
	cfg.SpawnRateScale = 0
	sc := cfg.simConfig()
	if sc.MaxActiveFronts != 3 {
		t.Errorf("front budget not applied: %+v", sc)
	}
	if sc.SpawnChance != 0 {
		t.Errorf("scale 0 should zero the spawn chance: %v", sc.SpawnChance)
	}

	cfg.MaxActiveFronts = 0 // bypasses buildConfig's clamp; simConfig must still pass it through
	cfg.SpawnRateScale = 10
	sc = cfg.simConfig()
	if sc.SpawnChance != 1 {
		t.Errorf("spawn chance must cap at 1: %v", sc.SpawnChance)
	}
}

func TestBuildConfigClampsFrontBudgetAndSeed(t *testing.T) {
	cfg := buildConfig(func(k string) any {
		return map[string]any{"MaxActiveFronts": 0, "Seed": -1}[k]
	})
	if cfg.MaxActiveFronts != 1 {
		t.Errorf("MaxActiveFronts 0 must clamp to 1: %d", cfg.MaxActiveFronts)
	}
	if cfg.Seed != 0 {
		t.Errorf("negative Seed must clamp to 0 (derive-from-world): %d", cfg.Seed)
	}
}

// TestOverlayMatchesCodeDefaults pins the two halves of the engine-trap fix
// together (see healClobberedConfig's block comment): the shipped overlay's
// ACTIVE keys are what registers Modules.weather.* for configs.SetVal (the
// admin write path), and its values double as boot defaults that the engine
// may apply over a partial operator block — so the active key set must equal
// configKeyMeta's writable keys and every value must equal buildConfig's code
// default, or the overlay would silently rewrite operator worlds.
func TestOverlayMatchesCodeDefaults(t *testing.T) {
	raw, err := files.ReadFile("files/data-overlays/config.yaml")
	if err != nil {
		t.Fatalf("shipped overlay missing: %v", err)
	}
	var overlay map[string]any
	if err := yaml.Unmarshal(raw, &overlay); err != nil {
		t.Fatalf("shipped overlay unparsable: %v", err)
	}

	cfg := buildConfig(func(string) any { return nil })
	want := map[string]any{
		"Enabled":             cfg.Enabled,
		"IncludeSecretExits":  cfg.IncludeSecretExits,
		"RebuildGraphOnBoot":  cfg.RebuildGraphOnBoot,
		"Seed":                cfg.Seed,
		"TickEveryGameHours":  cfg.TickEveryGameHours,
		"MaxActiveFronts":     cfg.MaxActiveFronts,
		"SpawnRateScale":      cfg.SpawnRateScale,
		"EmoteMode":           cfg.EmoteMode,
		"EmoteEveryRounds":    cfg.EmoteEveryRounds,
		"BuffsEnabled":        cfg.BuffsEnabled,
		"Persist":             cfg.Persist,
		"SeasonsEnabled":      cfg.SeasonsEnabled,
		"PerRoomRefinement":   cfg.PerRoomRefinement,
		"ExcludeZonePatterns": strings.Join(cfg.ExcludeZonePatterns, ","),
	}

	// Key set: exactly the writable admin keys, no more, no less. (Per-type
	// BuffOverrides.* stay commented in the overlay by design — the type set
	// is open, so they cannot be pre-registered; the admin page shows them
	// read-only.)
	for _, k := range writableConfigKeys() {
		if _, ok := overlay[k]; !ok {
			t.Errorf("overlay missing active key %s (the engine would reject admin writes to it)", k)
		}
		if _, ok := want[k]; !ok {
			t.Errorf("test value table missing key %s", k)
		}
	}
	for k := range overlay {
		if !registeredConfigKey(k) {
			t.Errorf("overlay ships unexpected active key %s (not a writable admin key)", k)
		}
	}

	// Values: semantically equal to the code defaults (the overlay carries
	// yaml types — float 1.0, int 0 — compared via configValuesEqual like the
	// heal does).
	for k, w := range want {
		if got, ok := overlay[k]; ok && !configValuesEqual(got, w) {
			t.Errorf("overlay %s = %v, code default = %v — keep them identical", k, got, w)
		}
	}
}

// healFixture stubs the heal seams: warn/info capture plus a fake engine
// config layer whose set() simulates configs.SetVal's union re-apply (one
// successful write restores EVERY file value into the live config — the exact
// mechanism the heal rides; see healClobberedConfig).
type healFixture struct {
	live   map[string]any // what get() reads (the live config's weather block)
	union  map[string]any // flat file values SetVal would re-apply on success
	sets   []string       // recorded writes, "key=value"
	reject bool           // engine rejects the write (e.g. unregistered key)
	warns  []string
	infos  []string
}

func newHealFixture(t *testing.T) *healFixture {
	t.Helper()
	f := &healFixture{live: map[string]any{}, union: map[string]any{}}
	origWarn, origInfo := warnConfig, infoConfig
	warnConfig = func(msg string, _ ...any) { f.warns = append(f.warns, msg) }
	infoConfig = func(msg string, _ ...any) { f.infos = append(f.infos, msg) }
	t.Cleanup(func() { warnConfig, infoConfig = origWarn, origInfo })
	return f
}

func (f *healFixture) get(k string) any { return f.live[k] }

func (f *healFixture) set(k, v string) {
	f.sets = append(f.sets, k+"="+v)
	if f.reject {
		return
	}
	for fk, fv := range f.union {
		f.live[fk] = fv
	}
	f.live[k] = v // Sprint-equal to the union value for k when both exist
}

func readerOf(doc string) func() ([]byte, error) {
	return func() ([]byte, error) { return []byte(doc), nil }
}

func TestHealNoOverridesFile(t *testing.T) {
	f := newHealFixture(t)
	// Pristine world: the overrides file does not exist. Not an error.
	healClobberedConfig(func() ([]byte, error) { return nil, os.ErrNotExist }, f.get, f.set)
	if len(f.sets) != 0 || len(f.warns) != 0 || len(f.infos) != 0 {
		t.Errorf("missing file must be silent: sets=%v warns=%v infos=%v", f.sets, f.warns, f.infos)
	}
}

func TestHealReadErrorWarnsOnce(t *testing.T) {
	f := newHealFixture(t)
	healClobberedConfig(func() ([]byte, error) { return nil, os.ErrPermission }, f.get, f.set)
	if len(f.sets) != 0 || len(f.infos) != 0 {
		t.Errorf("read error must not write: sets=%v infos=%v", f.sets, f.infos)
	}
	if len(f.warns) != 1 {
		t.Errorf("read error must warn once: %v", f.warns)
	}
}

func TestHealParseGarbageWarnsAndReturns(t *testing.T) {
	f := newHealFixture(t)
	healClobberedConfig(readerOf("Modules: [unclosed\n\t:"), f.get, f.set)
	if len(f.sets) != 0 || len(f.infos) != 0 {
		t.Errorf("garbage must not write: sets=%v infos=%v", f.sets, f.infos)
	}
	if len(f.warns) != 1 {
		t.Errorf("garbage must warn once: %v", f.warns)
	}
}

func TestHealNoWeatherBlock(t *testing.T) {
	f := newHealFixture(t)
	healClobberedConfig(readerOf("Modules:\n  other:\n    X: 1\nServer:\n  Seed: x\n"), f.get, f.set)
	if len(f.sets) != 0 || len(f.warns) != 0 || len(f.infos) != 0 {
		t.Errorf("no weather block must be silent: sets=%v warns=%v infos=%v", f.sets, f.warns, f.infos)
	}
}

func TestHealSteadyStateFullMatch(t *testing.T) {
	// Normal post-heal boots: full file block, every value live. Silent.
	f := newHealFixture(t)
	f.live = map[string]any{
		"Enabled": true, "TickEveryGameHours": 2, "SpawnRateScale": 1, // engine re-read 1.0 as int 1
		"SeasonsEnabled": false, "EmoteMode": "module",
	}
	doc := `
Modules:
  weather:
    Enabled: true
    TickEveryGameHours: 2
    SpawnRateScale: 1.0
    SeasonsEnabled: false
    EmoteMode: module
`
	healClobberedConfig(readerOf(doc), f.get, f.set)
	if len(f.sets) != 0 || len(f.warns) != 0 || len(f.infos) != 0 {
		t.Errorf("steady state must be silent: sets=%v warns=%v infos=%v", f.sets, f.warns, f.infos)
	}
}

func TestHealPartialClobber(t *testing.T) {
	// The upgrade-boot wipe (smoke scenario A): the operator's partial block
	// vanished from the live config (gets return nil); the live config holds
	// only overlay defaults for OTHER keys. One registered write — the first
	// mismatched registered key in sorted order — restores everything.
	f := newHealFixture(t)
	f.live = map[string]any{"MaxActiveFronts": 8, "Persist": true} // overlay-supplied survivors
	f.union = map[string]any{"Enabled": true, "TickEveryGameHours": 2, "SeasonsEnabled": false}
	doc := `
Modules:
  weather:
    Enabled: true
    TickEveryGameHours: 2
    SeasonsEnabled: false
`
	healClobberedConfig(readerOf(doc), f.get, f.set)
	if want := []string{"Enabled=true"}; !reflect.DeepEqual(f.sets, want) {
		t.Errorf("heal writes = %v, want %v (ONE write of the first mismatched registered key)", f.sets, want)
	}
	if len(f.infos) != 1 {
		t.Fatalf("heal must log success once: infos=%v warns=%v", f.infos, f.warns)
	}
	if f.live["TickEveryGameHours"] != 2 || f.live["SeasonsEnabled"] != false {
		t.Errorf("operator values not restored: %v", f.live)
	}
	if len(f.warns) != 0 {
		t.Errorf("successful heal must not warn: %v", f.warns)
	}
}

func TestHealNestedBuffOverridesClobber(t *testing.T) {
	// Nested map in the file block flattens to the engine's dotted key shape;
	// the unregistered BuffOverrides.storm mismatch rides along on the
	// registered TickEveryGameHours write (sorted: "B..." < "T...", but the
	// heal key must be the first REGISTERED mismatch).
	f := newHealFixture(t)
	f.union = map[string]any{"TickEveryGameHours": 4, "BuffOverrides.storm": "59002"}
	doc := `
Modules:
  weather:
    TickEveryGameHours: 4
    BuffOverrides:
      storm: "59002"
`
	healClobberedConfig(readerOf(doc), f.get, f.set)
	if want := []string{"TickEveryGameHours=4"}; !reflect.DeepEqual(f.sets, want) {
		t.Errorf("heal writes = %v, want %v", f.sets, want)
	}
	if f.live["BuffOverrides.storm"] != "59002" {
		t.Errorf("nested override not restored: %v", f.live)
	}
	if len(f.infos) != 1 || len(f.warns) != 0 {
		t.Errorf("infos=%v warns=%v", f.infos, f.warns)
	}
}

func TestHealUnregisteredOnlyMismatch(t *testing.T) {
	// Only an unregistered key mismatches: the heal must still fire — through
	// a registered key written back with its CURRENT value (live, since the
	// file lacks it); the value is a no-op, the union re-apply is the point.
	f := newHealFixture(t)
	for _, k := range writableConfigKeys() {
		f.live[k] = "x" // all registered keys present and matching nothing in the file
	}
	f.live["BuffsEnabled"] = true
	f.union = map[string]any{"BuffOverrides.storm": "59002"}
	doc := `
Modules:
  weather:
    BuffOverrides.storm: "59002"
`
	healClobberedConfig(readerOf(doc), f.get, f.set)
	// First sorted writable key is BuffsEnabled; file lacks it -> live value.
	if want := []string{"BuffsEnabled=true"}; !reflect.DeepEqual(f.sets, want) {
		t.Errorf("heal writes = %v, want %v", f.sets, want)
	}
	if f.live["BuffOverrides.storm"] != "59002" {
		t.Errorf("override not restored: %v", f.live)
	}
	if len(f.infos) != 1 || len(f.warns) != 0 {
		t.Errorf("infos=%v warns=%v", f.infos, f.warns)
	}
}

func TestHealWriteRejectedWarns(t *testing.T) {
	// Engine too old/new: the Set silently does nothing (PluginConfig.Set
	// discards errors). Re-verification catches it and warns; never blocks.
	f := newHealFixture(t)
	f.reject = true
	doc := "Modules:\n  weather:\n    TickEveryGameHours: 2\n"
	healClobberedConfig(readerOf(doc), f.get, f.set)
	if len(f.sets) != 1 {
		t.Errorf("heal must still attempt the write: %v", f.sets)
	}
	if len(f.warns) != 1 || len(f.infos) != 0 {
		t.Errorf("rejected write must warn, not celebrate: warns=%v infos=%v", f.warns, f.infos)
	}
}

func TestHealCaseInsensitiveBlockLookup(t *testing.T) {
	// Hand-edited files may case Modules/weather differently; leaf keys keep
	// their own spelling (compared against the live entry they produced).
	f := newHealFixture(t)
	f.union = map[string]any{"TickEveryGameHours": 3}
	doc := "modules:\n  WEATHER:\n    TickEveryGameHours: 3\n"
	healClobberedConfig(readerOf(doc), f.get, f.set)
	if want := []string{"TickEveryGameHours=3"}; !reflect.DeepEqual(f.sets, want) {
		t.Errorf("case-variant block not found: sets=%v", f.sets)
	}
	if len(f.infos) != 1 {
		t.Errorf("infos=%v warns=%v", f.infos, f.warns)
	}
}

func TestSeasonsEnabledConfig(t *testing.T) {
	if cfg := buildConfig(func(string) any { return nil }); !cfg.SeasonsEnabled {
		t.Error("SeasonsEnabled must default true")
	}
	off := buildConfig(func(k string) any {
		return map[string]any{"SeasonsEnabled": false}[k]
	})
	if off.SeasonsEnabled {
		t.Error("SeasonsEnabled override ignored")
	}
}
