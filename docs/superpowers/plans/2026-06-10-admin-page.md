# Admin Page (AP1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **STATUS: AWAITING CALABE'S APPROVAL — do not execute until he signs off (SOP gate).**

**Goal:** A single `/admin/weather` page — config editing with honest live/reboot apply badges, an auto-refreshing status panel, and spawn/clear/rebuild actions — built on the engine's module admin-page mechanism with strict thread isolation (HTTP handlers never touch module state).

**Architecture:** Spec `docs/superpowers/specs/2026-06-10-admin-page-design.md`. Three one-way bridges: an atomic `AdminSnapshot` for reads, module-defined events for actions and config changes (executed on the game loop via the same paths as the in-game commands), and the engine's persisting `configs.SetVal` for writes. Implemented on the held S3 branch (`worktree-s3-seasons-prose`) — one merge ships seasons + admin page.

**Tech Stack:** Go 1.24; engine admin web mechanism (`plugin.Web.AdminPage`/`AdminAPIEndpoint`/`RegisterPermissions`, verified in `internal/plugins/webconfig.go`); vanilla JS page modeled on the engine's `_datafiles/html/admin/gametime.html`.

---

## Verified engine facts (2026-06-10)

1. `plugin.Web.AdminPage(name, slug, htmlFile, addToNav, navGroup, navParent, description, navParentDescription, dataFunc)` — htmlFile relative to `datafiles/html/admin/`; page at `/admin/<slug>`. `plugin.Web.AdminAPIEndpoint(method, slug, handler, permissionKey...)` — at `/admin/api/v1/<slug>`; handler `func(*http.Request) (status int, success bool, data any)`, wrapped in `APIResponse{Success, Data}`. `plugin.Web.RegisterPermissions(ModulePermission{Key, Description, Category})`.
2. Pages/endpoints/permissions are harvested by `plugins.Load()` in the same pre-`onLoad` loop as commands (`plugins.go:533-553`) — **register in `init()`**, the M3 rule.
3. `configs.SetVal` (behind `plugin.Config.Set`) holds the config lock, merges into the overrides map, **saves the override file to disk**, overlays in-memory, and fires engine callbacks (`configs.go:329-376`). It validates property paths against the engine's key lookups — whether `Modules.weather.*` passes is **V-1, proven in Task 1 before anything else builds**.
4. Permission prefix semantics (`userrecord.go:435`): granted `weather` satisfies `weather.write` — one grant covers the in-game commands and the page's write endpoints.
5. The module's threading invariant (mutex-free MainWorker state) is documented in root `context.md` — this plan must not break it.

## Design decisions (read before starting)

- **Handlers never touch `module` fields.** Reads go through `adminSnapshot.Load()` (an `atomic.Pointer[AdminSnapshot]` package-level var); writes/actions go through `events.AddToQueue`. The ONLY module state the handlers may touch is that pointer.
- **Snapshot refresh points:** end of `startSim`, end of `tick`, end of every admin-action/config-change application, and `loadOrBuildGraph` failure paths (so "sim idle" is visible). Building it is cheap (tens of zones).
- **Action results are asynchronous:** the handler answers "accepted"; the outcome lands in the next snapshot's `LastActionResult`. The page re-polls after firing an action.
- **The config apply table is data** (`map[string]configKeyMeta{Badge, Coerce, LiveApply}`) — one source of truth serialized into the snapshot so the page renders exactly what the module will do. Per-key semantics per spec §4.
- **HTML task uses the engine reference**: the implementer reads `gametime.html` for the styling/fetch/auth conventions at implementation time rather than this plan embedding 400 lines of HTML — the plan specifies the page's required structure, element behavior, and API contract precisely instead.
- Test commands as always: standalone suites untouched by most tasks; engine-coupled work syncs to the checkout (`pwsh scripts/sync-to-checkout.ps1 -Checkout "$HOME\workspace\GoMud"`) and tests `go test ./modules/weather/...`. Commits conventional with the `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` trailer; no pushes.

## File structure

| File | Responsibility |
|---|---|
| `weather_admin.go` (new) | `AdminSnapshot` types, the atomic pointer, `buildSnapshot`, the config apply table, action/config-change appliers. |
| `weather_admin_api.go` (new) | HTTP handlers (status/config/action) + `registerAdminWeb()` called from `init()`. |
| `weather_events.go` (modify) | `WeatherAdminAction`, `WeatherConfigChanged` event types. |
| `weather.go` (modify) | `init()` calls `registerAdminWeb()`; `onLoad` registers the two new listeners; snapshot refresh hooks. |
| `weather_tick.go` (modify) | Snapshot refresh at `startSim`/`tick` end. |
| `files/datafiles/html/admin/weather.html` (new) | The page. |
| `weather_admin_test.go` (new) | In-checkout tests: snapshot builder, apply-table dispatch, handler validation. |
| context.md (root) + `README.md` + spec status | Docs. |

---

## Task 1: V-1 — prove the config write round-trip

> **V-1 VERDICT: PASS (2026-06-10).** All four properties hold. Facts for the
> remaining tasks: `PluginConfig.Set(name string, val any)` is **void** (the
> Task 4 handler code is correct as written); the data-overlay keys populate
> the engine's `keyLookups` at `plugins.Load()`, so `Modules.weather.*` paths
> validate; the override file is **`_datafiles/world/default/config-overrides.yaml`**
> (NOT `_datafiles/config-overrides.yaml`) — Task 7's smoke check looks there.

Nothing else builds until `plugin.Config.Set` is proven to (a) accept `Modules.weather.*` keys, (b) be visible to `plugin.Config.Get` immediately, (c) persist to the override file, (d) survive a reboot.

**Files:** Temporary, removed at the end of this task: a `configtest` case in `weather_commands.go`. Permanent: a dated note in this plan file recording the verdict.

- [ ] **Step 1: Add a temporary admin subcommand** (in `cmdWeather`'s switch, before `default:`):

```go
	case "configtest": // TEMPORARY (plan AP1 Task 1) — removed after V-1 is proven
		if err := m.plug.Config.Set("SpawnRateScale", "1.5"); err != nil {
			sendLine(user, fmt.Sprintf("Config.Set error: %v", err))
			return true, nil
		}
		sendLine(user, fmt.Sprintf("Set OK; Get => %v", m.plug.Config.Get("SpawnRateScale")))
```

(Check `PluginConfig.Set`'s exact signature in `internal/plugins/pluginconfig.go` first — it takes `(name string, val any)`; adjust the error handling to match its actual return, which may be none — if it returns nothing, drop the `err` plumbing and just Set then Get.)

- [ ] **Step 2: Sync, build, boot the checkout server; run `weather configtest` as admin.** Record: the Get result; the contents of the engine's config override file (locate it: `configs.overridePath` — likely `_datafiles/config-overrides.yaml`) showing `Modules.weather.SpawnRateScale`; then reboot and confirm `weather status`/logs reflect 1.5 surviving (`buildConfig` reads it). Then set it back to `1.0` the same way.
- [ ] **Step 3: Decide.** If the round-trip works: record "V-1 PASS (date)" in this plan file under this task and proceed. If `Set` rejects the key: STOP, investigate how the engine's own admin config editor persists module keys (`internal/web/api_v1_yaml.go` and friends), record findings, and adapt §3.3 of the spec before continuing — this is a design-input gate, not a code-around.
- [ ] **Step 4: Remove the temporary subcommand.** Verify checkout build+tests green.
- [ ] **Step 5: Commit** (plan-note edit + the add/remove netting to zero code change):

```bash
git add docs/superpowers/plans/2026-06-10-admin-page.md
git commit -m "docs(plan): record V-1 config round-trip verdict"
```

## Task 2: Snapshot types and builder

**Files:** Create `weather_admin.go`, `weather_admin_test.go`. Modify `weather_tick.go`, `weather.go`.

- [ ] **Step 1: Write failing tests `weather_admin_test.go`** (in-checkout; fabricate module state directly — same posture as `weather_config_test.go`):

```go
package weather

import (
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
		cfg:      buildConfig(func(string) any { return nil }),
		graph:    g,
		simReady: true,
		seasonsOn: true,
		simCfg:   sim.DefaultConfig(),
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
```

- [ ] **Step 2: Sync + run (checkout): `go test ./modules/weather/ -run 'TestBuildSnapshot|TestSnapshot|TestPublish'`** — FAIL.

- [ ] **Step 3: Implement `weather_admin.go`** (types + builder + publish; the apply table arrives fully in Task 3 — here it only needs `Key`+`Badge`):

```go
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
	SimReady      bool                `json:"simReady"`
	SeasonsOn     bool                `json:"seasonsOn"`
	Round         uint64              `json:"round"`
	NextTickRound uint64              `json:"nextTickRound"`
	GraphZones    int                 `json:"graphZones"`
	GraphEdges    int                 `json:"graphEdges"`
	Components    int                 `json:"graphComponents"`
	Fronts        []AdminFrontRow     `json:"fronts"`
	Zones         []AdminZoneRow      `json:"zones"`
	Config        []AdminConfigRow    `json:"config"`
	LastAction    string              `json:"lastAction"`       // human-readable result of the most recent admin action
}

type AdminFrontRow struct {
	Id        uint64  `json:"id"`
	Type      string  `json:"type"`
	Zone      string  `json:"zone"`
	Intensity float64 `json:"intensity"`
	Moisture  float64 `json:"moisture"`
	Age       int     `json:"age"`
	MaxAge    int     `json:"maxAge"`
}

type AdminZoneRow struct {
	Zone    string `json:"zone"`
	Biome   string `json:"biome"`
	Weather string `json:"weather"`
	Track   string `json:"track,omitempty"`
	Season  string `json:"season,omitempty"`
}

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
```

Add to the `weatherModule` struct in `weather.go` (after `zoneSeasons`): `lastAdminAction string // most recent admin-page action result (snapshot field)`.

For this task, implement `configRows()` minimally in `weather_admin.go` (Task 3 replaces the badge source with the full apply table):

```go
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
```

- [ ] **Step 4: Refresh hooks.** `weather_tick.go`: add `m.publishSnapshot()` as the last line of `startSim` (after `m.simReady = true`) and of `tick`. `weather.go`: add it at the end of `rebuildGraph` and in the graph-nil failure branch of... (check `startSim`'s early-return: when `m.graph == nil` it warns and returns — add `m.publishSnapshot()` before that return so "idle" is visible).

- [ ] **Step 5: Sync + run (checkout): `go test ./modules/weather/...`** — PASS; build+vet green; standalone suites untouched/green; gofmt clean.

- [ ] **Step 6: Commit** `git add weather_admin.go weather_admin_test.go weather_tick.go weather.go` — `feat(weather): atomic admin snapshot of module state`

## Task 3: Config apply table + event appliers

**Files:** Modify `weather_admin.go`, `weather_admin_test.go`, `weather_events.go`, `weather.go`.

- [ ] **Step 1: Event types** (append to `weather_events.go`):

```go
// WeatherAdminAction is queued by the admin web API; executed on the game
// loop through the same paths as the in-game admin commands.
type WeatherAdminAction struct {
	Action    string  // "spawn" | "clear" | "rebuild"
	Weather   string  // spawn: weather type
	Zone      string  // spawn: required; clear: optional
	Intensity float64 // spawn: 0 => default
}

// Type implements events.Event.
func (WeatherAdminAction) Type() string { return `WeatherAdminAction` }

// WeatherConfigChanged is queued after the admin web API persists a config
// write; the module re-reads config on the game loop and applies live keys.
type WeatherConfigChanged struct {
	Key string
}

// Type implements events.Event.
func (WeatherConfigChanged) Type() string { return `WeatherConfigChanged` }
```

- [ ] **Step 2: Failing tests** (append to `weather_admin_test.go`):

```go
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
```

(NOTE for the implementer: `applyConfigChange`'s season-disable path calls `engine.ReconcileSeasons(m.graph, nil)` which touches the live room manager — in this unit test the graph zones don't exist in the room registry, so `GetZoneConfig` returns nil and the call is a no-op loop; that's why the test can run in-checkout without a booted world. State this in a comment in the test.)

- [ ] **Step 3: Implement in `weather_admin.go`:**

```go
// configKeyMeta is the single source of truth for what saving each key does.
// Badge text is shown on the page (via configRows); LiveApply (nil = nothing
// to do immediately) runs on the game loop after the new config is adopted.
type configKeyApplier struct {
	Badge     string
	LiveApply func(m *weatherModule, old Config)
}

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
		m.lastAdminAction = "graph rebuilt"
	default:
		m.lastAdminAction = "unknown action " + a.Action
	}
	m.publishSnapshot()
}
```

Update `configRows()` to read badges from `configKeyMeta` instead of literals (single source of truth):

```go
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
```

(The Task 2 test's exact-badge assertions remain valid via `TestConfigKeyMetaCoversAllKeys`; adjust Task 2's literal-badge expectations if any conflict — the meta table is now canonical.)

- [ ] **Step 4: Listeners.** In `weather.go` `onLoad` (after the `NewRound` registration):

```go
	events.RegisterListener(WeatherAdminAction{}, m.onAdminAction)
	events.RegisterListener(WeatherConfigChanged{}, m.onConfigChanged)
```

and the listener glue (in `weather_admin.go`):

```go
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
```

(`weather_admin.go` gains imports: `events`, `engine`, `sim` as needed.)

- [ ] **Step 5: Sync + run (checkout): `go test ./modules/weather/...`** — PASS; build/vet/gofmt green.

- [ ] **Step 6: Commit** — `feat(weather): admin action and config-change appliers on the game loop`

## Task 4: Web registration + API handlers

**Files:** Create `weather_admin_api.go`. Modify `weather.go` (`init()`), `weather_admin_test.go`.

- [ ] **Step 1: Failing handler tests** (append to `weather_admin_test.go`; handlers take `*http.Request` directly — build requests with `httptest.NewRequest`):

```go
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
```

(Imports: `net/http/httptest`, `strings`. The config-write test does NOT call `plug.Config.Set` — `m.plug` is nil in the fabricated module; see Step 3's nil-guard, and assert the 400 paths only. The happy write path is exercised by the smoke test against a live server.)

- [ ] **Step 2: Sync + run — FAIL.**

- [ ] **Step 3: Implement `weather_admin_api.go`:**

```go
package weather

import (
	"encoding/json"
	"net/http"

	"github.com/GoMudEngine/GoMud/internal/events"
	"github.com/GoMudEngine/GoMud/internal/plugins"
)

// registerAdminWeb wires the admin page, API endpoints, and permission key.
// Called from init(): plugins.Load() harvests web surface BEFORE onLoad (the
// same rule as commands — see context.md).
func (m *weatherModule) registerAdminWeb() {
	m.plug.Web.AdminPage(
		"Weather", "weather", "weather.html",
		true, "Modules", "",
		"Weather & seasons: config, status and actions",
		"", nil)
	m.plug.Web.AdminAPIEndpoint("GET", "weather/status", m.handleAdminStatus)
	m.plug.Web.AdminAPIEndpoint("POST", "weather/config", m.handleAdminConfig, "weather.write")
	m.plug.Web.AdminAPIEndpoint("POST", "weather/action", m.handleAdminAction, "weather.write")
	m.plug.Web.RegisterPermissions(plugins.ModulePermission{
		Key:         "weather.write",
		Description: "Modify weather module config and fire weather actions",
		Category:    "Modules",
	})
}

// handleAdminStatus returns the current snapshot. Runs on a web goroutine —
// reads ONLY the atomic snapshot, never module state.
func (m *weatherModule) handleAdminStatus(_ *http.Request) (int, bool, any) {
	return http.StatusOK, true, loadSnapshot()
}

// handleAdminConfig validates and persists one config write, then queues the
// change for the game loop. Web goroutine: touches only the engine config
// layer (internally locked) and the event queue (thread-safe).
func (m *weatherModule) handleAdminConfig(r *http.Request) (int, bool, any) {
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return http.StatusBadRequest, false, "malformed body"
	}
	meta, ok := configKeyMeta[body.Key]
	if !ok {
		return http.StatusBadRequest, false, "unknown config key"
	}
	if m.plug == nil { // fabricated test module; live servers always have a plugin
		return http.StatusServiceUnavailable, false, "plugin not initialised"
	}
	m.plug.Config.Set(body.Key, body.Value)
	events.AddToQueue(WeatherConfigChanged{Key: body.Key})
	return http.StatusOK, true, map[string]any{"key": body.Key, "badge": meta.Badge}
}

// handleAdminAction shape-validates and queues an action for the game loop.
func (m *weatherModule) handleAdminAction(r *http.Request) (int, bool, any) {
	var a WeatherAdminAction
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		return http.StatusBadRequest, false, "malformed body"
	}
	switch a.Action {
	case "spawn":
		if a.Zone == "" || a.Weather == "" {
			return http.StatusBadRequest, false, "spawn requires weather type and zone"
		}
	case "clear", "rebuild":
		// zone optional / none
	default:
		return http.StatusBadRequest, false, "unknown action"
	}
	events.AddToQueue(a)
	return http.StatusOK, true, "accepted — result appears in the next status refresh"
}
```

(Verify `PluginConfig.Set`'s signature/return during implementation — Task 1 already touched it; adjust the call accordingly.) In `weather.go` `init()`, after `module.registerExports()`: `module.registerAdminWeb()`.

- [ ] **Step 4: Sync + run (checkout): `go test ./modules/weather/...`** — PASS; build/vet/gofmt green.

- [ ] **Step 5: Commit** — `feat(weather): admin web page registration and API handlers`

## Task 5: The page

**Files:** Create `files/datafiles/html/admin/weather.html`.

- [ ] **Step 1: Read the reference.** `C:\Users\Calabe Davis\workspace\GoMud\_datafiles\html\admin\gametime.html` — copy its conventions exactly: page skeleton/CSS hooks, how it fetches `/admin/api/v1/...` (auth headers/cookies come free on the admin session), its status/error display idioms, form layout.

- [ ] **Step 2: Build `weather.html`** with three sections:
  1. **Status** — sim/seasons state line; fronts table (id/type/zone/intensity/moisture/age); zones table (zone/biome/weather/season); graph summary; `lastAction` line. `fetchStatus()` on load + every 5s (`setInterval`), rendering from the `GET weather/status` JSON (`APIResponse.Data`).
  2. **Config** — table built FROM THE SNAPSHOT's `config` rows (key, current value, input, badge chip, per-row Save). Save → `POST weather/config` `{key, value}` → on success show the returned badge as confirmation and re-fetch status; on 4xx show the error text.
  3. **Actions** — spawn form (type text input, zone text input, intensity number 0–1), clear (optional zone) with `confirm()`, rebuild with `confirm()`. Each → `POST weather/action` → show response message → re-fetch status after ~2s (async actions).
  No external JS/CSS dependencies beyond what gametime.html itself uses.

- [ ] **Step 3: Sync + checkout build; boot briefly; verify `/admin/weather` renders with live data** (full checklist is Task 7 — here just confirm the page loads and polls).

- [ ] **Step 4: Commit** — `feat(weather): admin page UI`

## Task 6: Documentation

**Files:** Modify root `context.md`, `README.md`, `docs/superpowers/specs/2026-06-10-admin-page-design.md` (status header), `engine/context.md` only if touched (it isn't).

- [ ] Root `context.md`: `weather_admin.go`/`weather_admin_api.go` entries — the snapshot bridge (atomic pointer, game-loop-only writes), the two events + listeners, the apply table as single source of truth, the init() web registration (harvest rule), and an explicit **threading note**: the MainWorker invariant now has one designed exception surface — HTTP handlers — which touch only the atomic snapshot, the engine config layer, and the event queue.
- [ ] `README.md`: an "Admin page" subsection under Using it (what it does, `/admin/weather`, the `weather.write` permission and the prefix relationship to `weather`, the live/reboot badge concept); add `weather.write` to the permission mentions in What-can-break #4/#10 only if accuracy requires.
- [ ] Spec status header → "Implemented (AP1, <date>)".
- [ ] **Commit** — `docs: document the admin page`

## Task 7: Verification + smoke

- [ ] **Step 1:** Full clean run (standalone suites + vet + gofmt; checkout generate/build/vet/test).
- [ ] **Step 2: Smoke (checkout server + browser-equivalent HTTP):** Boot with the web admin enabled (check `_datafiles/config.yaml` web/admin port config; the smoke can drive everything with authenticated HTTP if a browser is impractical — note which you used). Checklist:
  1. Nav shows Weather under Modules; `/admin/weather` renders; status polls (fronts/zones/seasons populated).
  2. Config: set `SpawnRateScale` to `0.5` → 200 + badge "live"; verify the override file gained `Modules.weather.SpawnRateScale`; verify next snapshot's config row shows 0.5; reboot → still 0.5 (the V-1 round-trip through the real page). Set back to 1.0.
  3. `SeasonsEnabled` false via the page → seasons strip live (zones table loses seasons; `weather seasons` in-game says off). Back to true → seasons return without reboot.
  4. Actions: spawn storm in a real zone → lastAction + fronts table update ≤ one poll; in-game `look` shows the mutator; clear → gone; rebuild → graph summary refreshes.
  5. Permissions: a non-admin (or mod without `weather.write`) POST gets refused; GET status on an admin session works.
  6. No data races observed: run the checkout server briefly with `-race` if the toolchain allows (`go run -race .`) while clicking through — if CGO is unavailable on this host, record that it's still blocked and rely on the design review.
- [ ] **Step 3:** Record results; fix-or-report deviations honestly.
- [ ] **Step 4: Commit** any smoke-driven doc corrections — `docs(spec): record admin page smoke results`
