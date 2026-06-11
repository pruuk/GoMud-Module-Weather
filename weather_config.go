package weather

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/GoMudEngine/GoMud/internal/configs"
	"github.com/GoMudEngine/GoMud/internal/mudlog"
	"github.com/GoMudEngine/GoMud/internal/plugins"
	"github.com/GoMudEngine/GoMud/modules/weather/crawler"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
	"gopkg.in/yaml.v2"
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
// Modules.weather.*). Defaults live in TWO places kept identical by
// TestOverlayMatchesCodeDefaults: buildConfig below (defense in depth — a
// partial or clobbered live config still resolves usable values) and the
// shipped data overlay (files/data-overlays/config.yaml), whose ACTIVE keys
// are what registers Modules.weather.* with the engine so configs.SetVal —
// the admin page's write path — accepts them. The engine's overlay merge is
// destructive (see healConfigClobber); the module self-heals at boot.
// Keys are flat (BuffsEnabled, not Buffs.Enabled) because plugin config
// lookup reads flattened scalar leaves.
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
		// Enabled defaults TRUE when absent (OOBE: a fresh install boots
		// working with zero config) — only an explicit false disables.
		Enabled:            boolOr(get("Enabled"), true),
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

// ---------------------------------------------------------------------------
// Boot self-heal for the engine's destructive overlay merge.
//
// The trap has two sides (verified in engine source):
//
//  1. plugins.Load() feeds the keys of files/data-overlays/config.yaml to
//     configs.AddOverlayOverrides (internal/configs/configs.go:63) — the ONLY
//     mechanism that registers Modules.weather.* with the config layer's
//     key/type lookups. Without active overlay keys, configs.SetVal rejects
//     every admin-page write ("invalid property name") — silently, because
//     PluginConfig.Set discards the error (internal/plugins/pluginconfig.go:13).
//     So the overlay MUST ship active keys.
//  2. The same call collects the overlay keys MISSING from the operator's
//     config-overrides.yaml block and hands just those to
//     Config.OverlayOverrides, which yaml-unmarshals
//     {Modules:{weather:{<newKeys>}}} into the live Config — REPLACING the
//     inner Modules["weather"] map wholesale (Modules is map[string]any).
//     Every operator-set key vanishes from the LIVE config; the file itself
//     is untouched. This fires exactly on upgrade boots after the operator
//     ever used the admin page (its SetVal persistence wrote the block, and a
//     module update that adds an overlay key makes that block incomplete).
//
// The cure is SetVal itself (configs.go:329): one successful write of ONE
// registered key merges that key into the in-memory overrides union — which
// still holds every operator file value — re-applies the ENTIRE union to the
// live config, and writes the whole union back to config-overrides.yaml. That
// single call restores everything the boot clobber wiped AND completes the
// file's block so future boots have no new keys (the clobber never fires
// again). healConfigClobber detects the wipe (file value != live value) and
// performs that one write. It runs in onLoad BEFORE the config read, so an
// operator's Enabled:false (or any other setting) is honored on the very boot
// that would otherwise have lost it.
// ---------------------------------------------------------------------------

// overridesPath mirrors the engine's overridePathNoLock
// (internal/configs/configs.go:389-395): CONFIG_PATH env var wins, else
// FilePaths.DataFiles + "/config-overrides.yaml".
func overridesPath() string {
	if p := os.Getenv(`CONFIG_PATH`); p != `` {
		return p
	}
	return configs.GetConfig().FilePaths.DataFiles.String() + `/config-overrides.yaml`
}

// readOverridesFn / infoConfig are seams (module style, mirroring
// applyBuffOverridesFn): the real heal touches the filesystem, the engine
// config layer and the engine logger, none of which exist under `go test`.
var readOverridesFn = func() ([]byte, error) { return os.ReadFile(overridesPath()) }
var infoConfig = mudlog.Info

// healConfigClobber wires the live plugin into the testable core. Game loop
// only (onLoad), before loadConfig.
func (m *weatherModule) healConfigClobber() {
	healClobberedConfig(
		readOverridesFn,
		func(k string) any { return m.plug.Config.Get(k) },
		func(k, v string) { m.plug.Config.Set(k, v) },
	)
}

// healClobberedConfig is the testable core of the boot self-heal (mechanism:
// see the block comment above). Total by design: any IO/parse failure warns
// once and returns — it must never block boot; buildConfig's code defaults
// keep the module functional either way.
func healClobberedConfig(read func() ([]byte, error), get func(string) any, set func(key, value string)) {
	raw, err := read()
	if err != nil {
		// No file = pristine world: the overlay supplied everything and there
		// is no operator block the clobber could have wiped.
		if !os.IsNotExist(err) {
			warnConfig("Weather: cannot read config-overrides.yaml; skipping config-clobber check", "error", err)
		}
		return
	}
	block, err := weatherOverridesBlock(raw)
	if err != nil {
		warnConfig("Weather: cannot parse config-overrides.yaml; skipping config-clobber check", "error", err)
		return
	}
	if len(block) == 0 {
		return // no Modules.weather block: nothing the clobber could have wiped
	}

	flat := map[string]any{}
	flattenLeaves("", block, flat)

	// Compare every file leaf against the live config. The file keeps its own
	// key spelling on purpose: the live entry a hand-edited key produced (or
	// would produce after a restore) uses that same spelling, so file-spelled
	// lookups are the self-consistent comparison even for non-canonical case.
	var mismatched []string
	for k, v := range flat {
		if !configValuesEqual(v, get(k)) {
			mismatched = append(mismatched, k)
		}
	}
	if len(mismatched) == 0 {
		return // steady state: full file block, no clobber this boot
	}
	sort.Strings(mismatched) // deterministic heal-key choice + readable logs

	// The boot clobber fired. ONE successful Set of a REGISTERED key restores
	// the whole union (see block comment), so prefer a mismatched registered
	// key — the write is then also the fix. If only unregistered keys
	// mismatch (e.g. BuffOverrides.storm), write any registered key back with
	// its current correct value (file value if present, else live value): the
	// value is a no-op, the union re-apply side-effect is the point.
	healKey, healVal := "", ""
	for _, k := range mismatched {
		if registeredConfigKey(k) {
			healKey, healVal = k, fmt.Sprint(flat[k])
			break
		}
	}
	if healKey == "" {
		for _, k := range writableConfigKeys() {
			if v, ok := flat[k]; ok {
				healKey, healVal = k, fmt.Sprint(v)
				break
			}
			if v := get(k); v != nil {
				healKey, healVal = k, fmt.Sprint(v)
				break
			}
		}
	}
	if healKey == "" { // unreachable with the shipped overlay; stay total
		warnConfig("Weather: operator config clobbered but no registered key available to heal through",
			"mismatched", strings.Join(mismatched, ","))
		return
	}

	set(healKey, healVal)

	// Re-verify: the union re-apply should have restored every wiped key,
	// unregistered ones included (the union holds the whole file block). A
	// remaining mismatch means the engine rejected the write (engine too
	// old/new) — fall through; code defaults keep the module functional.
	stillBad := 0
	for _, k := range mismatched {
		if !configValuesEqual(flat[k], get(k)) {
			stillBad++
		}
	}
	if stillBad > 0 {
		warnConfig("Weather: config heal write did not take — operator values in config-overrides.yaml are NOT live this boot",
			"healKey", healKey, "unhealed", stillBad, "mismatched", strings.Join(mismatched, ","))
		return
	}
	infoConfig("Weather: restored operator config after engine overlay clobber",
		"keysHealed", len(mismatched), "via", healKey)
}

// weatherOverridesBlock extracts the Modules.weather mapping from the raw
// overrides file. Both lookups are case-insensitive: yaml.v2 keeps keys
// verbatim and hand-edited files may use any case (the engine itself
// normalizes via findFullPathFrom when it round-trips the file).
func weatherOverridesBlock(raw []byte) (map[string]any, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	mods, ok := lookupFold(doc, "Modules")
	if !ok {
		return nil, nil
	}
	weather, ok := lookupFold(asStringMap(mods), "weather")
	if !ok {
		return nil, nil
	}
	return asStringMap(weather), nil
}

// lookupFold is a case-insensitive map lookup (exact match wins).
func lookupFold(m map[string]any, key string) (any, bool) {
	if v, ok := m[key]; ok {
		return v, true
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

// asStringMap normalizes yaml.v2's nested map[interface{}]interface{} (and
// pass-through map[string]any) to map[string]any; nil for non-maps.
func asStringMap(v any) map[string]any {
	switch m := v.(type) {
	case map[string]any:
		return m
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out
	}
	return nil
}

// flattenLeaves flattens nested maps to dotted keys (BuffOverrides.storm, …),
// matching the engine's own configs.Flatten shape that plug.Config.Get reads.
func flattenLeaves(prefix string, m map[string]any, out map[string]any) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if sub := asStringMap(v); sub != nil {
			flattenLeaves(key, sub, out)
			continue
		}
		out[key] = v
	}
}

// configValuesEqual compares a YAML-typed file value against what the live
// config holds. The two sides never share a type system — the file carries
// yaml.v2 scalars, while Get returns whatever the engine's last
// OverlayOverrides yaml round-trip produced (bool/int/float64/string; e.g.
// SpawnRateScale 1.0 re-reads as int 1) — so compare canonical string forms.
// fmt.Sprint prints int 1 and float64 1 both as "1", bools as "true"/"false",
// and a vanished key (nil) as "<nil>", which never equals a real file value.
func configValuesEqual(a, b any) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// registeredConfigKey reports whether the engine accepts configs.SetVal for
// this key — true exactly for the shipped overlay's active keys, which are
// pinned equal to configKeyMeta's writable keys by
// TestOverlayMatchesCodeDefaults.
func registeredConfigKey(k string) bool {
	meta, ok := configKeyMeta[k]
	return ok && !meta.ReadOnly
}

// writableConfigKeys returns the registered key set, sorted (deterministic
// fallback choice in healClobberedConfig).
func writableConfigKeys() []string {
	keys := make([]string, 0, len(configKeyMeta))
	for k, meta := range configKeyMeta {
		if !meta.ReadOnly {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
