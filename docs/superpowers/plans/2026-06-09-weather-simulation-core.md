# Weather Simulation Core (M2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the pure, deterministic weather simulation — traveling fronts that move across the zone graph, are shaped by the terrain they cross (the biome ⇄ weather feedback loop), spawn within a budget, and resolve to a per-zone weather state — all as a reproducible pure-Go core with no engine imports.

**Architecture:** Everything lives in the existing `sim/` package and consumes the `sim.Graph` from M1 as its read-only world. A single pure `Step(prev, graph, climate, cfg, clock) -> (next, diff)` function advances the simulation one coarse tick; all randomness flows through a small serializable PRNG stored in `State`, so a given seed + graph + tick-count reproduces exactly. Climate is module-owned Go data (`DefaultClimate()`); file-based overrides and mutator application are M3.

**Tech Stack:** Go 1.25, standard library only (no third-party deps; no engine imports). Tested standalone in this repo.

**Spec:** Implements §7 (Weather Simulation Core) of `docs/superpowers/specs/2026-06-08-weather-module-design.md`. Output `StateDiff` is what M3's engine layer applies via mutators; `State` JSON is what M3 persists via `plugin.WriteBytes`.

---

## Important notes for the executor

- **Pure & standalone.** This milestone touches ONLY `sim/`. Test with **`go test ./sim/...`** — NOT `go test ./...` (the repo's `engine/` and `weather` packages import `internal/*` and only compile inside a GoMud checkout). No server, no checkout, no network needed.
- The `sim` purity guardrail (`sim/arch_test.go`) stays green: add **no** `internal/*` imports and **no** third-party deps.
- After each task: `go test ./sim/...`, `gofmt -l sim` (empty), `go vet ./sim/...` (clean), commit.

## Design decisions (read before starting)

- **The Graph is the world view.** `Step` takes `*sim.Graph` directly (it already exposes `Nodes[zone].Biome`, `Nodes[zone].HasOutdoor`, and `Neighbors(zone)`). No separate `WorldView` interface (a deliberate simplification of spec §7.1 — the Graph already IS the pure world representation).
- **RNG in State.** The PRNG cursor is a `uint64` field of `State`; `Step` builds an `RNG` from it and writes the advanced cursor back into the next `State`. Same cursor → same sequence.
- **Type evolution is data-driven** (re-roll from the destination zone's climate weights, biased to keep the current type), not a hardcoded transition table.
- **Intensity-scaled area coverage.** A front travels as a single center token but *covers an area* when resolving weather: it projects onto zones within `MaxFrontRadius` graph-hops with `effective intensity = Intensity × CoverageFalloff^hops`, covering a zone only while that stays `>= MinProjected`. So a strong storm blankets a wide ring of zones at once while a weak one barely covers its own zone — exactly "stronger storms spread further." Per zone, the front with the highest *effective* intensity wins. This lives entirely in `resolveWeather`; front travel/lifecycle are unchanged.
- **Frontless zones resolve to `Clear`** in M2 (a calm baseline). The "occasional light fog/overcast in calm zones" enrichment from §7.5 step 6 is deferred (noted in `context.md`).
- **Prevailing-wind direction is a LATER chunk** (a MUD owner setting the general direction storms originate/move, e.g. west→east). It needs directional metadata on graph edges, which a future crawler pass can derive from each exit's `MapDirection`. M2 movement is edge-weight + resistance + no-backtrack only.

## File Structure

| File | Responsibility |
|---|---|
| `sim/rng.go` | `RNG` — deterministic, serializable splitmix64 PRNG (`Uint64`/`Float64`/`Intn`/`State`). |
| `sim/rng_test.go` | RNG determinism + range tests. |
| `sim/weather.go` | Core types: `WeatherType` (+ `Clear`), `ZoneId`, `FrontId`, `Front`, `ZoneChange`, `StateDiff`, `State`, `Clock`; `clamp01`. |
| `sim/weather_test.go` | `clamp01` test. |
| `sim/climate.go` | `WeatherInfluence`, `ClimateProfile`, `Climate`, `Climate.For`, `DefaultClimate`; `Config`, `DefaultConfig`. |
| `sim/climate_test.go` | Climate lookup/fallback + DefaultConfig tests. |
| `sim/graph.go` (modify) | Add `(*Graph) Zones() []string` (sorted) convenience accessor. |
| `sim/tick.go` | `Step` + helpers: `ageAndFeedback`, `moveFronts`, `evolveTypes`, `removeDead`, `spawnFronts`, `resolveWeather` (intensity-scaled area coverage), `zonesWithin` (BFS), `pow`, `diffWeather`, weighted-pick helpers. |
| `sim/tick_test.go` | Per-behavior tests + golden-trace + storm-over-mountain feedback. |
| `sim/state.go` | `State.ToJSON` / `StateFromJSON` (persistence codec, incl. RNG cursor). |
| `sim/state_test.go` | State JSON round-trip. |
| `sim/context.md` (modify) | Document the new simulation surface. |

---

## Task 1: Deterministic serializable RNG

**Files:** Create `sim/rng.go`, `sim/rng_test.go`.

- [ ] **Step 1: Write the failing test `sim/rng_test.go`**

```go
package sim

import "testing"

func TestRNGDeterministic(t *testing.T) {
	a := NewRNG(42)
	b := NewRNG(42)
	for i := 0; i < 100; i++ {
		if a.Uint64() != b.Uint64() {
			t.Fatalf("same seed must produce same sequence (i=%d)", i)
		}
	}
}

func TestRNGStateRoundTrips(t *testing.T) {
	a := NewRNG(7)
	for i := 0; i < 10; i++ {
		a.Uint64()
	}
	// Reconstruct from the cursor and confirm the continuation matches.
	b := NewRNG(a.State())
	if a.Uint64() != b.Uint64() {
		t.Fatal("RNG reconstructed from State() must continue the same sequence")
	}
}

func TestRNGFloatAndIntnRanges(t *testing.T) {
	r := NewRNG(1)
	for i := 0; i < 1000; i++ {
		f := r.Float64()
		if f < 0 || f >= 1 {
			t.Fatalf("Float64 out of [0,1): %v", f)
		}
		n := r.Intn(5)
		if n < 0 || n >= 5 {
			t.Fatalf("Intn(5) out of range: %d", n)
		}
	}
	if r.Intn(0) != 0 {
		t.Fatal("Intn(0) should return 0, not panic")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./sim/ -run TestRNG`
Expected: FAIL (`undefined: NewRNG`).

- [ ] **Step 3: Implement `sim/rng.go`**

```go
package sim

// RNG is a small, deterministic, serializable PRNG (splitmix64). Its entire
// state is a single uint64, so it round-trips trivially through State.
type RNG struct{ state uint64 }

// NewRNG creates a PRNG positioned at the given cursor (use a seed to start, or
// a value from State() to resume an exact sequence).
func NewRNG(seed uint64) *RNG { return &RNG{state: seed} }

// State returns the current cursor, for serialization.
func (r *RNG) State() uint64 { return r.state }

// Uint64 returns the next pseudo-random 64-bit value (splitmix64).
func (r *RNG) Uint64() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Float64 returns a value in [0, 1).
func (r *RNG) Float64() float64 {
	return float64(r.Uint64()>>11) / float64(uint64(1)<<53)
}

// Intn returns a value in [0, n); returns 0 for n <= 0.
func (r *RNG) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Uint64() % uint64(n))
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./sim/ -run TestRNG`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/rng.go sim/rng_test.go
git commit -m "feat(sim): deterministic serializable splitmix64 RNG"
```

---

## Task 2: Core simulation types

**Files:** Create `sim/weather.go`, `sim/weather_test.go`.

- [ ] **Step 1: Write the failing test `sim/weather_test.go`**

```go
package sim

import "testing"

func TestClamp01(t *testing.T) {
	cases := map[float64]float64{-0.5: 0, 0: 0, 0.5: 0.5, 1: 1, 1.5: 1}
	for in, want := range cases {
		if got := clamp01(in); got != want {
			t.Errorf("clamp01(%v) = %v, want %v", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./sim/ -run TestClamp01`
Expected: FAIL (`undefined: clamp01`).

- [ ] **Step 3: Implement `sim/weather.go`**

```go
package sim

// ZoneId names a zone (matches Graph node keys).
type ZoneId = string

// WeatherType is an open, data-driven weather label (climate profiles define
// which are valid per biome). Clear is the calm baseline for frontless zones.
type WeatherType string

const Clear WeatherType = "clear"

// FrontId uniquely identifies a weather front within a run.
type FrontId uint64

// Front is a discrete weather system with a location and a trajectory.
type Front struct {
	Id        FrontId     `json:"id"`
	Type      WeatherType `json:"type"`
	Zone      ZoneId      `json:"zone"`
	Intensity float64     `json:"intensity"` // 0..1; <=0 means death
	Moisture  float64     `json:"moisture"`  // 0..1
	Age       int         `json:"age"`       // ticks alive
	MaxAge    int         `json:"maxAge"`    // soft cap; older fronts decay faster
	History   []ZoneId    `json:"history"`   // recent path (bounded), newest last
}

// State is the full simulation state: the RNG cursor, the front-id counter,
// active fronts, and the resolved per-zone weather. It is serializable.
type State struct {
	Round    uint64                 `json:"round"`
	RNGState uint64                 `json:"rngState"`
	NextID   FrontId                `json:"nextId"`
	Fronts   []Front                `json:"fronts"`
	Weather  map[ZoneId]WeatherType `json:"weather"`
}

// Clock carries the current coarse tick (and, later, season). Step stamps the
// next State with it.
type Clock struct {
	Round uint64 `json:"round"`
}

// ZoneChange records one zone's weather transition for a tick.
type ZoneChange struct {
	Zone ZoneId      `json:"zone"`
	From WeatherType `json:"from"`
	To   WeatherType `json:"to"`
}

// StateDiff is the set of per-zone weather changes a Step produced — what the
// engine layer applies (and nothing more).
type StateDiff struct {
	Changes []ZoneChange `json:"changes"`
}

// clamp01 constrains x to [0, 1].
func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./sim/ -run TestClamp01`
Expected: PASS. Also `go test ./sim/...` to confirm the package still builds and the arch guardrail passes.

- [ ] **Step 5: Commit**

```bash
git add sim/weather.go sim/weather_test.go
git commit -m "feat(sim): core simulation types (Front, State, StateDiff, Clock)"
```

---

## Task 3: Climate model + simulation config

**Files:** Create `sim/climate.go`, `sim/climate_test.go`.

- [ ] **Step 1: Write the failing test `sim/climate_test.go`**

```go
package sim

import "testing"

func TestClimateForFallsBackToDefault(t *testing.T) {
	c := DefaultClimate()
	if _, ok := c["default"]; !ok {
		t.Fatal("DefaultClimate must include a 'default' profile")
	}
	// A biome with no profile returns the default profile.
	got := c.For("no-such-biome")
	if got.SpawnWeight != c["default"].SpawnWeight || len(got.Weather) != len(c["default"].Weather) {
		t.Error("For(unknown) should return the default profile")
	}
	// A known biome returns its own profile.
	if _, ok := c["tundra"]; ok {
		if _, hasSnow := c.For("tundra").Weather["snow"]; !hasSnow {
			t.Error("tundra profile should include snow")
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxActiveFronts <= 0 {
		t.Error("MaxActiveFronts must be positive")
	}
	if cfg.HistoryLen <= 0 {
		t.Error("HistoryLen must be positive")
	}
	if cfg.SpawnChance < 0 || cfg.SpawnChance > 1 {
		t.Error("SpawnChance must be in [0,1]")
	}
	if cfg.CoverageFalloff <= 0 || cfg.CoverageFalloff > 1 {
		t.Error("CoverageFalloff must be in (0,1]")
	}
	if cfg.MinProjected <= 0 || cfg.MinProjected > 1 {
		t.Error("MinProjected must be in (0,1]")
	}
	if cfg.MaxFrontRadius < 0 {
		t.Error("MaxFrontRadius must be >= 0")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./sim/ -run 'TestClimate|TestDefaultConfig'`
Expected: FAIL (`undefined: DefaultClimate`).

- [ ] **Step 3: Implement `sim/climate.go`**

```go
package sim

// WeatherInfluence is the terrain → front-dynamics half of the feedback loop:
// how a biome modifies a front passing through it each tick.
type WeatherInfluence struct {
	IntensityDelta     float64 `json:"intensityDelta"`
	MoistureDelta      float64 `json:"moistureDelta"`
	MovementResistance float64 `json:"movementResistance"` // 0..1; higher = lingers
}

// ClimateProfile is one biome's weather data: valid weather types + spawn
// weights (biome → weather), the influence it exerts on passing fronts
// (weather ← biome), and how often new fronts originate here.
type ClimateProfile struct {
	Weather     map[WeatherType]float64 `json:"weather"`
	Influence   WeatherInfluence        `json:"influence"`
	SpawnWeight float64                 `json:"spawnWeight"`
}

// Climate maps biome id -> profile. Use For() to resolve with default fallback.
type Climate map[string]ClimateProfile

// For returns the profile for a biome, or the "default" profile if the biome
// has none. If even "default" is absent, returns a zero profile.
func (c Climate) For(biome string) ClimateProfile {
	if p, ok := c[biome]; ok {
		return p
	}
	return c["default"]
}

// Config holds simulation tuning knobs.
type Config struct {
	MaxActiveFronts int     // global front budget
	SpawnChance     float64 // per-tick chance to spawn when under budget (0..1)
	HistoryLen      int     // bounded front path length (no-backtrack window)
	FrontHardAge    int     // hard age cap; older fronts die regardless

	// Area coverage: a front projects onto zones within MaxFrontRadius hops of
	// its center; the intensity it projects falls off by CoverageFalloff per hop,
	// and a zone is only covered while the projected value stays >= MinProjected.
	// Net effect: stronger fronts naturally cover a larger area.
	CoverageFalloff float64 // 0..1 multiplier per hop (e.g. 0.5 = halve each hop)
	MinProjected    float64 // minimum projected intensity for a zone to be covered
	MaxFrontRadius  int     // hard cap on coverage radius (hops)
}

// DefaultConfig returns sensible simulation defaults.
func DefaultConfig() Config {
	return Config{
		MaxActiveFronts: 8,
		SpawnChance:     0.25,
		HistoryLen:      4,
		FrontHardAge:    48,
		CoverageFalloff: 0.5,
		MinProjected:    0.15,
		MaxFrontRadius:  2,
	}
}

// DefaultClimate returns the built-in climate for the standard biomes. Builders
// can replace/extend this (file-based overrides land in M3). Influence sign
// convention: positive IntensityDelta feeds a front, negative saps it.
func DefaultClimate() Climate {
	return Climate{
		"default": {
			Weather:     map[WeatherType]float64{"clear": 6, "overcast": 3, "rain": 2, "fog": 1},
			Influence:   WeatherInfluence{IntensityDelta: -0.02, MoistureDelta: 0, MovementResistance: 0.1},
			SpawnWeight: 1.0,
		},
		"plains": {
			Weather:     map[WeatherType]float64{"clear": 5, "overcast": 3, "rain": 3, "storm": 2},
			Influence:   WeatherInfluence{IntensityDelta: 0.02, MoistureDelta: 0, MovementResistance: 0.05},
			SpawnWeight: 1.2,
		},
		"forest": {
			Weather:     map[WeatherType]float64{"clear": 4, "overcast": 4, "rain": 4, "fog": 3},
			Influence:   WeatherInfluence{IntensityDelta: -0.01, MoistureDelta: 0.02, MovementResistance: 0.15},
			SpawnWeight: 1.0,
		},
		"mountain": {
			Weather:     map[WeatherType]float64{"overcast": 4, "snow": 4, "storm": 2, "fog": 3},
			Influence:   WeatherInfluence{IntensityDelta: -0.15, MoistureDelta: -0.10, MovementResistance: 0.5},
			SpawnWeight: 0.8,
		},
		"desert": {
			Weather:     map[WeatherType]float64{"clear": 7, "dust": 3, "heatwave": 2},
			Influence:   WeatherInfluence{IntensityDelta: -0.05, MoistureDelta: -0.08, MovementResistance: 0.1},
			SpawnWeight: 0.7,
		},
		"tundra": {
			Weather:     map[WeatherType]float64{"clear": 5, "overcast": 4, "snow": 6, "blizzard": 2, "fog": 2},
			Influence:   WeatherInfluence{IntensityDelta: -0.05, MoistureDelta: -0.02, MovementResistance: 0.2},
			SpawnWeight: 1.0,
		},
		"swamp": {
			Weather:     map[WeatherType]float64{"overcast": 4, "rain": 5, "fog": 5, "storm": 2},
			Influence:   WeatherInfluence{IntensityDelta: 0.01, MoistureDelta: 0.05, MovementResistance: 0.2},
			SpawnWeight: 1.1,
		},
		"ocean": {
			Weather:     map[WeatherType]float64{"clear": 3, "overcast": 4, "rain": 4, "storm": 4, "fog": 2},
			Influence:   WeatherInfluence{IntensityDelta: 0.06, MoistureDelta: 0.08, MovementResistance: 0.02},
			SpawnWeight: 1.5,
		},
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./sim/ -run 'TestClimate|TestDefaultConfig'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/climate.go sim/climate_test.go
git commit -m "feat(sim): climate profiles + simulation config with built-in defaults"
```

---

## Task 4: `Graph.Zones()` accessor

**Files:** Modify `sim/graph.go`; add to `sim/graph_test.go`.

- [ ] **Step 1: Add the failing test to `sim/graph_test.go`**

```go
func TestGraphZones(t *testing.T) {
	g := &Graph{Nodes: map[string]ZoneNode{
		"B": {Zone: "B"}, "A": {Zone: "A"}, "C": {Zone: "C"},
	}}
	got := g.Zones()
	want := []string{"A", "B", "C"} // sorted for determinism
	if len(got) != len(want) {
		t.Fatalf("want %d zones, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Zones()[%d] = %q, want %q (must be sorted)", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./sim/ -run TestGraphZones`
Expected: FAIL (`g.Zones undefined`).

- [ ] **Step 3: Add `Zones` to `sim/graph.go`**

Add `"sort"` to the imports (the file currently imports only `"encoding/json"`), then add:

```go
// Zones returns all zone names in the graph, sorted for deterministic iteration.
func (g *Graph) Zones() []string {
	out := make([]string, 0, len(g.Nodes))
	for z := range g.Nodes {
		out = append(out, z)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./sim/ -run TestGraphZones`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/graph.go sim/graph_test.go
git commit -m "feat(sim): Graph.Zones() sorted accessor for the simulation"
```

---

## Task 5: `Step` skeleton — age, terrain feedback, death, resolve, diff

This task makes `Step` end-to-end (no movement/evolution/spawning yet): fronts age and are shaped by their current zone's influence, dead fronts are removed, weather resolves from surviving fronts with **intensity-scaled area coverage** (a front covers its zone and, the stronger it is, a wider ring of neighbors; frontless zones = `Clear`), and a diff is produced. Tests target the helpers directly so they remain valid as later tasks extend `Step`.

**Files:** Create `sim/tick.go`; create `sim/tick_test.go`.

- [ ] **Step 1: Write the failing test `sim/tick_test.go`**

```go
package sim

import "testing"

// twoZoneGraph: A(plains) <-> B(mountain), one edge weight 1.
func twoZoneGraph() *Graph {
	return &Graph{
		Nodes: map[string]ZoneNode{
			"A": {Zone: "A", Biome: "plains", Rooms: 3, HasOutdoor: true},
			"B": {Zone: "B", Biome: "mountain", Rooms: 3, HasOutdoor: true},
		},
		Edges: []Edge{{A: "A", B: "B", Weight: 1}},
	}
}

// These tests exercise the tick HELPERS directly (not the full Step), so they
// stay valid as later tasks add movement/spawning to Step.

func TestAgeAndFeedback_DrainsAndClamps(t *testing.T) {
	g := twoZoneGraph()
	// Front in mountain zone B (influence intensityDelta -0.15).
	fronts := []Front{{Id: 1, Type: "storm", Zone: "B", Intensity: 0.1, Moisture: 0.5, MaxAge: 24}}
	out := ageAndFeedback(fronts, g, DefaultClimate())
	if out[0].Intensity != 0 {
		t.Errorf("mountain feedback should drain 0.1 to 0 (clamped), got %v", out[0].Intensity)
	}
	if out[0].Age != 1 {
		t.Errorf("age should increment to 1, got %d", out[0].Age)
	}
}

func TestRemoveDead_DropsZeroIntensityAndAged(t *testing.T) {
	cfg := DefaultConfig()
	fronts := []Front{
		{Id: 1, Intensity: 0},                              // dead: no intensity
		{Id: 2, Intensity: 0.5, Age: cfg.FrontHardAge + 1}, // dead: too old
		{Id: 3, Intensity: 0.5, Age: 1},                    // alive
	}
	out := removeDead(fronts, cfg)
	if len(out) != 1 || out[0].Id != 3 {
		t.Errorf("only front 3 should survive, got %+v", out)
	}
}

func TestResolveWeather_AreaCoverageScalesWithIntensity(t *testing.T) {
	// Line A-B-C-D. Defaults: falloff 0.5, minProjected 0.15, maxRadius 2.
	g := &Graph{
		Nodes: map[string]ZoneNode{"A": {Zone: "A"}, "B": {Zone: "B"}, "C": {Zone: "C"}, "D": {Zone: "D"}},
		Edges: []Edge{{A: "A", B: "B", Weight: 1}, {A: "B", B: "C", Weight: 1}, {A: "C", B: "D", Weight: 1}},
	}
	cfg := DefaultConfig()

	// Strong storm at B (0.9): B=0.9, A&C=0.45, D=0.225 — all four covered (<= 2 hops).
	strong := resolveWeather(g, []Front{{Id: 1, Type: "storm", Zone: "B", Intensity: 0.9}}, cfg)
	for _, z := range []ZoneId{"A", "B", "C", "D"} {
		if strong[z] != "storm" {
			t.Errorf("strong storm should cover %s, got %q", z, strong[z])
		}
	}

	// Weak front at B (0.2): neighbors project 0.1 < 0.15 — only B covered.
	weak := resolveWeather(g, []Front{{Id: 1, Type: "fog", Zone: "B", Intensity: 0.2}}, cfg)
	if weak["B"] != "fog" {
		t.Errorf("weak front should hold B, got %q", weak["B"])
	}
	if weak["A"] != Clear || weak["C"] != Clear {
		t.Errorf("weak front (0.2) should NOT spread; A=%q C=%q", weak["A"], weak["C"])
	}
}

func TestResolveWeather_StrongerEffectiveIntensityWins(t *testing.T) {
	g := &Graph{Nodes: map[string]ZoneNode{"A": {Zone: "A"}, "B": {Zone: "B"}}, Edges: []Edge{{A: "A", B: "B", Weight: 1}}}
	cfg := DefaultConfig()
	// storm centered at A (0.9 -> projects 0.45 onto B); fog local at B (0.5).
	fronts := []Front{{Id: 1, Type: "storm", Zone: "A", Intensity: 0.9}, {Id: 2, Type: "fog", Zone: "B", Intensity: 0.5}}
	w := resolveWeather(g, fronts, cfg)
	if w["A"] != "storm" {
		t.Errorf("A should be storm (0.9), got %q", w["A"])
	}
	if w["B"] != "fog" {
		t.Errorf("B: local fog 0.5 should beat storm's projected 0.45, got %q", w["B"])
	}
}

func TestResolveWeather_FrontlessZonesAreClear(t *testing.T) {
	g := twoZoneGraph()
	w := resolveWeather(g, nil, DefaultConfig())
	if w["A"] != Clear || w["B"] != Clear {
		t.Errorf("no fronts -> all zones Clear, got %+v", w)
	}
}

func TestDiffWeather_ReportsOnlyChanges(t *testing.T) {
	prev := map[ZoneId]WeatherType{"A": Clear, "B": "rain"}
	next := map[ZoneId]WeatherType{"A": "storm", "B": "rain"}
	diff := diffWeather(prev, next)
	if len(diff.Changes) != 1 || diff.Changes[0].Zone != "A" ||
		diff.Changes[0].From != Clear || diff.Changes[0].To != "storm" {
		t.Errorf("expected one A: clear->storm change, got %+v", diff.Changes)
	}
}

func TestStep_DeterministicAndCoversAllZones(t *testing.T) {
	g := twoZoneGraph()
	cfg := DefaultConfig()
	st := State{RNGState: 7, NextID: 1, Weather: map[ZoneId]WeatherType{}}
	a, _ := Step(st, g, DefaultClimate(), cfg, Clock{Round: 1})
	b, _ := Step(st, g, DefaultClimate(), cfg, Clock{Round: 1})
	if a.RNGState != b.RNGState || len(a.Fronts) != len(b.Fronts) {
		t.Fatal("Step must be deterministic for identical inputs")
	}
	for _, z := range g.Zones() {
		if _, ok := a.Weather[z]; !ok {
			t.Errorf("zone %q missing from resolved weather", z)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./sim/`
Expected: FAIL — `undefined: Step` (and the tick helpers `ageAndFeedback`, `resolveWeather`, etc.).

- [ ] **Step 3: Implement `sim/tick.go`**

```go
package sim

import "sort"

// Step advances the simulation one coarse tick. It is a pure function of its
// inputs: all randomness comes from prev.RNGState. It returns the next State
// (fronts advanced, weather resolved, RNG cursor written back) and the StateDiff
// of per-zone weather changes the engine layer should apply.
func Step(prev State, g *Graph, climate Climate, cfg Config, now Clock) (State, StateDiff) {
	rng := NewRNG(prev.RNGState)

	fronts := cloneFronts(prev.Fronts)
	fronts = ageAndFeedback(fronts, g, climate)
	fronts = moveFronts(fronts, g, climate, cfg, rng)
	fronts = evolveTypes(fronts, g, climate, rng)
	fronts = removeDead(fronts, cfg)

	nextID := prev.NextID
	fronts, nextID = spawnFronts(fronts, g, climate, cfg, rng, nextID)

	weather := resolveWeather(g, fronts, cfg)
	diff := diffWeather(prev.Weather, weather)

	return State{
		Round:    now.Round,
		RNGState: rng.State(),
		NextID:   nextID,
		Fronts:   fronts,
		Weather:  weather,
	}, diff
}

func cloneFronts(in []Front) []Front {
	out := make([]Front, len(in))
	copy(out, in)
	for i := range out {
		out[i].History = append([]ZoneId(nil), in[i].History...)
	}
	return out
}

// ageAndFeedback ages each front and applies the influence of the biome of the
// zone it currently occupies (the weather <- biome half of the feedback loop),
// plus an age-based decay once past MaxAge. Intensity/Moisture are clamped.
func ageAndFeedback(fronts []Front, g *Graph, climate Climate) []Front {
	for i := range fronts {
		f := &fronts[i]
		f.Age++
		inf := climate.For(g.Nodes[f.Zone].Biome).Influence
		f.Intensity += inf.IntensityDelta
		f.Moisture += inf.MoistureDelta
		if f.MaxAge > 0 && f.Age > f.MaxAge {
			f.Intensity -= 0.05 * float64(f.Age-f.MaxAge)
		}
		f.Intensity = clamp01(f.Intensity)
		f.Moisture = clamp01(f.Moisture)
	}
	return fronts
}

// removeDead drops fronts whose intensity has reached zero or that exceed the
// hard age cap.
func removeDead(fronts []Front, cfg Config) []Front {
	out := fronts[:0]
	for _, f := range fronts {
		if f.Intensity <= 0 {
			continue
		}
		if cfg.FrontHardAge > 0 && f.Age > cfg.FrontHardAge {
			continue
		}
		out = append(out, f)
	}
	return out
}

// resolveWeather computes per-zone weather with intensity-scaled area coverage:
// each front projects onto zones within cfg.MaxFrontRadius hops of its center,
// with projected intensity = Intensity * CoverageFalloff^hops; a zone is covered
// only while that stays >= cfg.MinProjected. Per zone, the front projecting the
// highest effective intensity wins (deterministic tie-break by lowest FrontId).
// Zones no front reaches are Clear. Stronger fronts naturally cover more zones.
func resolveWeather(g *Graph, fronts []Front, cfg Config) map[ZoneId]WeatherType {
	type claim struct {
		eff   float64
		id    FrontId
		wtype WeatherType
	}
	best := map[ZoneId]claim{}
	for i := range fronts {
		f := &fronts[i]
		for zone, hops := range zonesWithin(g, f.Zone, cfg.MaxFrontRadius) {
			eff := f.Intensity * pow(cfg.CoverageFalloff, hops)
			if eff < cfg.MinProjected {
				continue
			}
			cur, ok := best[zone]
			if !ok || eff > cur.eff || (eff == cur.eff && f.Id < cur.id) {
				best[zone] = claim{eff: eff, id: f.Id, wtype: f.Type}
			}
		}
	}
	out := make(map[ZoneId]WeatherType, len(g.Nodes))
	for _, z := range g.Zones() {
		if c, ok := best[z]; ok {
			out[z] = c.wtype
		} else {
			out[z] = Clear
		}
	}
	return out
}

// zonesWithin returns each zone reachable from center within maxRadius hops,
// mapped to its hop-distance (center is 0). Deterministic shortest-path BFS;
// its result is order-independent, so resolveWeather stays deterministic.
func zonesWithin(g *Graph, center ZoneId, maxRadius int) map[ZoneId]int {
	dist := map[ZoneId]int{center: 0}
	frontier := []ZoneId{center}
	for d := 0; d < maxRadius; d++ {
		var next []ZoneId
		for _, z := range frontier {
			for _, e := range g.Neighbors(z) {
				if _, seen := dist[e.B]; !seen {
					dist[e.B] = d + 1
					next = append(next, e.B)
				}
			}
		}
		frontier = next
	}
	return dist
}

// pow raises base to a non-negative integer power (small exponents; avoids
// importing math).
func pow(base float64, exp int) float64 {
	r := 1.0
	for i := 0; i < exp; i++ {
		r *= base
	}
	return r
}

// diffWeather returns the zones whose weather changed from prev to next, sorted
// by zone for deterministic output.
func diffWeather(prev, next map[ZoneId]WeatherType) StateDiff {
	zones := make([]ZoneId, 0, len(next))
	for z := range next {
		zones = append(zones, z)
	}
	sort.Strings(zones)

	var changes []ZoneChange
	for _, z := range zones {
		from := prev[z] // "" if previously unknown
		if next[z] != from {
			changes = append(changes, ZoneChange{Zone: z, From: from, To: next[z]})
		}
	}
	return StateDiff{Changes: changes}
}
```

- [ ] **Step 4: Add temporary stubs so the package compiles**

`Step` references `moveFronts`, `evolveTypes`, and `spawnFronts`, implemented in Tasks 6–8. Add these stubs to `sim/tick.go` now so the package compiles; later tasks REPLACE each stub with the real implementation:

```go
// --- stubs replaced in later tasks ---

func moveFronts(fronts []Front, g *Graph, climate Climate, cfg Config, rng *RNG) []Front {
	return fronts
}

func evolveTypes(fronts []Front, g *Graph, climate Climate, rng *RNG) []Front {
	return fronts
}

func spawnFronts(fronts []Front, g *Graph, climate Climate, cfg Config, rng *RNG, nextID FrontId) ([]Front, FrontId) {
	return fronts, nextID
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./sim/`
Expected: PASS (the helper tests + the Step determinism/coverage test). Also `go test ./sim/...` (all sim tests + arch guardrail), `gofmt -l sim` (empty), `go vet ./sim/...` (clean).

- [ ] **Step 6: Commit**

```bash
git add sim/tick.go sim/tick_test.go
git commit -m "feat(sim): Step skeleton — age/feedback, death, resolve, diff (movement/spawn stubbed)"
```

---

## Task 6: Front movement

Replace the `moveFronts` stub with movement along graph edges, weighted by edge weight and damped by the destination/current biome's `MovementResistance`, avoiding immediate backtracking via `History`.

**Files:** Modify `sim/tick.go` (replace `moveFronts`); add to `sim/tick_test.go`.

- [ ] **Step 1: Add the failing test to `sim/tick_test.go`**

```go
func TestMoveFronts_HighResistanceZoneLingers(t *testing.T) {
	// Mountain (resistance 0.5) vs plains (0.05). Run many independent single
	// fronts one tick each; the mountain front should move far less often.
	g := twoZoneGraph()
	climate := DefaultClimate()
	cfg := DefaultConfig()

	movesFrom := func(zone ZoneId, seed uint64) int {
		moved := 0
		for i := 0; i < 200; i++ {
			rng := NewRNG(seed + uint64(i))
			f := []Front{{Id: 1, Type: "storm", Zone: zone, Intensity: 0.8, Moisture: 0.5, MaxAge: 24}}
			out := moveFronts(f, g, climate, cfg, rng)
			if out[0].Zone != zone {
				moved++
			}
		}
		return moved
	}
	mountainMoves := movesFrom("B", 1000)
	plainsMoves := movesFrom("A", 1000)
	if !(plainsMoves > mountainMoves) {
		t.Errorf("plains front should move more often than mountain front; plains=%d mountain=%d",
			plainsMoves, mountainMoves)
	}
}

func TestMoveFronts_RecordsHistoryOnMove(t *testing.T) {
	// A 3-zone line A-B-C; a front in B that moves should append its old zone to
	// History. Force movement by using zero resistance everywhere via a custom climate.
	g := &Graph{
		Nodes: map[string]ZoneNode{
			"A": {Zone: "A", Biome: "flat"}, "B": {Zone: "B", Biome: "flat"}, "C": {Zone: "C", Biome: "flat"},
		},
		Edges: []Edge{{A: "A", B: "B", Weight: 1}, {A: "B", B: "C", Weight: 1}},
	}
	climate := Climate{"flat": {Weather: map[WeatherType]float64{"clear": 1}, Influence: WeatherInfluence{MovementResistance: 0}, SpawnWeight: 1}, "default": {Weather: map[WeatherType]float64{"clear": 1}}}
	cfg := DefaultConfig()

	movedSomewhere := false
	for i := 0; i < 50; i++ {
		rng := NewRNG(uint64(i) + 1)
		f := []Front{{Id: 1, Type: "storm", Zone: "B", Intensity: 0.8, MaxAge: 24}}
		out := moveFronts(f, g, climate, cfg, rng)
		if out[0].Zone != "B" {
			movedSomewhere = true
			if len(out[0].History) == 0 || out[0].History[len(out[0].History)-1] != "B" {
				t.Fatalf("on move, History should end with the departed zone 'B'; got %v", out[0].History)
			}
		}
	}
	if !movedSomewhere {
		t.Fatal("with zero resistance, the front should sometimes move")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./sim/ -run TestMoveFronts`
Expected: FAIL — the stub never moves fronts, so both tests fail (no moves recorded).

- [ ] **Step 3: Replace the `moveFronts` stub in `sim/tick.go`**

```go
// moveFronts may advance each front to an adjacent zone. The chance to move is
// (1 - currentResistance); the destination is chosen weighted by edge weight,
// excluding the most recent zone in History (no immediate backtrack) when an
// alternative exists. On a move, the departed zone is appended to History
// (bounded by cfg.HistoryLen).
func moveFronts(fronts []Front, g *Graph, climate Climate, cfg Config, rng *RNG) []Front {
	for i := range fronts {
		f := &fronts[i]
		resistance := climate.For(g.Nodes[f.Zone].Biome).Influence.MovementResistance
		if rng.Float64() < resistance {
			continue // lingers
		}
		neighbors := g.Neighbors(f.Zone)
		if len(neighbors) == 0 {
			continue
		}
		dest := pickNeighbor(neighbors, lastZone(f.History), rng)
		if dest == "" || dest == f.Zone {
			continue
		}
		f.History = appendBounded(f.History, f.Zone, cfg.HistoryLen)
		f.Zone = dest
	}
	return fronts
}

// pickNeighbor selects a destination weighted by edge weight, excluding `avoid`
// (the last zone) unless it is the only option.
func pickNeighbor(neighbors []Edge, avoid ZoneId, rng *RNG) ZoneId {
	candidates := neighbors
	if avoid != "" {
		filtered := make([]Edge, 0, len(neighbors))
		for _, e := range neighbors {
			if e.B != avoid {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) > 0 {
			candidates = filtered
		}
	}
	total := 0
	for _, e := range candidates {
		w := e.Weight
		if w < 1 {
			w = 1
		}
		total += w
	}
	if total <= 0 {
		return ""
	}
	r := rng.Intn(total)
	for _, e := range candidates {
		w := e.Weight
		if w < 1 {
			w = 1
		}
		if r < w {
			return e.B
		}
		r -= w
	}
	return candidates[len(candidates)-1].B
}

func lastZone(history []ZoneId) ZoneId {
	if len(history) == 0 {
		return ""
	}
	return history[len(history)-1]
}

func appendBounded(history []ZoneId, z ZoneId, max int) []ZoneId {
	history = append(history, z)
	if max > 0 && len(history) > max {
		history = history[len(history)-max:]
	}
	return history
}
```

(`g.Neighbors(zone)` returns edges oriented with `.B` = the neighbor — see M1's `Graph.Neighbors`.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./sim/`
Expected: PASS (the new movement tests pass; all Task-5 helper/Step tests still pass).

- [ ] **Step 5: Commit**

```bash
git add sim/tick.go sim/tick_test.go
git commit -m "feat(sim): front movement (edge-weighted, resistance-damped, no backtrack)"
```

---

## Task 7: Type evolution

Replace the `evolveTypes` stub: when a front has just entered a new zone (its current zone differs from where it was — i.e., it moved this tick), with some probability re-roll its weather type from the new zone's climate weights, biased to keep its current type when that type is valid there.

**Files:** Modify `sim/tick.go` (replace `evolveTypes`, add `pickWeatherType`); add to `sim/tick_test.go`.

- [ ] **Step 1: Add the failing test to `sim/tick_test.go`**

```go
func TestEvolveTypes_AdaptsToNewBiome(t *testing.T) {
	// A front that just moved INTO tundra (history shows it came from elsewhere)
	// should, over many seeds, sometimes become a tundra-typical type (snow).
	g := &Graph{Nodes: map[string]ZoneNode{
		"T": {Zone: "T", Biome: "tundra"}, "P": {Zone: "P", Biome: "plains"},
	}}
	climate := DefaultClimate()
	becameSnowy := 0
	for i := 0; i < 300; i++ {
		rng := NewRNG(uint64(i) + 1)
		// Type "storm" entering tundra (storm is not in tundra's profile).
		f := []Front{{Id: 1, Type: "storm", Zone: "T", Intensity: 0.8, History: []ZoneId{"P"}}}
		out := evolveTypes(f, g, climate, rng)
		if out[0].Type == "snow" || out[0].Type == "blizzard" {
			becameSnowy++
		}
	}
	if becameSnowy == 0 {
		t.Error("a storm entering tundra should sometimes evolve toward snow/blizzard")
	}
}

func TestEvolveTypes_StationaryFrontKeepsType(t *testing.T) {
	// A front whose current zone == last history entry did NOT move this tick;
	// it should keep its type (no re-roll).
	g := &Graph{Nodes: map[string]ZoneNode{"T": {Zone: "T", Biome: "tundra"}}}
	for i := 0; i < 50; i++ {
		rng := NewRNG(uint64(i) + 1)
		f := []Front{{Id: 1, Type: "storm", Zone: "T", Intensity: 0.8, History: []ZoneId{"T"}}}
		out := evolveTypes(f, g, DefaultClimate(), rng)
		if out[0].Type != "storm" {
			t.Fatalf("stationary front should keep its type, got %q", out[0].Type)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./sim/ -run TestEvolveTypes`
Expected: FAIL — the stub never changes types, so `TestEvolveTypes_AdaptsToNewBiome` fails.

- [ ] **Step 3: Replace the `evolveTypes` stub in `sim/tick.go`**

```go
// evolveTypes re-rolls the weather type of fronts that moved this tick (current
// zone != last History entry), drawing from the new zone's climate weights. The
// current type gets a bias bonus so changes are gradual, not jarring.
func evolveTypes(fronts []Front, g *Graph, climate Climate, rng *RNG) []Front {
	const keepBias = 3.0 // extra weight on the front's current type if valid here
	for i := range fronts {
		f := &fronts[i]
		if f.Zone == lastZone(f.History) {
			continue // did not move this tick
		}
		profile := climate.For(g.Nodes[f.Zone].Biome)
		if len(profile.Weather) == 0 {
			continue
		}
		f.Type = pickWeatherType(profile, f.Type, keepBias, rng)
	}
	return fronts
}

// pickWeatherType chooses a weather type from a profile's weights, adding
// keepBias to `current` if it appears in the profile. Iteration is over a sorted
// key list so selection is deterministic for a given RNG sequence.
func pickWeatherType(profile ClimateProfile, current WeatherType, keepBias float64, rng *RNG) WeatherType {
	keys := make([]WeatherType, 0, len(profile.Weather))
	for k := range profile.Weather {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool { return keys[a] < keys[b] })

	total := 0.0
	for _, k := range keys {
		w := profile.Weather[k]
		if k == current {
			w += keepBias
		}
		total += w
	}
	if total <= 0 {
		return current
	}
	r := rng.Float64() * total
	for _, k := range keys {
		w := profile.Weather[k]
		if k == current {
			w += keepBias
		}
		if r < w {
			return k
		}
		r -= w
	}
	return keys[len(keys)-1]
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./sim/ -run 'TestEvolveTypes|TestStep_|TestMoveFronts'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/tick.go sim/tick_test.go
git commit -m "feat(sim): data-driven front type evolution on zone entry"
```

---

## Task 8: Front spawning + budget

Replace the `spawnFronts` stub: if under `MaxActiveFronts`, with probability `SpawnChance`, spawn one new front in a zone chosen weighted by each zone's climate `SpawnWeight`, typed from that zone's climate.

**Files:** Modify `sim/tick.go` (replace `spawnFronts`); add to `sim/tick_test.go`.

- [ ] **Step 1: Add the failing test to `sim/tick_test.go`**

```go
func TestSpawnFronts_RespectsBudget(t *testing.T) {
	g := twoZoneGraph()
	climate := DefaultClimate()
	cfg := DefaultConfig()
	cfg.MaxActiveFronts = 2
	cfg.SpawnChance = 1.0 // always try to spawn

	st := State{RNGState: 1, NextID: 1, Weather: map[ZoneId]WeatherType{}}
	// Run many ticks; fronts accumulate but never exceed the budget.
	for i := 0; i < 50; i++ {
		var diff StateDiff
		st, diff = Step(st, g, climate, cfg, Clock{Round: uint64(i + 1)})
		_ = diff
		if len(st.Fronts) > cfg.MaxActiveFronts {
			t.Fatalf("front count %d exceeded budget %d at tick %d", len(st.Fronts), cfg.MaxActiveFronts, i)
		}
	}
}

func TestSpawnFronts_AssignsIncrementingIDs(t *testing.T) {
	g := twoZoneGraph()
	cfg := DefaultConfig()
	cfg.SpawnChance = 1.0
	rng := NewRNG(5)
	fronts, nextID := spawnFronts(nil, g, DefaultClimate(), cfg, rng, FrontId(10))
	if len(fronts) != 1 {
		t.Fatalf("expected one spawned front, got %d", len(fronts))
	}
	if fronts[0].Id != 10 {
		t.Errorf("spawned front should take nextID 10, got %d", fronts[0].Id)
	}
	if nextID != 11 {
		t.Errorf("nextID should advance to 11, got %d", nextID)
	}
	if fronts[0].Intensity <= 0 {
		t.Error("spawned front should start with positive intensity")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./sim/ -run TestSpawnFronts`
Expected: FAIL — the stub never spawns (`TestSpawnFronts_AssignsIncrementingIDs` gets 0 fronts).

- [ ] **Step 3: Replace the `spawnFronts` stub in `sim/tick.go`**

```go
// spawnFronts may add one new front when under budget. It draws once on
// SpawnChance, then picks an origin zone weighted by climate SpawnWeight and a
// type from that zone's climate. New fronts start at moderate intensity/moisture.
func spawnFronts(fronts []Front, g *Graph, climate Climate, cfg Config, rng *RNG, nextID FrontId) ([]Front, FrontId) {
	if len(fronts) >= cfg.MaxActiveFronts {
		return fronts, nextID
	}
	if rng.Float64() >= cfg.SpawnChance {
		return fronts, nextID
	}
	origin := pickSpawnZone(g, climate, rng)
	if origin == "" {
		return fronts, nextID
	}
	profile := climate.For(g.Nodes[origin].Biome)
	wtype := pickWeatherType(profile, "", 0, rng)
	if wtype == "" {
		return fronts, nextID
	}
	f := Front{
		Id:        nextID,
		Type:      wtype,
		Zone:      origin,
		Intensity: 0.4 + 0.3*rng.Float64(), // 0.4..0.7
		Moisture:  0.5,
		Age:       0,
		MaxAge:    12 + rng.Intn(24), // 12..35 ticks
		History:   nil,
	}
	return append(fronts, f), nextID + 1
}

// pickSpawnZone selects an origin zone weighted by climate SpawnWeight (zones
// iterated in sorted order for determinism).
func pickSpawnZone(g *Graph, climate Climate, rng *RNG) ZoneId {
	zones := g.Zones()
	total := 0.0
	for _, z := range zones {
		w := climate.For(g.Nodes[z].Biome).SpawnWeight
		if w > 0 {
			total += w
		}
	}
	if total <= 0 {
		return ""
	}
	r := rng.Float64() * total
	for _, z := range zones {
		w := climate.For(g.Nodes[z].Biome).SpawnWeight
		if w <= 0 {
			continue
		}
		if r < w {
			return z
		}
		r -= w
	}
	return zones[len(zones)-1]
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./sim/ -run 'TestSpawnFronts|TestStep_'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/tick.go sim/tick_test.go
git commit -m "feat(sim): budgeted front spawning weighted by climate SpawnWeight"
```

---

## Task 9: Determinism golden-trace + storm-over-mountain feedback

Two characterization tests over the full `Step`: reproducibility, and the headline feedback behavior (a storm crossing mountains weakens and dies).

**Files:** Add to `sim/tick_test.go`.

- [ ] **Step 1: Add the tests to `sim/tick_test.go`**

```go
func runTicks(seed uint64, n int) State {
	g := twoZoneGraph()
	climate := DefaultClimate()
	cfg := DefaultConfig()
	st := State{RNGState: seed, NextID: 1, Weather: map[ZoneId]WeatherType{}}
	for i := 0; i < n; i++ {
		st, _ = Step(st, g, climate, cfg, Clock{Round: uint64(i + 1)})
	}
	return st
}

func TestStep_Deterministic(t *testing.T) {
	a := runTicks(12345, 40)
	b := runTicks(12345, 40)
	// Same seed + world + tick count => identical resolved weather and RNG cursor.
	if a.RNGState != b.RNGState {
		t.Fatal("RNG cursor diverged across identical runs")
	}
	if len(a.Weather) != len(b.Weather) {
		t.Fatalf("weather map sizes differ: %d vs %d", len(a.Weather), len(b.Weather))
	}
	for z, w := range a.Weather {
		if b.Weather[z] != w {
			t.Errorf("zone %q weather diverged: %q vs %q", z, w, b.Weather[z])
		}
	}
	if len(a.Fronts) != len(b.Fronts) {
		t.Errorf("front counts diverged: %d vs %d", len(a.Fronts), len(b.Fronts))
	}
}

func TestStep_StormDiesCrossingMountains(t *testing.T) {
	// A long mountain chain. A strong storm seeded at one end must lose intensity
	// as it crosses and eventually die — terrain is not a passive recipient.
	nodes := map[string]ZoneNode{}
	var edges []Edge
	const N = 6
	prev := ""
	for i := 0; i < N; i++ {
		z := string(rune('A' + i))
		nodes[z] = ZoneNode{Zone: z, Biome: "mountain", HasOutdoor: true}
		if prev != "" {
			edges = append(edges, Edge{A: prev, B: z, Weight: 1})
		}
		prev = z
	}
	g := &Graph{Nodes: nodes, Edges: edges}
	climate := DefaultClimate()
	cfg := DefaultConfig()
	cfg.SpawnChance = 0 // isolate the seeded storm

	st := State{
		RNGState: 99, NextID: 2,
		Fronts:  []Front{{Id: 1, Type: "storm", Zone: "A", Intensity: 0.9, Moisture: 0.9, MaxAge: 100}},
		Weather: map[ZoneId]WeatherType{},
	}
	start := st.Fronts[0].Intensity
	died := false
	minSeen := start
	for i := 0; i < 40; i++ {
		st, _ = Step(st, g, climate, cfg, Clock{Round: uint64(i + 1)})
		if len(st.Fronts) == 0 {
			died = true
			break
		}
		if st.Fronts[0].Intensity < minSeen {
			minSeen = st.Fronts[0].Intensity
		}
	}
	if !died {
		t.Fatalf("storm should die crossing the mountains; final intensity %v (min seen %v)",
			lastIntensity(st), minSeen)
	}
}

func lastIntensity(st State) float64 {
	if len(st.Fronts) == 0 {
		return 0
	}
	return st.Fronts[0].Intensity
}
```

- [ ] **Step 2: Run to verify they pass**

Run: `go test ./sim/ -run TestStep_`
Expected: PASS. If `TestStep_StormDiesCrossingMountains` does NOT die within 40 ticks, that signals the mountain `IntensityDelta` (-0.15) is too weak relative to MaxAge handling — STOP and report so the climate constant can be tuned (do not silently extend the tick budget). With the defaults here, a 0.9 storm under -0.15/tick reaches 0 in ~6 ticks of mountain occupancy, so death is expected well within 40 ticks.

- [ ] **Step 3: Commit**

```bash
git add sim/tick_test.go
git commit -m "test(sim): determinism golden-trace + storm-over-mountain feedback"
```

---

## Task 10: State persistence codec

A pure JSON codec for `State` (including the RNG cursor and fronts), so M3 can persist/restore the simulation via `plugin.WriteBytes`/`ReadBytes`.

**Files:** Create `sim/state.go`, `sim/state_test.go`.

- [ ] **Step 1: Write the failing test `sim/state_test.go`**

```go
package sim

import (
	"reflect"
	"testing"
)

func TestStateJSONRoundTrip(t *testing.T) {
	st := State{
		Round:    7,
		RNGState: 0xDEADBEEF,
		NextID:   3,
		Fronts: []Front{
			{Id: 1, Type: "storm", Zone: "A", Intensity: 0.5, Moisture: 0.6, Age: 4, MaxAge: 24, History: []ZoneId{"B", "A"}},
			{Id: 2, Type: "fog", Zone: "C", Intensity: 0.2, Moisture: 0.9, Age: 1, MaxAge: 12, History: nil},
		},
		Weather: map[ZoneId]WeatherType{"A": "storm", "B": Clear, "C": "fog"},
	}
	b, err := st.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	got, err := StateFromJSON(b)
	if err != nil {
		t.Fatalf("StateFromJSON: %v", err)
	}
	if !reflect.DeepEqual(st, got) {
		t.Errorf("round trip mismatch:\n want %+v\n got  %+v", st, got)
	}
}

func TestStateFromJSONError(t *testing.T) {
	if _, err := StateFromJSON([]byte("{bad")); err == nil {
		t.Error("StateFromJSON should error on malformed JSON")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./sim/ -run TestState`
Expected: FAIL (`st.ToJSON undefined`).

- [ ] **Step 3: Implement `sim/state.go`**

```go
package sim

import "encoding/json"

// ToJSON serializes the full simulation state (RNG cursor + fronts + resolved
// weather) for persistence.
func (s State) ToJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// StateFromJSON restores a State from its serialized form.
func StateFromJSON(b []byte) (State, error) {
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, err
	}
	return s, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./sim/ -run TestState`
Expected: PASS. Then `go test ./sim/...` (full suite) — all PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/state.go sim/state_test.go
git commit -m "feat(sim): State JSON persistence codec"
```

---

## Task 11: Update `sim/context.md`

**Files:** Modify `sim/context.md`.

- [ ] **Step 1: Append a simulation section to `sim/context.md`**

Add this after the existing content:

````markdown
## Weather simulation (M2)

Beyond the geography `Graph`, `sim` now contains the pure, deterministic weather
simulation. It consumes a `*Graph` as its read-only world and produces weather
state as plain data — no engine imports.

### Key files
- **rng.go**: `RNG`, a serializable splitmix64 PRNG (cursor = one `uint64`).
- **weather.go**: `WeatherType` (+ `Clear`), `Front`, `State`, `StateDiff`,
  `ZoneChange`, `Clock`, and `clamp01`.
- **climate.go**: `ClimateProfile` / `WeatherInfluence` / `Climate` (biome →
  weather weights + influence + spawn weight), `Climate.For` (default fallback),
  `DefaultClimate`; plus `Config` / `DefaultConfig` (front budget, spawn chance,
  history length, hard age cap).
- **tick.go**: `Step(prev, graph, climate, cfg, clock) -> (next, diff)` and its
  helpers (`ageAndFeedback`, `moveFronts`, `evolveTypes`, `removeDead`,
  `spawnFronts`, `resolveWeather`, `diffWeather`).
- **state.go**: `State.ToJSON` / `StateFromJSON` (persistence codec).

### The tick
Each `Step`: age fronts and apply the current zone's biome influence (the
weather ← biome feedback), move fronts along edges (damped by `MovementResistance`,
no immediate backtrack), evolve a moved front's type from the new zone's climate,
drop dead fronts (intensity ≤ 0 or past the hard age cap), maybe spawn one front
under budget (origin weighted by `SpawnWeight`), resolve per-zone weather with
**intensity-scaled area coverage** (a front projects onto zones within
`MaxFrontRadius` hops at `Intensity × CoverageFalloff^hops`, covered while
`>= MinProjected`; per zone the highest *effective* intensity wins; frontless
zones = `Clear`), and emit the diff.

### Determinism
All randomness flows through the `RNG` built from `State.RNGState`; the advanced
cursor is written back into the next `State`. Same seed + graph + tick count ⇒
identical fronts and per-zone weather (see `TestStep_Deterministic`).

### Deferred (later milestones)
- File/YAML climate overrides and mutator application live in the engine layer (M3).
- **Prevailing-wind direction** (a MUD owner biasing where storms originate/move,
  e.g. west→east) is a planned later chunk: it needs directional edge metadata,
  which a future crawler pass can derive from each exit's `MapDirection`.
- Calm-zone variety (occasional light fog/overcast in frontless zones) and an
  explicit windward "orographic" precip spike are noted enrichments, not built.
````

- [ ] **Step 2: Verify + commit**

Run: `go test ./sim/...` → PASS. `gofmt -l sim` → empty.

```bash
git add sim/context.md
git commit -m "docs(sim): document the weather simulation core (M2)"
```

---

## Self-Review Notes (author)

**Spec coverage (§7):** §7.1 interfaces → `Step(prev, *Graph, Climate, Config, Clock)` (Graph serves as the WorldView — documented deviation); `State`/`StateDiff` (Task 2). §7.2 core types → Task 2. §7.3 climate profiles → Task 3 (Go-defined `DefaultClimate`; file overrides deferred to M3, noted). §7.4 feedback loop → biome→weather in `spawnFronts`/`evolveTypes` (Tasks 7/8), weather←biome in `ageAndFeedback` (Task 5). §7.5 tick steps 1–7 → Tasks 5 (age/feedback/death/area-coverage resolve/diff), 6 (movement), 7 (type evolution), 8 (spawn/budget); resolve = highest *effective* intensity per zone (Task 5). §7.6 determinism/RNG → Tasks 1, 9. §7.7 acceptance → pure (arch guardrail stays green; no internal/* or third-party), reproducible (Task 9), feedback verified (Task 9 storm-over-mountain), budget respected (Task 8), clamps (Tasks 2/5). **Area coverage (front strength → spread radius, user-requested)** → Task 3 (config) + Task 5 (`resolveWeather`/`zonesWithin`). Persistence for M3 → Task 10.

**Deliberate simplifications (documented in-plan + context.md):** Graph-as-WorldView (no separate interface); climate as Go data (YAML overrides → M3); type evolution via weighted re-roll instead of a transition table; frontless zones = `Clear` (calm-variety deferred). **Prevailing-wind direction is a planned LATER chunk** (needs directional edge metadata from exit `MapDirection`), not omitted-by-accident — M2 movement is edge weight + resistance + no-backtrack only.

**Test robustness:** the tick mechanics (feedback, death, movement, evolution, spawning, area-coverage resolve) are tested via their **helper functions directly**, not the full `Step`, so adding a later pipeline stage can't silently break an earlier task's assertions. `Step` itself gets a determinism + all-zones-covered smoke test (Task 5) and the golden-trace + storm-over-mountain integration tests (Task 9).

**Type/name consistency:** `Step`, `State{Round,RNGState,NextID,Fronts,Weather}`, `Front{Id,Type,Zone,Intensity,Moisture,Age,MaxAge,History}`, `Climate.For`, `ClimateProfile{Weather,Influence,SpawnWeight}`, `Config{MaxActiveFronts,SpawnChance,HistoryLen,FrontHardAge,CoverageFalloff,MinProjected,MaxFrontRadius}`, `RNG{NewRNG,Uint64,Float64,Intn,State}`, helper names (`ageAndFeedback`/`moveFronts`/`evolveTypes`/`removeDead`/`spawnFronts`/`resolveWeather(g,fronts,cfg)`/`zonesWithin`/`pow`/`diffWeather`/`pickNeighbor`/`pickWeatherType`/`pickSpawnZone`/`appendBounded`/`lastZone`/`cloneFronts`) are consistent across tasks. `Graph.Zones()`/`Neighbors()`/`Nodes` match M1's API. The Task-5 stubs use the exact signatures their Task-6/7/8 replacements adopt.

**Placeholders:** none — every step has complete code or an exact command + expected result.

**Tuning caveat called out (Task 9 Step 2):** the storm-over-mountain death depends on the mountain `IntensityDelta` vs. the seeded intensity; the plan states the expected math and tells the executor to STOP and report rather than extend the tick budget if it doesn't converge — so a constant that needs tuning surfaces as a decision, not a silent fudge.
