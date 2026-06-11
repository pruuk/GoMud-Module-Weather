package weather

import (
	"fmt"
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
	for _, want := range []string{"TickEveryGameHours", "SeasonsEnabled", "BuffsEnabled", "Enabled", "Seed", "PerRoomRefinement", "ExcludeZonePatterns", "BuffOverrides.*"} {
		if !seen[want] {
			t.Errorf("config row for %s missing", want)
		}
	}
}

func TestBuildSnapshotRefinementFields(t *testing.T) {
	m := adminTestModule()
	// Default mode is "occupied"; no players in the test world -> 0 rooms.
	s := m.buildSnapshot()
	if s.RefinementMode != RefineOccupied {
		t.Errorf("RefinementMode: %q", s.RefinementMode)
	}
	if s.RefinedRooms != 0 {
		t.Errorf("RefinedRooms should be 0 with no players: %d", s.RefinedRooms)
	}
	m.cfg.PerRoomRefinement = RefineOff
	if s = m.buildSnapshot(); s.RefinementMode != RefineOff || s.RefinedRooms != 0 {
		t.Errorf("off mode: %q %d", s.RefinementMode, s.RefinedRooms)
	}
}

func TestApplyConfigChangeRefinementMode(t *testing.T) {
	// Mode switches run their LiveApply on the game loop; in this unit test the
	// graph zones aren't in the live room registry, so the engine calls are
	// no-op loops — we validate adoption and snapshot refresh.
	m := adminTestModule()
	for _, mode := range []string{RefineAll, RefineOff, RefineOccupied} {
		newCfg := m.cfg
		newCfg.PerRoomRefinement = mode
		m.applyConfigChange(newCfg, "PerRoomRefinement")
		if m.cfg.PerRoomRefinement != mode {
			t.Fatalf("mode %q not adopted: %q", mode, m.cfg.PerRoomRefinement)
		}
		if snap := loadSnapshot(); snap.RefinementMode != mode {
			t.Errorf("snapshot not refreshed for %q: %q", mode, snap.RefinementMode)
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
	// Single source for the expected key count.
	const wantConfigKeys = 15
	if len(configKeyMeta) != wantConfigKeys {
		t.Errorf("expected %d config keys, got %d", wantConfigKeys, len(configKeyMeta))
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
	// The synthetic BuffOverrides.* row is display-only: writes must be
	// refused here even before the page renders it read-only (Task 6).
	readOnly := httptest.NewRequest("POST", "/x", strings.NewReader(`{"key":"BuffOverrides.*","value":"59002"}`))
	if status, success, _ := m.handleAdminConfig(readOnly); status != 400 || success {
		t.Errorf("read-only key must 400: %d %v", status, success)
	}
}

// TestConfigHandlerValueValidation: bad values 400 with a useful message;
// good values pass validation (the fabricated test module has no plugin, so
// they then hit the 503 plug-nil guard — which proves validation PASSED,
// since validation runs before that guard).
func TestConfigHandlerValueValidation(t *testing.T) {
	m := adminTestModule()
	cases := []struct {
		name, key, value string
		wantStatus       int
		wantMsgPart      string // for 400s: the message must mention this
	}{
		// ints
		{"bad int", "TickEveryGameHours", "abc", 400, "whole number"},
		{"int below clamp floor", "TickEveryGameHours", "0", 400, "1 or higher"},
		{"good int", "TickEveryGameHours", "6", 503, ""},
		{"negative front budget", "MaxActiveFronts", "-1", 400, "1 or higher"},
		{"good front budget", "MaxActiveFronts", "12", 503, ""},
		{"emote cadence below floor", "EmoteEveryRounds", "4", 400, "5 or higher"},
		{"good emote cadence", "EmoteEveryRounds", "30", 503, ""},
		{"negative seed", "Seed", "-3", 400, "0 or higher"},
		{"good seed", "Seed", "0", 503, ""},
		// floats
		{"bad float", "SpawnRateScale", "fast", 400, "not a number"},
		{"negative float", "SpawnRateScale", "-0.1", 400, "0 or higher"},
		{"good float", "SpawnRateScale", "1.5", 503, ""},
		// bools (engine bool parsing = strconv.ParseBool)
		{"bad bool", "BuffsEnabled", "maybe", 400, "boolean"},
		{"good bool numeric", "BuffsEnabled", "1", 503, ""},
		{"good bool uppercase", "Enabled", "TRUE", 503, ""},
		{"bad bool yes", "Persist", "yes", 400, "boolean"},
		// enums (case-insensitive)
		{"bad emote mode", "EmoteMode", "loud", 400, "module, tag-only"},
		{"good emote mode mixed case", "EmoteMode", "Tag-Only", 503, ""},
		{"bad refinement", "PerRoomRefinement", "sometimes", 400, "occupied, all, off"},
		{"good refinement uppercase", "PerRoomRefinement", "ALL", 503, ""},
		// free text stays permissive
		{"free text", "ExcludeZonePatterns", "instance_*, arena_*", 503, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"key":%q,"value":%q}`, tc.key, tc.value)
			req := httptest.NewRequest("POST", "/x", strings.NewReader(body))
			status, success, data := m.handleAdminConfig(req)
			if status != tc.wantStatus {
				t.Fatalf("%s=%q: status %d, want %d (data: %v)", tc.key, tc.value, status, tc.wantStatus, data)
			}
			if tc.wantStatus == 400 {
				if success {
					t.Error("400 must not report success")
				}
				msg, _ := data.(string)
				if !strings.Contains(msg, tc.wantMsgPart) {
					t.Errorf("message %q should mention %q", msg, tc.wantMsgPart)
				}
			}
		})
	}
}

// TestConfigValidatorsNormalize: the value Validate returns is what gets
// persisted — canonical bools, lowercased enums, trimmed numbers/text.
func TestConfigValidatorsNormalize(t *testing.T) {
	cases := []struct{ key, in, want string }{
		{"BuffsEnabled", "1", "true"},
		{"Persist", "F", "false"},
		{"Enabled", "True", "true"},
		{"EmoteMode", "TAG-ONLY", "tag-only"},
		{"PerRoomRefinement", " Occupied ", "occupied"},
		{"TickEveryGameHours", " 6 ", "6"},
		{"SpawnRateScale", "1.50", "1.5"},
		{"ExcludeZonePatterns", "  instance_*  ", "instance_*"},
	}
	for _, tc := range cases {
		got, err := configKeyMeta[tc.key].Validate(tc.in)
		if err != nil {
			t.Errorf("%s=%q: unexpected error %v", tc.key, tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s=%q: normalized to %q, want %q", tc.key, tc.in, got, tc.want)
		}
	}
}

// TestConfigKindsAndOptions: every key carries the input kind the page needs;
// enums carry their choices; every writable key validates.
func TestConfigKindsAndOptions(t *testing.T) {
	wantKinds := map[string]string{
		"Enabled": "bool", "IncludeSecretExits": "bool", "RebuildGraphOnBoot": "bool",
		"BuffsEnabled": "bool", "Persist": "bool", "SeasonsEnabled": "bool",
		"Seed": "int", "TickEveryGameHours": "int", "MaxActiveFronts": "int",
		"EmoteEveryRounds": "int", "SpawnRateScale": "float",
		"EmoteMode": "enum", "PerRoomRefinement": "enum",
		"ExcludeZonePatterns": "text", "BuffOverrides.*": "text",
	}
	m := adminTestModule()
	for _, row := range m.configRows() {
		if row.Kind != wantKinds[row.Key] {
			t.Errorf("%s: kind %q, want %q", row.Key, row.Kind, wantKinds[row.Key])
		}
		if row.Kind == "enum" && len(row.Options) == 0 {
			t.Errorf("%s: enum row without options", row.Key)
		}
		if row.Kind != "enum" && len(row.Options) != 0 {
			t.Errorf("%s: non-enum row carries options %v", row.Key, row.Options)
		}
	}
	if got := strings.Join(configKeyMeta["PerRoomRefinement"].Options, ","); got != "occupied,all,off" {
		t.Errorf("refinement options: %q", got)
	}
	if got := strings.Join(configKeyMeta["EmoteMode"].Options, ","); got != "module,tag-only" {
		t.Errorf("emote mode options: %q", got)
	}
	for key, meta := range configKeyMeta {
		if !meta.ReadOnly && meta.Validate == nil {
			t.Errorf("writable key %s has no validator", key)
		}
	}
}

// TestAdminRebuildPublishesOnce guards the single-publish rule for the admin
// rebuild arm: nothing publishes before/inside rebuildGraph (snapshot pointer
// generation as proxy), and the arm's single tail publish carries the
// outcome-derived lastAdminAction.
func TestAdminRebuildPublishesOnce(t *testing.T) {
	m := adminTestModule()
	m.publishSnapshot()
	before := adminSnapshot.Load()

	var during *AdminSnapshot
	orig := rebuildGraphFn
	rebuildGraphFn = func(m *weatherModule) { during = adminSnapshot.Load() } // crawl stub: keep the graph
	defer func() { rebuildGraphFn = orig }()

	m.applyAdminAction(WeatherAdminAction{Action: "rebuild"})

	if during != before {
		t.Error("snapshot published before/inside rebuildGraph — attribution would be wrong")
	}
	after := adminSnapshot.Load()
	if after == before {
		t.Fatal("rebuild action did not publish a snapshot")
	}
	if after.LastAction != "graph rebuilt" {
		t.Errorf("lastAction = %q", after.LastAction)
	}

	// Failure path publishes too (rebuildGraph kept/lost the graph; arm reports).
	rebuildGraphFn = func(m *weatherModule) { m.graph = nil }
	m.applyAdminAction(WeatherAdminAction{Action: "rebuild"})
	failed := adminSnapshot.Load()
	if failed == after {
		t.Fatal("failed rebuild did not publish a snapshot")
	}
	if failed.LastAction != "graph rebuild failed (see server log)" {
		t.Errorf("failure lastAction = %q", failed.LastAction)
	}
}

func TestConfigRowsNewKeys(t *testing.T) {
	m := adminTestModule()
	rowOf := func(key string) AdminConfigRow {
		for _, r := range m.configRows() {
			if r.Key == key {
				return r
			}
		}
		t.Fatalf("row %s missing", key)
		return AdminConfigRow{}
	}

	bo := rowOf("BuffOverrides.*")
	if !bo.ReadOnly {
		t.Error("BuffOverrides.* row must be read-only")
	}
	if bo.Badge != "takes effect on reboot" {
		t.Errorf("BuffOverrides.* badge: %q", bo.Badge)
	}
	if bo.Value != "(none)" {
		t.Errorf("no overrides configured: value %v, want (none)", bo.Value)
	}
	m.cfg.BuffOverrides = map[string][]int{"storm": {59002}, "blizzard": {}}
	if v := rowOf("BuffOverrides.*").Value; v != "blizzard→[]; storm→[59002]" {
		t.Errorf("summary = %v", v)
	}

	ez := rowOf("ExcludeZonePatterns")
	if ez.ReadOnly {
		t.Error("ExcludeZonePatterns must stay editable")
	}
	if ez.Badge != "applies on next graph rebuild" {
		t.Errorf("ExcludeZonePatterns badge: %q", ez.Badge)
	}
	// Slice rendered as a fresh string — snapshot isolation (see configRows).
	if ez.Value != "instance_*,ephemeral_*" {
		t.Errorf("ExcludeZonePatterns value = %v", ez.Value)
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
