package weather

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/seasons"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

func adminTestModule() *weatherModule {
	g := &sim.Graph{Nodes: map[string]sim.ZoneNode{
		"Frost": {Zone: "Frost", Biome: "tundra"},
		"Dune":  {Zone: "Dune", Biome: "desert"},
	}}
	m := &weatherModule{
		cfg:       buildConfig(func(string) any { return nil }),
		graph:     g,
		simReady:  true,
		seasonsOn: true,
		simCfg:    sim.DefaultConfig(),
		state: sim.State{
			Round:  42,
			Fronts: []sim.Front{{Id: 7, Type: "storm", Zone: "Frost", Intensity: 0.8, Age: 3, MaxAge: 24}},
			Weather: map[sim.ZoneId]sim.WeatherType{
				"Frost": "storm", "Dune": sim.Clear,
			},
		},
		zoneSeasons: map[sim.ZoneId]seasons.ZoneSeason{
			"Frost": {Track: "temperate", Season: "winter", Blend: 1.0},
		},
		nextTick: 1000,
	}
	return m
}

func TestBuildSnapshot(t *testing.T) {
	m := adminTestModule()
	s := m.buildSnapshot()
	if !s.SimReady || !s.SeasonsOn {
		t.Errorf("flags: %+v", s)
	}
	if s.Round != 42 || s.NextTickRound != 1000 {
		t.Errorf("rounds: %+v", s)
	}
	if len(s.Fronts) != 1 || s.Fronts[0].Type != "storm" || s.Fronts[0].Zone != "Frost" {
		t.Errorf("fronts: %+v", s.Fronts)
	}
	if len(s.Zones) != 2 {
		t.Fatalf("zones: %+v", s.Zones)
	}
	// Zones sorted by name; Dune first.
	if s.Zones[0].Zone != "Dune" || s.Zones[0].Weather != "clear" || s.Zones[0].Season != "" {
		t.Errorf("Dune row: %+v", s.Zones[0])
	}
	if s.Zones[1].Zone != "Frost" || s.Zones[1].Weather != "storm" || s.Zones[1].Season != "winter" || s.Zones[1].Track != "temperate" {
		t.Errorf("Frost row: %+v", s.Zones[1])
	}
	// Config rows cover every public key with a badge.
	if len(s.Config) == 0 {
		t.Fatal("config rows missing")
	}
	seen := map[string]bool{}
	for _, c := range s.Config {
		seen[c.Key] = true
		if c.Badge == "" {
			t.Errorf("key %s missing badge", c.Key)
		}
	}
	for _, want := range []string{"TickEveryGameHours", "SeasonsEnabled", "BuffsEnabled", "Enabled", "Seed"} {
		if !seen[want] {
			t.Errorf("config row for %s missing", want)
		}
	}
}

func TestSnapshotIsolation(t *testing.T) {
	m := adminTestModule()
	s := m.buildSnapshot()
	// Mutating the snapshot must not touch module state (deep copy).
	s.Fronts[0].Type = "tampered"
	s.Zones[0].Weather = "tampered"
	if m.state.Fronts[0].Type != "storm" || m.state.Weather["Dune"] != sim.Clear {
		t.Error("snapshot shares memory with module state")
	}
}

func TestPublishAndLoadSnapshot(t *testing.T) {
	m := adminTestModule()
	m.publishSnapshot()
	s := loadSnapshot()
	if s == nil || !s.SimReady {
		t.Fatalf("published snapshot not readable: %+v", s)
	}
	if !strings.Contains(strings.Join(configKeysOf(s), ","), "SpawnRateScale") {
		t.Error("config keys incomplete")
	}
}

func configKeysOf(s *AdminSnapshot) []string {
	out := make([]string, 0, len(s.Config))
	for _, c := range s.Config {
		out = append(out, c.Key)
	}
	return out
}

func TestConfigKeyMetaCoversAllKeys(t *testing.T) {
	m := adminTestModule()
	for _, row := range m.configRows() {
		meta, ok := configKeyMeta[row.Key]
		if !ok {
			t.Errorf("no meta for %s", row.Key)
			continue
		}
		if meta.Badge != row.Badge {
			t.Errorf("%s: row badge %q != meta badge %q", row.Key, row.Badge, meta.Badge)
		}
	}
	if len(configKeyMeta) != 12 {
		t.Errorf("expected 12 config keys, got %d", len(configKeyMeta))
	}
}

func TestApplyConfigChangeLiveKeys(t *testing.T) {
	m := adminTestModule()
	// Simulate a persisted change: cfg re-read happens via loadConfig in the
	// real path; here we hand applyConfigChange the new config directly.
	newCfg := m.cfg
	newCfg.SpawnRateScale = 0 // stops new fronts
	newCfg.TickEveryGameHours = 6
	m.applyConfigChange(newCfg, "SpawnRateScale")
	if m.cfg.SpawnRateScale != 0 {
		t.Error("cfg not adopted")
	}
	if m.simCfg.SpawnChance != 0 {
		t.Error("simCfg not re-derived for live key")
	}
}

func TestApplyConfigChangeSeasonsToggle(t *testing.T) {
	// applyConfigChange's season-disable path calls engine.ReconcileSeasons
	// which iterates g.Zones() and calls rooms.GetZoneConfig for each — in this
	// unit test the graph zones ("Frost", "Dune") don't exist in the live room
	// registry, so GetZoneConfig returns nil and the call is a no-op loop.
	// The test therefore validates field mutations without a booted world.
	m := adminTestModule()
	newCfg := m.cfg
	newCfg.SeasonsEnabled = false
	m.applyConfigChange(newCfg, "SeasonsEnabled")
	if m.seasonsOn {
		t.Error("seasons should turn off live")
	}
	if len(m.zoneSeasons) != 0 {
		t.Error("zone seasons should clear on live disable")
	}
}

func TestStatusHandler(t *testing.T) {
	m := adminTestModule()
	m.publishSnapshot()
	status, success, data := m.handleAdminStatus(httptest.NewRequest("GET", "/admin/api/v1/weather/status", nil))
	if status != 200 || !success {
		t.Fatalf("status=%d success=%v", status, success)
	}
	snap, ok := data.(*AdminSnapshot)
	if !ok || !snap.SimReady {
		t.Fatalf("payload: %T %+v", data, data)
	}
}

func TestConfigHandlerValidation(t *testing.T) {
	m := adminTestModule()
	bad := httptest.NewRequest("POST", "/x", strings.NewReader(`{"key":"NotAKey","value":"1"}`))
	if status, success, _ := m.handleAdminConfig(bad); status != 400 || success {
		t.Errorf("unknown key must 400: %d %v", status, success)
	}
	malformed := httptest.NewRequest("POST", "/x", strings.NewReader(`{nope`))
	if status, _, _ := m.handleAdminConfig(malformed); status != 400 {
		t.Errorf("malformed body must 400: %d", status)
	}
}

func TestActionHandlerValidation(t *testing.T) {
	m := adminTestModule()
	bad := httptest.NewRequest("POST", "/x", strings.NewReader(`{"action":"explode"}`))
	if status, success, _ := m.handleAdminAction(bad); status != 400 || success {
		t.Errorf("unknown action must 400: %d %v", status, success)
	}
	missing := httptest.NewRequest("POST", "/x", strings.NewReader(`{"action":"spawn","zone":""}`))
	if status, _, _ := m.handleAdminAction(missing); status != 400 {
		t.Errorf("spawn without zone must 400: %d", status)
	}
}
