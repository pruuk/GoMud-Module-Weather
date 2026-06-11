package weather

import (
	"sort"
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
	SimReady      bool             `json:"simReady"`
	SeasonsOn     bool             `json:"seasonsOn"`
	Round         uint64           `json:"round"`
	NextTickRound uint64           `json:"nextTickRound"`
	GraphZones    int              `json:"graphZones"`
	GraphEdges    int              `json:"graphEdges"`
	Components    int              `json:"graphComponents"`
	Fronts        []AdminFrontRow  `json:"fronts"`
	Zones         []AdminZoneRow   `json:"zones"`
	Config        []AdminConfigRow `json:"config"`
	LastAction    string           `json:"lastAction"` // human-readable result of the most recent admin action
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
	Key   string `json:"key"`
	Value any    `json:"value"`
	Badge string `json:"badge"` // "live" or the human reboot/deferred note
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
func (m *weatherModule) publishSnapshot() {
	s := m.buildSnapshot()
	adminSnapshot.Store(s)
}

// buildSnapshot deep-copies the admin view of module state. Game loop only.
func (m *weatherModule) buildSnapshot() *AdminSnapshot {
	s := &AdminSnapshot{
		SimReady:      m.simReady,
		SeasonsOn:     m.seasonsOn,
		Round:         m.state.Round,
		NextTickRound: m.nextTick,
		LastAction:    m.lastAdminAction,
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
// Badge text is shown on the page (via configRows); LiveApply (nil = nothing
// to do immediately) runs on the game loop after the new config is adopted.
type configKeyApplier struct {
	Badge     string
	LiveApply func(m *weatherModule, old Config)
}

// configKeyMeta maps each public config key to its apply semantics and badge.
var configKeyMeta = map[string]configKeyApplier{
	"Enabled":            {Badge: "takes effect on reboot"},
	"IncludeSecretExits": {Badge: "applies on next graph rebuild"},
	"RebuildGraphOnBoot": {Badge: "boot flag"},
	"Seed":               {Badge: "applies when state is re-seeded"},
	"TickEveryGameHours": {Badge: "live", LiveApply: func(m *weatherModule, _ Config) {
		m.simCfg = m.cfg.simConfig()
		m.nextTick = engine.NextTickRound(engine.TickPeriod(m.cfg.TickEveryGameHours))
	}},
	"MaxActiveFronts": {Badge: "live", LiveApply: func(m *weatherModule, _ Config) {
		m.simCfg = m.cfg.simConfig()
	}},
	"SpawnRateScale": {Badge: "live", LiveApply: func(m *weatherModule, _ Config) {
		m.simCfg = m.cfg.simConfig()
	}},
	"EmoteMode": {Badge: "live"},
	"EmoteEveryRounds": {Badge: "live", LiveApply: func(m *weatherModule, _ Config) {
		m.scheduleEmote(engine.CurrentRound())
	}},
	"BuffsEnabled": {Badge: "live to disable; reboot to re-enable", LiveApply: func(m *weatherModule, old Config) {
		if old.BuffsEnabled && !m.cfg.BuffsEnabled {
			engine.StripBuffs()
		}
		// false->true has no live path (no restore) — badge says reboot.
	}},
	"Persist": {Badge: "live"},
	"SeasonsEnabled": {Badge: "live", LiveApply: func(m *weatherModule, old Config) {
		switch {
		case old.SeasonsEnabled && !m.cfg.SeasonsEnabled:
			m.seasonsOn = false
			m.zoneSeasons = nil
			engine.ReconcileSeasons(m.graph, nil) // strip season mutators now
		case !old.SeasonsEnabled && m.cfg.SeasonsEnabled:
			m.loadSeasons() // idempotent; baseline without events
		}
	}},
}

// configRows serializes the config view for the snapshot. Badges come from
// configKeyMeta — single source of truth; every public key must appear.
// NOTE: snapshot isolation depends on Config holding ONLY scalar fields — a
// future slice/map config value would cross into the snapshot by reference
// through the `any` Value and must be deep-copied here instead.
func (m *weatherModule) configRows() []AdminConfigRow {
	c := m.cfg
	values := map[string]any{
		"Enabled": c.Enabled, "IncludeSecretExits": c.IncludeSecretExits,
		"RebuildGraphOnBoot": c.RebuildGraphOnBoot, "Seed": c.Seed,
		"TickEveryGameHours": c.TickEveryGameHours, "MaxActiveFronts": c.MaxActiveFronts,
		"SpawnRateScale": c.SpawnRateScale, "EmoteMode": c.EmoteMode,
		"EmoteEveryRounds": c.EmoteEveryRounds, "BuffsEnabled": c.BuffsEnabled,
		"Persist": c.Persist, "SeasonsEnabled": c.SeasonsEnabled,
	}
	rows := make([]AdminConfigRow, 0, len(values))
	for key, val := range values {
		rows = append(rows, AdminConfigRow{Key: key, Value: val, Badge: configKeyMeta[key].Badge})
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].Key < rows[b].Key })
	return rows
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
	m.publishSnapshot()
}

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
		engine.Reconcile(m.state.Weather)
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
		engine.Reconcile(m.state.Weather)
		m.persistState()
		m.lastAdminAction = "cleared " + label
	case "rebuild":
		m.rebuildGraph()
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
	m.publishSnapshot()
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
