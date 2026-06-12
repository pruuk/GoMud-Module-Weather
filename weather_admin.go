package weather

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/GoMudEngine/GoMud/internal/events"
	"github.com/GoMudEngine/GoMud/modules/weather/engine"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// AdminSnapshot is the read-side bridge to the admin page: an immutable,
// deep-copied view of module state built ON THE GAME LOOP and published via
// an atomic pointer. HTTP handlers read it and never touch live state — the
// module's mutex-free MainWorker invariant depends on this.
type AdminSnapshot struct {
	SimReady       bool             `json:"simReady"`
	SeasonsOn      bool             `json:"seasonsOn"`
	Round          uint64           `json:"round"`
	NextTickRound  uint64           `json:"nextTickRound"`
	GraphZones     int              `json:"graphZones"`
	GraphEdges     int              `json:"graphEdges"`
	Components     int              `json:"graphComponents"`
	RefinementMode string           `json:"refinementMode"` // occupied | all | off
	RefinedRooms   int              `json:"refinedRooms"`   // occupied-room count when a room mode is active; 0 when off
	Fronts         []AdminFrontRow  `json:"fronts"`
	Zones          []AdminZoneRow   `json:"zones"`
	Config         []AdminConfigRow `json:"config"`
	LastAction     string           `json:"lastAction"` // human-readable result of the most recent admin action
}

// AdminFrontRow is one row of the fronts table in the snapshot.
type AdminFrontRow struct {
	Id        uint64  `json:"id"`
	Type      string  `json:"type"`
	Zone      string  `json:"zone"`
	Intensity float64 `json:"intensity"`
	Moisture  float64 `json:"moisture"`
	Age       int     `json:"age"`
	MaxAge    int     `json:"maxAge"`
}

// AdminZoneRow is one row of the zones table in the snapshot.
type AdminZoneRow struct {
	Zone    string `json:"zone"`
	Biome   string `json:"biome"`
	Weather string `json:"weather"`
	Track   string `json:"track,omitempty"`
	Season  string `json:"season,omitempty"`
}

// AdminConfigRow is one row of the config table in the snapshot.
type AdminConfigRow struct {
	Key      string   `json:"key"`
	Value    any      `json:"value"`
	Badge    string   `json:"badge"`              // "live" or the human reboot/deferred note
	Kind     string   `json:"kind"`               // input widget: "bool" | "int" | "float" | "enum" | "text"
	Options  []string `json:"options,omitempty"`  // enum choices (Kind == "enum")
	ReadOnly bool     `json:"readOnly,omitempty"` // synthetic summary rows; the config API refuses writes
}

// adminSnapshot is the published snapshot; package-level so handlers can read
// it without touching the module. Written only from the game loop.
var adminSnapshot atomic.Pointer[AdminSnapshot]

// loadSnapshot returns the current snapshot, or a valid "not started" one.
func loadSnapshot() *AdminSnapshot {
	if s := adminSnapshot.Load(); s != nil {
		return s
	}
	return &AdminSnapshot{LastAction: "module starting"}
}

// publishSnapshot rebuilds and atomically publishes the snapshot. Game loop only.
//
// Single-publish rule (canonical statement): helpers that mutate state on a
// caller's behalf (rebuildGraph, startSim, ...) never call this; every
// game-loop ENTRY POINT that mutates snapshot-visible state calls it exactly
// once, at its end, after any lastAdminAction attribution is in place — so
// success and failure alike surface exactly once, correctly attributed.
func (m *weatherModule) publishSnapshot() {
	s := m.buildSnapshot()
	adminSnapshot.Store(s)
}

// buildSnapshot deep-copies the admin view of module state. Game loop only.
func (m *weatherModule) buildSnapshot() *AdminSnapshot {
	s := &AdminSnapshot{
		SimReady:       m.simReady,
		SeasonsOn:      m.seasonsOn,
		Round:          m.state.Round,
		NextTickRound:  m.nextTick,
		RefinementMode: m.cfg.PerRoomRefinement,
		LastAction:     m.lastAdminAction,
	}
	if m.cfg.PerRoomRefinement != RefineOff {
		// Cheap: the room manager maintains the occupied set incrementally.
		// Game-loop-only, like the rest of this builder.
		s.RefinedRooms = engine.OccupiedRoomCount()
	}
	if m.graph != nil {
		s.GraphZones = len(m.graph.Nodes)
		s.GraphEdges = len(m.graph.Edges)
		s.Components = m.graph.Components

		zones := m.graph.Zones()
		s.Zones = make([]AdminZoneRow, 0, len(zones))
		for _, z := range zones {
			row := AdminZoneRow{
				Zone:    z,
				Biome:   m.graph.Nodes[z].Biome,
				Weather: string(m.state.Weather[z]),
			}
			if row.Weather == "" {
				row.Weather = string(sim.Clear)
			}
			if zs, ok := m.zoneSeasons[z]; ok {
				row.Track, row.Season = zs.Track, zs.Season
			}
			s.Zones = append(s.Zones, row)
		}
		sort.Slice(s.Zones, func(a, b int) bool { return s.Zones[a].Zone < s.Zones[b].Zone })
	}
	s.Fronts = make([]AdminFrontRow, 0, len(m.state.Fronts))
	for _, f := range m.state.Fronts {
		s.Fronts = append(s.Fronts, AdminFrontRow{
			Id: uint64(f.Id), Type: string(f.Type), Zone: f.Zone,
			Intensity: f.Intensity, Moisture: f.Moisture, Age: f.Age, MaxAge: f.MaxAge,
		})
	}
	s.Config = m.configRows()
	return s
}

// configKeyApplier is the single source of truth for what saving each key does.
// Badge text is shown on the page (via configRows); Kind/Options drive the
// page's typed inputs; Validate (nil = free text) rejects bad writes in the
// API handler; LiveApply (nil = nothing to do immediately) runs on the game
// loop after the new config is adopted.
type configKeyApplier struct {
	Badge    string
	Kind     string   // input widget: "bool" | "int" | "float" | "enum" | "text" ("" = text)
	Options  []string // valid choices when Kind == "enum"
	ReadOnly bool     // row is display-only; handleAdminConfig rejects writes
	// Validate mirrors buildConfig's coercion RULES for the key but REJECTS
	// what the loader would silently default or clamp, so the overrides file
	// never accumulates values the next boot quietly rewrites. Returns the
	// normalized string to persist (canonical bool, lowercased enum, ...).
	Validate  func(string) (string, error)
	LiveApply func(m *weatherModule, old Config)
}

// Enum choice sets shared by the meta table (page <select> options) and the
// validators — single source for both.
var (
	emoteModeOptions  = []string{EmoteModeModule, EmoteModeTagOnly}
	refineModeOptions = []string{RefineOccupied, RefineAll, RefineOff}
)

// validateBool accepts exactly what the engine's config layer accepts when it
// stores the value (configs.ConfigBool.Set → strconv.ParseBool: true/false,
// t/f, 1/0, any case), normalized to canonical "true"/"false".
func validateBool(s string) (string, error) {
	b, err := strconv.ParseBool(strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("%q is not a boolean (use true/false, t/f or 1/0)", s)
	}
	return strconv.FormatBool(b), nil
}

// validateIntMin parses like buildConfig's intOr but rejects bad input and
// values below the loader's clamp floor instead of defaulting/clamping.
func validateIntMin(min int) func(string) (string, error) {
	return func(s string) (string, error) {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return "", fmt.Errorf("%q is not a whole number", s)
		}
		if n < min {
			return "", fmt.Errorf("%d is out of range (must be %d or higher)", n, min)
		}
		return strconv.Itoa(n), nil
	}
}

// validateFloatMin is the floatOr counterpart of validateIntMin.
func validateFloatMin(min float64) func(string) (string, error) {
	return func(s string) (string, error) {
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return "", fmt.Errorf("%q is not a number", s)
		}
		if f < min {
			return "", fmt.Errorf("%v is out of range (must be %v or higher)", f, min)
		}
		return strconv.FormatFloat(f, 'g', -1, 64), nil
	}
}

// validateEnum accepts exactly the listed values, case-insensitively,
// normalized to the canonical (lowercase) spelling.
func validateEnum(options []string) func(string) (string, error) {
	return func(s string) (string, error) {
		v := strings.ToLower(strings.TrimSpace(s))
		for _, o := range options {
			if v == o {
				return o, nil
			}
		}
		return "", fmt.Errorf("%q is not one of: %s", s, strings.Join(options, ", "))
	}
}

// validateFreeText trims and passes anything through — keys whose loader-side
// parsing is already permissive (comma-separated pattern lists).
func validateFreeText(s string) (string, error) { return strings.TrimSpace(s), nil }

// configKeyMeta maps each public config key to its apply semantics, badge,
// input kind and validation.
var configKeyMeta = map[string]configKeyApplier{
	"Enabled":            {Badge: "takes effect on reboot", Kind: "bool", Validate: validateBool},
	"IncludeSecretExits": {Badge: "applies on next graph rebuild", Kind: "bool", Validate: validateBool},
	"RebuildGraphOnBoot": {Badge: "boot flag", Kind: "bool", Validate: validateBool},
	// Seed 0 = "derive from the world's zone names"; negatives would wrap.
	"Seed": {Badge: "applies when state is re-seeded", Kind: "int", Validate: validateIntMin(minSeed)},
	"TickEveryGameHours": {Badge: "live", Kind: "int", Validate: validateIntMin(minTickEveryGameHours), LiveApply: func(m *weatherModule, _ Config) {
		m.simCfg = m.cfg.simConfig()
		m.nextTick = engine.NextTickRound(engine.TickPeriod(m.cfg.TickEveryGameHours))
	}},
	"MaxActiveFronts": {Badge: "live", Kind: "int", Validate: validateIntMin(minMaxActiveFronts), LiveApply: func(m *weatherModule, _ Config) {
		m.simCfg = m.cfg.simConfig()
	}},
	"SpawnRateScale": {Badge: "live", Kind: "float", Validate: validateFloatMin(minSpawnRateScale), LiveApply: func(m *weatherModule, _ Config) {
		m.simCfg = m.cfg.simConfig()
	}},
	"EmoteMode": {Badge: "live", Kind: "enum", Options: emoteModeOptions, Validate: validateEnum(emoteModeOptions)},
	"EmoteEveryRounds": {Badge: "live", Kind: "int", Validate: validateIntMin(minEmoteEveryRounds), LiveApply: func(m *weatherModule, _ Config) {
		m.scheduleEmote(engine.CurrentRound())
	}},
	"BuffsEnabled": {Badge: "live to disable; reboot to re-enable", Kind: "bool", Validate: validateBool, LiveApply: func(m *weatherModule, old Config) {
		if old.BuffsEnabled && !m.cfg.BuffsEnabled {
			stripBuffsFn()
		}
		// false->true has no live path (no restore) — badge says reboot.
	}},
	"Persist": {Badge: "live", Kind: "bool", Validate: validateBool},
	"PerRoomRefinement": {Badge: "live", Kind: "enum", Options: refineModeOptions, Validate: validateEnum(refineModeOptions), LiveApply: func(m *weatherModule, old Config) {
		if old.PerRoomRefinement == m.cfg.PerRoomRefinement {
			return
		}
		// Leaving a room mode for "off": clear room mutators from occupied
		// rooms before re-asserting zone-level weather. (For all->off and
		// all->occupied, strays in UNOCCUPIED rooms are left to the specs'
		// decayrate safety net — the engine retires them on room load/round
		// tick without a world-wide pass.)
		if old.PerRoomRefinement != RefineOff && m.cfg.PerRoomRefinement == RefineOff {
			engine.StripOccupiedRoomWeather()
		}
		// off->room mode needs the zone footprint stripped first; applyWeather
		// does StripZoneWeather before refining, so ordering is covered.
		m.applyWeather()
	}},
	"SeasonsEnabled": {Badge: "live", Kind: "bool", Validate: validateBool, LiveApply: func(m *weatherModule, old Config) {
		switch {
		case old.SeasonsEnabled && !m.cfg.SeasonsEnabled:
			m.seasonsOn = false
			m.zoneSeasons = nil
			engine.ReconcileSeasons(m.graph, nil) // strip season mutators now
		case !old.SeasonsEnabled && m.cfg.SeasonsEnabled:
			m.loadSeasons() // idempotent; baseline without events
		}
	}},
	// No LiveApply: the new patterns persist and the existing rebuild action
	// (or 'weather rebuild') picks them up from m.cfg.
	"ExcludeZonePatterns": {Badge: "applies on next graph rebuild", Kind: "text", Validate: validateFreeText},
	// BuffOverrides.<type> are per-type flat keys set in config-overrides.yaml;
	// the table shows ONE synthetic read-only summary row for all of them.
	"BuffOverrides.*": {Badge: "takes effect on reboot", Kind: "text", ReadOnly: true},
}

// configRows serializes the config view for the snapshot. Badges come from
// configKeyMeta — single source of truth; every public key must appear.
// NOTE: snapshot isolation depends on every Value below being a scalar —
// Config's slice/map fields (ExcludeZonePatterns, BuffOverrides) are rendered
// to fresh strings here for exactly that reason; a future slice/map value
// must be deep-copied or stringified the same way.
func (m *weatherModule) configRows() []AdminConfigRow {
	c := m.cfg
	values := map[string]any{
		"Enabled": c.Enabled, "IncludeSecretExits": c.IncludeSecretExits,
		"RebuildGraphOnBoot": c.RebuildGraphOnBoot, "Seed": c.Seed,
		"TickEveryGameHours": c.TickEveryGameHours, "MaxActiveFronts": c.MaxActiveFronts,
		"SpawnRateScale": c.SpawnRateScale, "EmoteMode": c.EmoteMode,
		"EmoteEveryRounds": c.EmoteEveryRounds, "BuffsEnabled": c.BuffsEnabled,
		"Persist": c.Persist, "SeasonsEnabled": c.SeasonsEnabled,
		"PerRoomRefinement":   c.PerRoomRefinement,
		"ExcludeZonePatterns": strings.Join(c.ExcludeZonePatterns, ","),
		"BuffOverrides.*":     buffOverridesSummary(c.BuffOverrides),
	}
	rows := make([]AdminConfigRow, 0, len(values))
	for key, val := range values {
		meta := configKeyMeta[key]
		kind := meta.Kind
		if kind == "" {
			kind = "text"
		}
		rows = append(rows, AdminConfigRow{
			Key: key, Value: val, Badge: meta.Badge, Kind: kind,
			// Fresh copy per snapshot — same isolation rule as the values above.
			Options:  append([]string(nil), meta.Options...),
			ReadOnly: meta.ReadOnly,
		})
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].Key < rows[b].Key })
	return rows
}

// buffOverridesSummary renders the synthetic BuffOverrides.* row value, e.g.
// "blizzard→[]; storm→[59002]" ("[]" = explicit strip); "(none)" when no
// overrides are configured.
func buffOverridesSummary(overrides map[string][]int) string {
	if len(overrides) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s→%v", k, overrides[k]))
	}
	return strings.Join(parts, "; ")
}

// applyConfigChange adopts a freshly re-read config and runs the changed
// key's live applier. Game loop only. Refreshes the snapshot.
func (m *weatherModule) applyConfigChange(newCfg Config, key string) {
	old := m.cfg
	m.cfg = newCfg
	if meta, ok := configKeyMeta[key]; ok && meta.LiveApply != nil && m.simReady {
		meta.LiveApply(m, old)
	}
	m.lastAdminAction = "config " + key + " saved"
	m.publishSnapshot() // single-publish rule: see publishSnapshot
}

// rebuildGraphFn is a test seam: rebuildGraph crawls the live world and logs
// through the engine logger, neither of which exists under `go test`.
var rebuildGraphFn = (*weatherModule).rebuildGraph

// applyAdminAction executes a web-initiated action on the game loop through
// the same paths as the in-game commands. Refreshes the snapshot.
func (m *weatherModule) applyAdminAction(a WeatherAdminAction) {
	if !m.simReady && a.Action != "rebuild" {
		m.lastAdminAction = a.Action + ": simulation not running"
		m.publishSnapshot()
		return
	}
	switch a.Action {
	case "spawn":
		zone, ok := m.graph.FindZone(a.Zone)
		if !ok {
			m.lastAdminAction = "spawn: unknown zone " + a.Zone
			break
		}
		next, _, ok := sim.ForceSpawn(m.state, m.graph, m.simCfg, sim.WeatherType(a.Weather), zone, a.Intensity, sim.Clock{Round: engine.CurrentRound()})
		if !ok {
			m.lastAdminAction = "spawn failed"
			break
		}
		m.state = next
		m.applyWeather()
		m.persistState()
		m.lastAdminAction = "spawned " + a.Weather + " @ " + zone
	case "clear":
		var zones []sim.ZoneId
		label := "everywhere"
		if a.Zone != "" {
			zone, ok := m.graph.FindZone(a.Zone)
			if !ok {
				m.lastAdminAction = "clear: unknown zone " + a.Zone
				break
			}
			zones = []sim.ZoneId{zone}
			label = zone
		}
		next, _ := sim.ClearZones(m.state, m.graph, m.simCfg, zones, sim.Clock{Round: engine.CurrentRound()})
		m.state = next
		m.applyWeather()
		m.persistState()
		m.lastAdminAction = "cleared " + label
	case "rebuild":
		// Single-publish rule: see publishSnapshot (the one publish below the
		// switch runs after lastAdminAction is set).
		rebuildGraphFn(m)
		// Same honesty limit as the in-game command: a failed crawl that kept
		// the old graph isn't detectable here, but a nil graph is.
		if m.graph == nil {
			m.lastAdminAction = "graph rebuild failed (see server log)"
		} else {
			m.lastAdminAction = "graph rebuilt"
		}
	default:
		m.lastAdminAction = "unknown action " + a.Action
	}
	m.publishSnapshot() // single-publish rule: see publishSnapshot
}

// onAdminAction / onConfigChanged run on the game loop (MainWorker) — the
// write-side bridges from the admin web API.
func (m *weatherModule) onAdminAction(e events.Event) events.ListenerReturn {
	if a, ok := e.(WeatherAdminAction); ok {
		m.applyAdminAction(a)
	}
	return events.Continue
}

func (m *weatherModule) onConfigChanged(e events.Event) events.ListenerReturn {
	if c, ok := e.(WeatherConfigChanged); ok {
		m.applyConfigChange(loadConfig(m.plug), c.Key)
	}
	return events.Continue
}
