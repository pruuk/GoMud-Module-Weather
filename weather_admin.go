package weather

import (
	"sort"
	"sync/atomic"

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

// configRows serializes the config view for the snapshot. Badges come from
// the apply table (Task 3); every public key must appear.
func (m *weatherModule) configRows() []AdminConfigRow {
	c := m.cfg
	rows := []AdminConfigRow{
		{"Enabled", c.Enabled, "takes effect on reboot"},
		{"IncludeSecretExits", c.IncludeSecretExits, "applies on next graph rebuild"},
		{"RebuildGraphOnBoot", c.RebuildGraphOnBoot, "boot flag"},
		{"Seed", c.Seed, "applies when state is re-seeded"},
		{"TickEveryGameHours", c.TickEveryGameHours, "live"},
		{"MaxActiveFronts", c.MaxActiveFronts, "live"},
		{"SpawnRateScale", c.SpawnRateScale, "live"},
		{"EmoteMode", c.EmoteMode, "live"},
		{"EmoteEveryRounds", c.EmoteEveryRounds, "live"},
		{"BuffsEnabled", c.BuffsEnabled, "live to disable; reboot to re-enable"},
		{"Persist", c.Persist, "live"},
		{"SeasonsEnabled", c.SeasonsEnabled, "live"},
	}
	return rows
}
