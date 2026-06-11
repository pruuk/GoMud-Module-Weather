package weather

import (
	"strconv"
	"strings"

	"github.com/GoMudEngine/GoMud/internal/mudlog"
	"github.com/GoMudEngine/GoMud/internal/plugins"
	"github.com/GoMudEngine/GoMud/modules/weather/crawler"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// EmoteModeModule / EmoteModeTagOnly are the two §9.4 delivery modes.
const (
	EmoteModeModule  = "module"   // the module emits ambient lines itself
	EmoteModeTagOnly = "tag-only" // mutator tags only; the world's scripts react
)

// RefineOccupied / RefineAll / RefineOff are the §2.1 per-room refinement
// modes: room-scoped weather for occupied rooms only, for every room in every
// zone (force-loads rooms), or classic zone-scoped application.
const (
	RefineOccupied = "occupied" // refine rooms holding players (default)
	RefineAll      = "all"      // refine every room — force-loads by design
	RefineOff      = "off"      // zone-scoped weather mutators (v1 behavior)
)

// Numeric floors shared by buildConfig's sanity clamps (below) and the admin
// page's validators (configKeyMeta in weather_admin.go) — single source so the
// loader and the write-side validation can never drift apart.
const (
	minSeed               = 0 // 0 = "derive from the world"; negatives would wrap via uint64()
	minTickEveryGameHours = 1
	minMaxActiveFronts    = 1
	minEmoteEveryRounds   = 5
	minSpawnRateScale     = 0.0
)

// Config is the resolved module configuration (keys live under
// Modules.weather.* and default from files/data-overlays/config.yaml). Keys
// are flat (BuffsEnabled, not Buffs.Enabled) because plugin config lookup
// reads flattened scalar leaves.
type Config struct {
	Enabled            bool
	IncludeSecretExits bool
	RebuildGraphOnBoot bool

	Seed               uint64  // 0 = derive a stable seed from the world's zone names
	TickEveryGameHours int     // weather-simulation cadence in game hours (>= 1)
	MaxActiveFronts    int     // global front budget (>= 1)
	SpawnRateScale     float64 // multiplier on the default spawn chance
	EmoteMode          string  // EmoteModeModule | EmoteModeTagOnly
	EmoteEveryRounds   int     // ambient emote cadence in rounds (jittered ±25%, >= 5)
	BuffsEnabled       bool    // false strips buff ids from weather mutator specs
	Persist            bool    // save/restore fronts + RNG across reboots
	SeasonsEnabled     bool    // master switch for the seasons layer
	PerRoomRefinement  string  // RefineOccupied | RefineAll | RefineOff

	// BuffOverrides replaces the outdoor weather specs' PlayerBuffIds per type
	// (flat keys "BuffOverrides.<type>"): entry with ids = replacement, entry
	// with an empty list = explicit strip, no entry = shipped buffs. Applied
	// once at startSim, BEFORE the BuffsEnabled strip (disabled buffs win).
	BuffOverrides map[string][]int
	// ExcludeZonePatterns are the crawler's zone-skip globs (flat key, comma-
	// separated). Defaults to the crawler's stock instance_*/ephemeral_* pair;
	// there is no "exclude nothing" sentinel — to effectively disable
	// exclusion, set a single never-matching token (e.g. "zzz-no-exclusions").
	ExcludeZonePatterns []string
}

// getter abstracts plugin.Config.Get for testability.
type getter func(string) any

func asBool(v any) bool { b, _ := v.(bool); return b }

func boolOr(v any, def bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func intOr(v any, def int) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i
		}
	}
	return def
}

func floatOr(v any, def float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return f
		}
	}
	return def
}

func stringOr(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

// buildConfig resolves config from a getter, applying defaults and sanity
// clamps so a partial or hand-mangled overlay still yields a usable module.
func buildConfig(get getter) Config {
	seed := intOr(get("Seed"), 0)
	if seed < minSeed {
		seed = minSeed // negative would wrap via uint64(); treat as "derive from world"
	}

	c := Config{
		Enabled:            asBool(get("Enabled")),
		IncludeSecretExits: boolOr(get("IncludeSecretExits"), true),
		RebuildGraphOnBoot: asBool(get("RebuildGraphOnBoot")),

		Seed:               uint64(seed),
		TickEveryGameHours: intOr(get("TickEveryGameHours"), 1),
		MaxActiveFronts:    intOr(get("MaxActiveFronts"), 8),
		SpawnRateScale:     floatOr(get("SpawnRateScale"), 1.0),
		EmoteMode:          strings.ToLower(stringOr(get("EmoteMode"), EmoteModeModule)),
		EmoteEveryRounds:   intOr(get("EmoteEveryRounds"), 20),
		BuffsEnabled:       boolOr(get("BuffsEnabled"), true),
		Persist:            boolOr(get("Persist"), true),
		SeasonsEnabled:     boolOr(get("SeasonsEnabled"), true),
		PerRoomRefinement:  strings.ToLower(stringOr(get("PerRoomRefinement"), RefineOccupied)),
	}
	// Floors are the shared min* constants above — the admin validators
	// (configKeyMeta in weather_admin.go) reject below-floor writes outright.
	if c.TickEveryGameHours < minTickEveryGameHours {
		c.TickEveryGameHours = minTickEveryGameHours
	}
	if c.MaxActiveFronts < minMaxActiveFronts {
		c.MaxActiveFronts = minMaxActiveFronts
	}
	if c.EmoteEveryRounds < minEmoteEveryRounds {
		c.EmoteEveryRounds = minEmoteEveryRounds
	}
	if c.SpawnRateScale < minSpawnRateScale {
		c.SpawnRateScale = minSpawnRateScale
	}
	if c.EmoteMode != EmoteModeModule && c.EmoteMode != EmoteModeTagOnly {
		c.EmoteMode = EmoteModeModule
	}
	if c.PerRoomRefinement != RefineOccupied && c.PerRoomRefinement != RefineAll && c.PerRoomRefinement != RefineOff {
		c.PerRoomRefinement = RefineOccupied
	}
	c.BuffOverrides = buffOverrides(get)
	c.ExcludeZonePatterns = excludePatterns(get("ExcludeZonePatterns"))
	return c
}

// warnConfig / warnedConfigKeys: buildConfig warns at most once per bad key,
// through a seam because the engine logger is uninitialized under `go test`.
// Game loop only (loadConfig runs in onLoad and the config-changed listener).
var warnConfig = mudlog.Warn
var warnedConfigKeys = map[string]bool{}

func warnConfigOnce(key, msg string, args ...any) {
	if warnedConfigKeys[key] {
		return
	}
	warnedConfigKeys[key] = true
	warnConfig(msg, args...)
}

// buffOverrides reads "BuffOverrides.<type>" for every known weather type
// (sim.KnownWeatherTypes — "clear" included; it has no mutator, so an
// override for it warns at apply time). Returns nil when nothing is set.
func buffOverrides(get getter) map[string][]int {
	var out map[string][]int
	for _, t := range sim.KnownWeatherTypes {
		key := "BuffOverrides." + string(t)
		v := get(key)
		if v == nil { // key absent: no override
			continue
		}
		ids, ok := parseBuffIds(v, key)
		if !ok {
			continue
		}
		if out == nil {
			out = map[string][]int{}
		}
		out[string(t)] = ids
	}
	return out
}

// parseBuffIds parses one BuffOverrides value: comma-separated positive ints;
// an empty string means "explicit strip" (empty list, ok). Bad tokens are
// skipped with a warn (once per key); a value yielding NO valid ids at all is
// dropped entirely (ok=false) — fail-soft to the shipped buffs, like every
// other bad value in this module.
func parseBuffIds(v any, key string) ([]int, bool) {
	switch n := v.(type) {
	case int:
		if n > 0 {
			return []int{n}, true
		}
	case int64:
		if n > 0 {
			return []int{int(n)}, true
		}
	case float64:
		if i := int(n); i > 0 {
			return []int{i}, true
		}
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return []int{}, true // key present, value empty: explicit strip
		}
		var ids []int
		var bad []string
		for _, tok := range strings.Split(s, ",") {
			if tok = strings.TrimSpace(tok); tok == "" {
				continue
			}
			if id, err := strconv.Atoi(tok); err == nil && id > 0 {
				ids = append(ids, id)
			} else {
				bad = append(bad, tok)
			}
		}
		if len(bad) > 0 {
			warnConfigOnce(key, "Weather: ignoring bad buff ids in config",
				"key", key, "bad", strings.Join(bad, ","))
		}
		if len(ids) > 0 {
			return ids, true
		}
		return nil, false // nothing usable: keep the shipped buffs
	}
	warnConfigOnce(key, "Weather: unusable buff override value", "key", key)
	return nil, false
}

// excludePatterns parses the ExcludeZonePatterns key (comma-separated globs,
// whitespace-trimmed). Absent or empty keeps the crawler's stock default —
// identical behavior for existing installs.
func excludePatterns(v any) []string {
	raw := strings.TrimSpace(stringOr(v, ""))
	if raw == "" {
		return crawler.DefaultOptions().ExcludeZonePatterns
	}
	var pats []string
	for _, tok := range strings.Split(raw, ",") {
		if tok = strings.TrimSpace(tok); tok != "" {
			pats = append(pats, tok)
		}
	}
	if len(pats) == 0 {
		return crawler.DefaultOptions().ExcludeZonePatterns
	}
	return pats
}

// simConfig maps module config onto the simulation's tuning knobs.
func (c Config) simConfig() sim.Config {
	sc := sim.DefaultConfig()
	sc.MaxActiveFronts = c.MaxActiveFronts
	sc.SpawnChance *= c.SpawnRateScale
	if sc.SpawnChance > 1 {
		sc.SpawnChance = 1
	}
	return sc
}

// loadConfig reads the module's live config via the plugin API.
func loadConfig(p *plugins.Plugin) Config {
	return buildConfig(func(k string) any { return p.Config.Get(k) })
}
