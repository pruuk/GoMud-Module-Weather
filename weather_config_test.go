package weather

import (
	"reflect"
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/crawler"
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
	// A getter with no values: every knob falls back to its shipped default.
	cfg := buildConfig(func(string) any { return nil })
	if cfg.Enabled {
		t.Error("Enabled must default false when config is absent (overlay normally supplies true)")
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
