# Engine Integration & Presentation (M3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the pure M2 weather simulation into a live GoMud server: a coarse weather clock drives `sim.Step`, the resulting `StateDiff` is applied as zone-wide mutators, ambient emotes voice the weather in occupied rooms, state persists across reboots, and a full player/admin command set plus an exported API round it out — first end-to-end weather (spec §8–§10, milestone M3).

**Architecture:** The root `weather` package stays the wiring/lifecycle owner (single-goroutine event loop — no mutexes). A new pure `content/` package parses module-owned YAML data (climate overrides, emote tables). The `engine/` package gains the only new engine-world calls: mutator application/reconciliation, the gametime clock, state persistence codec, and the ambient-emote emitter. Weather **mutator specs ship as plugin data files** — upstream GoMud now loads `mutators/*.yaml` from plugin filesystems (verified below), so zero engine changes remain true.

**Tech Stack:** Go 1.25. New dev dependency for the standalone repo: `gopkg.in/yaml.v2` (already a GoMud engine dependency, so nothing changes in-checkout). Engine-coupled packages compile/test only inside the upstream GoMud checkout (`~/workspace/GoMud`).

**Spec:** Implements §8 (engine adapter), §9 (presentation & application), §10 (config) of `docs/superpowers/specs/2026-06-08-weather-module-design.md`, plus the M2-review follow-ups (adjacency index, full-state determinism test).

---

## Verified engine facts (upstream GoMud master, commit `0ce6447`, fetched 2026-06-09)

The executor should trust these — they were re-verified against upstream source (NOT the DOGMud fork) specifically for this plan. Paths are relative to the GoMud checkout at `~/workspace/GoMud`.

1. **`mutators.MutatorSpec`** (`internal/mutators/mutators.go:56`): `MutatorId string`; `NameModifier`/`DescriptionModifier`/`AlertModifier *TextModifier` (`{Behavior, Text, ColorPattern}`, alert is append-only by engine design); `DecayIntoId string`; `PlayerBuffIds`/`MobBuffIds`/`NativeBuffIds []int`; `DecayRate string`; `RespawnRate string`; `LightMod int`; `Exits`; `Pvp`; `Tags []string`. (No `RegenMultiplier` — the spec §5.4 mention of it is stale; we don't need it.)
2. **`mutators.MutatorList`** methods: `Add(name) bool` (false if the spec id is unknown; **appends a duplicate if the mutator is already live** — always check `Has` first), `Remove(name) bool`, `Has(name) bool`, `GetActive() MutatorList`, `Update(round)`. A removed mutator with no `RespawnRate` is purged from the list by `Update`. CORRECTION (found in M3 execution, empirically verified): Remove() resets SpawnedRound to 0 and immediately runs Update(), whose decay branch has no liveness guard — any spec with decayintoid is instantly resurrected as its decay target on Remove. Weather specs therefore must NOT set decayintoid; decayrate alone despawns and purges cleanly.
3. **Modules can ship mutator specs.** `main.go:199` wires `mutators.RegisterFS(plugins.GetPluginRegistry())`; `internal/mutators/plugin.go` merges any `mutators/*.yaml` found in plugin filesystems into the registry at `LoadDataFiles()` (duplicate ids are rejected with an error log). `plugin.AttachFileSystem` (`internal/plugins/plugins.go:345`) registers embedded paths **stripping everything up to and including `datafiles/`** — so our embedded `files/datafiles/mutators/weather_storm.yaml` is visible as `mutators/weather_storm.yaml`. **Filename rule:** the loader requires the path to end with `util.ConvertForFilename(MutatorId) + ".yaml"`, which lowercases and converts every non-`[a-z0-9]` rune to `_` — id `weather-storm` ⇒ file `weather_storm.yaml` (precedent: default world's `forest_mist.yaml` with `mutatorid: forest-mist`).
4. **Module-shipped buff specs also landed upstream** (`internal/buffs/plugin.go`, wired in `main.go:200`) — the spec's R-core-1 contingency is now real. We still **reuse existing engine buff ids for M3** (31 `freezing_snow`, 33 `thirsty` — verified present in `_datafiles/world/default/buffs/`) because numeric-id collision policy is unresolved upstream; bespoke weather buffs become an M4 option.
5. **Zone-wide application:** `rooms.GetZoneConfig(zone) *ZoneConfig` (`internal/rooms/roommanager.go:581`); `ZoneConfig.Mutators mutators.MutatorList` applies to the whole zone; the engine hook `UpdateZoneMutators` (`internal/hooks/NewRound_UpdateZoneMutators.go`) calls `Mutators.Update(round)` for every zone with mutators each `NewRound`; renders merge zone+room mutators live. Zone configs are written to disk **only by explicit admin zone-edit actions** — runtime-added mutators are in-memory and vanish on reboot, hence the Reconcile-on-boot step in this plan.
6. **Clock:** `events.NewRound{RoundNumber uint64, TimeNow time.Time}`; `gametime.GetDate(forceRound ...uint64) GameDate`; `GameDate.AddPeriod(period string) uint64` parses `"N hour(s)"` (game-hours, `gametime.go:431`) and returns the target round; `util.GetRoundCount() uint64`.
7. **Persistence:** `plugin.WriteBytes(id, []byte)` / `plugin.ReadBytes(id)`; `plugins.Save()` (→ our `SetOnSave` callback) runs on shutdown, copyover, AND periodic autosave (`internal/hooks/NewTurn_AutoSave.go`).
8. **Emote delivery:** `room.SendText(txt string, excludeUserIds ...int)` queues a message to a room; `rooms.GetRoomsWithPlayers() []int`; `rooms.LoadRoom(id)`; `room.GetBiome()` (nil-able, has `BiomeId`). `BiomeInfo` has **no indoor/outdoor flag** — keep using `engine.isOutdoorBiome`'s heuristic set.
9. **Commands & permissions:** `plugin.AddUserCommand(cmd, handler, allowWhenDowned, isAdminOnly)`. For per-subcommand gating inside one handler: `user.HasRolePermission("weather", true)` — admins always pass, mods pass if granted the `weather` permission key.
10. **Config:** `plugin.Config.Get(name)` reads the flattened `Modules.weather.<name>` key (values arrive as `any`: bool/int/float64/string from YAML). Use **flat key names** (e.g. `BuffsEnabled`, not nested `Buffs.Enabled`) — flat keys are what `configs.Flatten` is guaranteed to produce for scalar leaves.
11. **Exports:** `plugin.ExportFunction(stringId, fn)` (precedent: `modules/gmcp/gmcp.go:59`).
12. **Color patterns** are world data (`_datafiles/world/default/color-patterns.yaml`). Valid names used in this plan: `gray`, `blue`, `mute-dblue`, `frost`, `brown`, `embers`. (There is no `storm` pattern — the spec's §9.1 example was illustrative.)
13. **Module registration in the checkout** already exists from M1b: `modules/all-modules.go` imports `modules/weather`, and `scripts/sync-to-checkout.ps1` mirrors this repo into `~/workspace/GoMud/modules/weather` (excluding `go.mod`/`go.sum`).

## Design decisions (read before starting)

- **Scope cuts (deliberate, recorded):**
  - **Per-room refinement** (indoor/biome-variant *mutators*, spec §9.1 refinement + `PerRoomRefinement` config key) is **deferred to M4**. Emotes ARE indoor-aware in M3 (the table layer), so indoor players still hear "rain drums on the roof"; only the room-description variant machinery waits.
  - **`Buffs.Overrides`** config (remap weather→buff ids) is deferred to M4; M3 ships the `BuffsEnabled` master toggle only.
  - **`PrevailingWind`** stays deferred (needs directional edge metadata from a future crawler pass — see `sim/context.md`).
  - **No climate YAML files ship in M3** — `sim.DefaultClimate()` covers the standard biomes. The *loader* ships now (so builders can drop `files/datafiles/climate/<biome>.yaml` and rebuild); M4 may externalize the defaults.
- **Presentation randomness never touches the sim RNG.** Emote line selection uses `util.Rand` (engine) via an injected `roll func(int) int`. Consuming the sim RNG for prose would perturb the deterministic weather trace.
- **One weather mutator per zone, namespaced `weather-<type>`.** `clear` (and the unset `""`) map to *no* mutator. The applier is the orchestrator (authoritative add/remove); the specs' `decayrate` is the §9.2 self-heal safety net only — **no `respawnrate` ever** (it would fight the orchestrator and prevent purge-on-remove) and **no `decayintoid` ever** (the engine's Remove would instantly resurrect the entry as the decay target — see Verified facts #2). Because `decayrate` also fires during normal operation when a weather spell outlasts it, the tick path calls `engine.Reconcile` (not just `Apply`) every tick so engine-side decay drift self-corrects within one tick.
- **Reconcile on boot.** Persisted sim state is restored, then `engine.Reconcile` forces every zone's live `weather-*` mutators to match it (zone mutators don't survive reboots; an admin zone-save could also leave strays).
- **`yaml.v2` in pure packages is allowed.** The purity rule (arch tests) forbids `GoMudEngine/GoMud/internal` imports — engine independence, not zero-deps. `gopkg.in/yaml.v2` is in the engine's go.mod (used by `internal/mutators` itself), so in-checkout builds are unaffected; the standalone repo's dev `go.mod` gains the dependency.
- **State persistence wraps `sim.State` in a versioned envelope in `engine/`** (`{version, state}`) rather than adding a version field to `sim.State` — sim stays untouched, and stale formats are detected and discarded (fresh seed) like the graph cache.
- **`Graph.Neighbors` returns a shared slice once the adjacency index lands (Task 1). Callers must never mutate it** — the existing `printGraphForZone` sorts its result and MUST copy first (fixed in Task 14).
- **Fail-soft everywhere** (spec §2.3.5): no graph ⇒ sim disabled with a warning, commands degrade politely; missing emote table ⇒ silence; unknown mutator spec ⇒ warn once per id; bad persisted state ⇒ fresh seed.

## File structure

| File | Responsibility |
|---|---|
| `sim/graph.go` (modify) | Adjacency index for `Neighbors` (M2 follow-up); `FindZone` case-insensitive resolver. |
| `sim/state.go` (modify) | `NewState(seed)`, `DeriveSeed(graph)`. |
| `sim/query.go` (new) | `Coverage`, `Covering(g, fronts, cfg, zone)` — read-only coverage query for commands/API. |
| `sim/mutate.go` (new) | `ForceSpawn`, `ClearZones` — pure admin mutations that re-resolve + diff. |
| `sim/tick_test.go` (modify) | Full-`State` `reflect.DeepEqual` determinism (M2 follow-up). |
| `content/climate.go`, `content/emotes.go` (new pkg) | Parse/load module YAML: climate overrides → `sim.Climate`; emote `Tables` + `Pick`. |
| `content/arch_test.go`, `content/moduledata_test.go` | Purity guard; validation of the shipped YAML under `files/datafiles/`. |
| `files/datafiles/mutators/weather_*.yaml` (new, 8) | Weather mutator specs (engine `MutatorSpec` schema, plugin-FS loaded). |
| `files/datafiles/emotes/*.yaml` (new, 8) | Default ambient emote tables. |
| `engine/state.go` (new) | `StateIdentifier`, versioned `EncodeState`/`DecodeState`. |
| `engine/apply.go` (new) | `MutatorIdFor`, `applyChange`, `reconcileZone` (testable core) + `Apply(diff)`, `Reconcile(weather)`, `StripBuffs()` glue. |
| `engine/clock.go` (new) | `NextTickRound(period)`, `TickPeriod(hours)`, `CurrentRound()`. |
| `engine/emotes.go` (new) | `EmitAmbient(weather, tables, roll)` — occupied rooms → `Pick` → `room.SendText`. |
| `weather_config.go` (modify) | Full M3 `Config` + coercion helpers + `simConfig()`. |
| `files/data-overlays/config.yaml` (modify) | New default keys. |
| `weather.go` (modify) + `weather_tick.go` (new) | Lifecycle: startup (content/state/reconcile), tick loop, emote scheduling, persistence, `onSave`. |
| `weather_commands.go` (new; command code moves out of `weather.go`) | Player `weather` + admin `zones/fronts/spawn/clear/graph/rebuild/status`. |
| `weather_api.go` (new) | Exported `GetWeather`/`GetFronts`/`SpawnFront`. |
| `context.md`, `sim/context.md`, `engine/context.md`, `content/context.md`, `CONTRIBUTING.md` | Documentation (per-package context.md rule). |

**Test commands.** Standalone (this repo): `go test ./sim/... ./crawler/... ./content/...`. Engine-coupled (root + `engine/`): sync then test in the checkout —

```powershell
pwsh scripts/sync-to-checkout.ps1 -Checkout "$HOME\workspace\GoMud"
Push-Location "$HOME\workspace\GoMud"; go test ./modules/weather/...; Pop-Location
```

---

## Task 1: Adjacency index for `Graph.Neighbors` (M2 follow-up)

`Neighbors` is O(E) per call and the engine will call it hundreds of times per tick (BFS per front, movement, coverage). Build the index once, lazily.

**Files:** Modify `sim/graph.go`, `sim/graph_test.go`.

- [ ] **Step 1: Add failing tests to `sim/graph_test.go`**

```go
func TestNeighborsUsesStableIndex(t *testing.T) {
	g := &Graph{
		Nodes: map[string]ZoneNode{"A": {Zone: "A"}, "B": {Zone: "B"}, "C": {Zone: "C"}},
		Edges: []Edge{{A: "A", B: "B", Weight: 2}, {A: "B", B: "C", Weight: 1}},
	}
	first := g.Neighbors("B")
	second := g.Neighbors("B")
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("B should have 2 neighbors, got %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("Neighbors must be stable across calls: %v vs %v", first, second)
		}
	}
	// Orientation: every returned edge is oriented from the queried zone.
	for _, e := range first {
		if e.A != "B" {
			t.Errorf("edge not oriented from queried zone: %+v", e)
		}
	}
	if g.Neighbors("missing") != nil {
		t.Error("unknown zone should return nil")
	}
}

func TestNeighborsIndexRebuiltAfterDecode(t *testing.T) {
	g := &Graph{
		Nodes: map[string]ZoneNode{"A": {Zone: "A"}, "B": {Zone: "B"}},
		Edges: []Edge{{A: "A", B: "B", Weight: 1}},
	}
	_ = g.Neighbors("A") // force index build
	b, err := g.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	g2, err := FromJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	n := g2.Neighbors("B")
	if len(n) != 1 || n[0].B != "A" || n[0].Weight != 1 {
		t.Fatalf("decoded graph must answer Neighbors correctly, got %v", n)
	}
}
```

- [ ] **Step 2: Run to verify the new tests' assumptions against the old code**

Run: `go test ./sim/ -run TestNeighbors`
Expected: PASS already (the O(E) scan also satisfies them) — these tests pin behavior so the rewrite can't regress it. That is acceptable here; the change is a performance refactor, not new behavior.

- [ ] **Step 3: Replace `Neighbors` in `sim/graph.go`**

Add an unexported index field to `Graph` (JSON ignores unexported fields, so the cache format is unchanged):

```go
type Graph struct {
	Version      int                 `json:"version"`
	BuiltAtRound uint64              `json:"builtAtRound"`
	Nodes        map[string]ZoneNode `json:"nodes"`
	Edges        []Edge              `json:"edges"`
	Components   int                 `json:"components"`

	adj map[string][]Edge // lazy adjacency index; rebuilt after FromJSON (nil there)
}
```

Replace the `Neighbors` method:

```go
// Neighbors returns the zones adjacent to z, each as an Edge oriented from z
// (Edge.A == z). The result is a shared slice from a lazily-built index —
// callers MUST NOT mutate it (copy before sorting). Returns nil for unknown
// or isolated zones.
func (g *Graph) Neighbors(z string) []Edge {
	if g.adj == nil {
		g.buildAdjacency()
	}
	return g.adj[z]
}

// buildAdjacency indexes Edges by zone, both orientations. Called lazily from
// Neighbors; the module runs on GoMud's single game-loop goroutine, so the
// unsynchronized lazy build is safe.
func (g *Graph) buildAdjacency() {
	g.adj = make(map[string][]Edge, len(g.Nodes))
	for _, e := range g.Edges {
		g.adj[e.A] = append(g.adj[e.A], e)
		if e.B != e.A {
			g.adj[e.B] = append(g.adj[e.B], Edge{A: e.B, B: e.A, Weight: e.Weight})
		}
	}
}
```

- [ ] **Step 4: Run the full sim suite (determinism must hold — index order matches the old scan order: `g.Edges` order per zone)**

Run: `go test ./sim/...`
Expected: PASS, including `TestStep_Deterministic` and the golden-trace test (the per-zone edge order produced by the index is identical to the old per-call scan order, so RNG-weighted picks are unchanged).

- [ ] **Step 5: Commit**

```bash
git add sim/graph.go sim/graph_test.go
git commit -m "perf(sim): index zone adjacency once instead of scanning all edges per Neighbors call"
```

---

## Task 2: Full-state determinism test (M2 follow-up)

**Files:** Modify `sim/tick_test.go`.

- [ ] **Step 1: Strengthen `TestStep_Deterministic`**

Replace the body of `TestStep_Deterministic` (currently compares RNG cursor, weather map, and front counts piecemeal) with a full-state comparison, and add `"reflect"` to the test file's imports:

```go
func TestStep_Deterministic(t *testing.T) {
	a := runTicks(12345, 40)
	b := runTicks(12345, 40)
	// Same seed + world + tick count => byte-for-byte identical State
	// (fronts incl. History, weather, RNG cursor, NextID, Round).
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("identical runs diverged:\n a=%+v\n b=%+v", a, b)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./sim/ -run TestStep_Deterministic -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add sim/tick_test.go
git commit -m "test(sim): compare full State with reflect.DeepEqual in determinism test (M2 review follow-up)"
```

---

## Task 3: `NewState` and `DeriveSeed`

The engine layer needs a blessed way to construct a fresh run and a stable per-world default seed (spec §7.6: "default derived from world name so two worlds differ but each world is stable"). We derive from the graph's sorted zone names — pure, no engine API needed.

**Files:** Modify `sim/state.go`, `sim/state_test.go`.

- [ ] **Step 1: Write failing tests in `sim/state_test.go`**

```go
func TestNewState(t *testing.T) {
	s := NewState(99)
	if s.RNGState != 99 {
		t.Errorf("seed not stored: %d", s.RNGState)
	}
	if s.NextID != 1 {
		t.Errorf("NextID should start at 1, got %d", s.NextID)
	}
	if s.Weather == nil || len(s.Weather) != 0 {
		t.Errorf("Weather should be empty non-nil map: %v", s.Weather)
	}
	if len(s.Fronts) != 0 {
		t.Errorf("Fronts should be empty: %v", s.Fronts)
	}
}

func TestDeriveSeedStableAndWorldSensitive(t *testing.T) {
	g1 := &Graph{Nodes: map[string]ZoneNode{"A": {}, "B": {}}}
	g1b := &Graph{Nodes: map[string]ZoneNode{"B": {}, "A": {}}} // same zones, different insertion
	g2 := &Graph{Nodes: map[string]ZoneNode{"A": {}, "C": {}}}
	if DeriveSeed(g1) != DeriveSeed(g1b) {
		t.Error("same zone set must derive the same seed")
	}
	if DeriveSeed(g1) == DeriveSeed(g2) {
		t.Error("different worlds should derive different seeds")
	}
	if DeriveSeed(g1) == 0 {
		t.Error("derived seed should be non-zero for a non-empty world")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./sim/ -run 'TestNewState|TestDeriveSeed'`
Expected: FAIL (`undefined: NewState`, `undefined: DeriveSeed`).

- [ ] **Step 3: Implement in `sim/state.go`**

```go
// NewState returns the initial simulation state for a fresh run.
func NewState(seed uint64) State {
	return State{
		RNGState: seed,
		NextID:   1,
		Weather:  map[ZoneId]WeatherType{},
	}
}

// DeriveSeed produces a stable default seed from the graph's sorted zone names
// (FNV-1a), so each world gets the same seed on every boot but two worlds
// differ. Used when the configured Seed is 0.
func DeriveSeed(g *Graph) uint64 {
	const prime = 1099511628211
	h := uint64(14695981039346656037)
	for _, z := range g.Zones() {
		for i := 0; i < len(z); i++ {
			h ^= uint64(z[i])
			h *= prime
		}
		h ^= 0xff // name separator so ["ab","c"] != ["a","bc"]
		h *= prime
	}
	return h
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./sim/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/state.go sim/state_test.go
git commit -m "feat(sim): NewState constructor and world-stable DeriveSeed"
```

---

## Task 4: Coverage query (`Covering`) and `FindZone`

The `weather` player command and the exported API need "which fronts affect this zone, how strongly" — the read-only twin of `resolveWeather`'s coverage rule. Commands also need case-insensitive zone lookup.

**Files:** Create `sim/query.go`, `sim/query_test.go`. Modify `sim/graph.go`, `sim/graph_test.go`.

- [ ] **Step 1: Write failing tests in `sim/query_test.go`**

```go
package sim

import "testing"

func coverageGraph() *Graph {
	// A - B - C - D chain.
	return &Graph{
		Nodes: map[string]ZoneNode{
			"A": {Zone: "A", Biome: "plains"}, "B": {Zone: "B", Biome: "plains"},
			"C": {Zone: "C", Biome: "plains"}, "D": {Zone: "D", Biome: "plains"},
		},
		Edges: []Edge{{A: "A", B: "B", Weight: 1}, {A: "B", B: "C", Weight: 1}, {A: "C", B: "D", Weight: 1}},
	}
}

func TestCovering(t *testing.T) {
	g := coverageGraph()
	cfg := DefaultConfig() // falloff 0.5, min 0.15, radius 2
	fronts := []Front{
		{Id: 1, Type: "storm", Zone: "A", Intensity: 0.8},
		{Id: 2, Type: "fog", Zone: "C", Intensity: 0.3},
	}
	// Zone B: storm projects 0.8*0.5=0.40 (1 hop), fog projects 0.3*0.5=0.15 (1 hop).
	got := Covering(g, fronts, cfg, "B")
	if len(got) != 2 {
		t.Fatalf("expected 2 covering fronts at B, got %d: %+v", len(got), got)
	}
	if got[0].Front.Id != 1 || got[0].Hops != 1 || got[0].Effective != 0.4 {
		t.Errorf("strongest first: %+v", got[0])
	}
	if got[1].Front.Id != 2 || got[1].Effective != 0.15 {
		t.Errorf("weaker second: %+v", got[1])
	}
	// Zone D: storm is 3 hops away (out of radius); fog projects 0.15 at 1 hop.
	got = Covering(g, fronts, cfg, "D")
	if len(got) != 1 || got[0].Front.Id != 2 {
		t.Fatalf("expected only fog at D: %+v", got)
	}
	// Unknown zone: nothing.
	if got := Covering(g, fronts, cfg, "nope"); len(got) != 0 {
		t.Errorf("unknown zone should have no coverage: %+v", got)
	}
}

func TestCoveringMatchesResolveWeather(t *testing.T) {
	g := coverageGraph()
	cfg := DefaultConfig()
	fronts := []Front{
		{Id: 1, Type: "storm", Zone: "A", Intensity: 0.8},
		{Id: 2, Type: "rain", Zone: "B", Intensity: 0.9},
	}
	resolved := resolveWeather(g, fronts, cfg)
	for _, z := range g.Zones() {
		covers := Covering(g, fronts, cfg, z)
		want := Clear
		if len(covers) > 0 {
			want = covers[0].Front.Type
		}
		if resolved[z] != want {
			t.Errorf("zone %s: resolveWeather=%s but Covering says %s", z, resolved[z], want)
		}
	}
}
```

And in `sim/graph_test.go`:

```go
func TestFindZone(t *testing.T) {
	g := &Graph{Nodes: map[string]ZoneNode{"Frostfang": {Zone: "Frostfang"}}}
	if z, ok := g.FindZone("Frostfang"); !ok || z != "Frostfang" {
		t.Errorf("exact match failed: %q %v", z, ok)
	}
	if z, ok := g.FindZone("frostFANG"); !ok || z != "Frostfang" {
		t.Errorf("case-insensitive match failed: %q %v", z, ok)
	}
	if _, ok := g.FindZone("nowhere"); ok {
		t.Error("missing zone must not match")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./sim/ -run 'TestCovering|TestFindZone'`
Expected: FAIL (`undefined: Covering`, `undefined: (*Graph).FindZone`).

- [ ] **Step 3: Implement `sim/query.go`**

```go
package sim

import "sort"

// Coverage describes one front's projection onto a queried zone.
type Coverage struct {
	Front     Front
	Effective float64 // projected intensity at the queried zone
	Hops      int     // graph distance from the front's center
}

// Covering returns the fronts whose area projection reaches zone, strongest
// effective intensity first (ties broken by lowest front id). It mirrors
// resolveWeather's coverage rule exactly: projection = Intensity *
// CoverageFalloff^hops within MaxFrontRadius, covered while >= MinProjected.
func Covering(g *Graph, fronts []Front, cfg Config, zone ZoneId) []Coverage {
	var out []Coverage
	for i := range fronts {
		f := fronts[i]
		hops, ok := zonesWithin(g, f.Zone, cfg.MaxFrontRadius)[zone]
		if !ok {
			continue
		}
		eff := f.Intensity * pow(cfg.CoverageFalloff, hops)
		if eff < cfg.MinProjected {
			continue
		}
		out = append(out, Coverage{Front: f, Effective: eff, Hops: hops})
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Effective != out[b].Effective {
			return out[a].Effective > out[b].Effective
		}
		return out[a].Front.Id < out[b].Front.Id
	})
	return out
}
```

And in `sim/graph.go` (add `"strings"` to imports):

```go
// FindZone resolves a zone name case-insensitively to its canonical graph key.
// An exact match wins; otherwise the first case-insensitive match is returned.
func (g *Graph) FindZone(name string) (string, bool) {
	if _, ok := g.Nodes[name]; ok {
		return name, true
	}
	for _, z := range g.Zones() {
		if strings.EqualFold(z, name) {
			return z, true
		}
	}
	return "", false
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./sim/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/query.go sim/query_test.go sim/graph.go sim/graph_test.go
git commit -m "feat(sim): Covering coverage query and case-insensitive FindZone"
```

---

## Task 5: Pure admin mutations — `ForceSpawn` and `ClearZones`

`weather spawn`/`weather clear`/the exported `SpawnFront` mutate the simulation. Keep the logic pure in `sim/` so the engine applies the result through the same `StateDiff` path as a tick.

**Files:** Create `sim/mutate.go`, `sim/mutate_test.go`.

- [ ] **Step 1: Write failing tests in `sim/mutate_test.go`**

```go
package sim

import "testing"

func TestForceSpawn(t *testing.T) {
	g := coverageGraph()
	cfg := DefaultConfig()
	st := NewState(1)

	next, diff, ok := ForceSpawn(st, g, cfg, "storm", "B", 0.9, Clock{Round: 5})
	if !ok {
		t.Fatal("spawn into a known zone must succeed")
	}
	if len(next.Fronts) != 1 || next.Fronts[0].Type != "storm" || next.Fronts[0].Zone != "B" {
		t.Fatalf("front not created: %+v", next.Fronts)
	}
	if next.Fronts[0].Id != 1 || next.NextID != 2 {
		t.Errorf("front id accounting wrong: id=%d nextId=%d", next.Fronts[0].Id, next.NextID)
	}
	if next.Weather["B"] != "storm" {
		t.Errorf("zone B should be storm, got %s", next.Weather["B"])
	}
	if len(diff.Changes) == 0 {
		t.Error("diff should record the new weather")
	}
	if next.Round != 5 {
		t.Errorf("round not stamped: %d", next.Round)
	}
	// Original state untouched (pure function).
	if len(st.Fronts) != 0 || len(st.Weather) != 0 {
		t.Error("input state must not be mutated")
	}

	if _, _, ok := ForceSpawn(st, g, cfg, "storm", "nowhere", 0.9, Clock{}); ok {
		t.Error("unknown zone must fail")
	}
}

func TestForceSpawnDefaultIntensity(t *testing.T) {
	g := coverageGraph()
	next, _, ok := ForceSpawn(NewState(1), g, DefaultConfig(), "rain", "A", 0, Clock{})
	if !ok || next.Fronts[0].Intensity != 0.6 {
		t.Fatalf("zero intensity should default to 0.6, got %+v", next.Fronts)
	}
}

func TestClearZones(t *testing.T) {
	g := coverageGraph()
	cfg := DefaultConfig()
	st := NewState(1)
	st, _, _ = ForceSpawn(st, g, cfg, "storm", "A", 0.9, Clock{})
	st, _, _ = ForceSpawn(st, g, cfg, "fog", "D", 0.9, Clock{})

	// Clearing zone A removes the storm centered there (coverage trivially
	// reaches its own center) but not the fog at D, whose projection to A is
	// 3 hops away — beyond MaxFrontRadius 2, so it does not cover A.
	next, diff := ClearZones(st, g, cfg, []ZoneId{"A"}, Clock{Round: 9})
	if len(next.Fronts) != 1 || next.Fronts[0].Type != "fog" {
		t.Fatalf("only the fog front should survive: %+v", next.Fronts)
	}
	if next.Weather["A"] != Clear {
		t.Errorf("cleared zone should be clear, got %s", next.Weather["A"])
	}
	if len(diff.Changes) == 0 {
		t.Error("clearing should diff")
	}

	// No zones = clear everything.
	next, _ = ClearZones(st, g, cfg, nil, Clock{Round: 9})
	if len(next.Fronts) != 0 {
		t.Fatalf("clear-all should drop every front: %+v", next.Fronts)
	}
	for z, w := range next.Weather {
		if w != Clear {
			t.Errorf("zone %s should be clear, got %s", z, w)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./sim/ -run 'TestForceSpawn|TestClearZones'`
Expected: FAIL (`undefined: ForceSpawn`, `undefined: ClearZones`).

- [ ] **Step 3: Implement `sim/mutate.go`**

```go
package sim

// ForceSpawn injects a front at zone (admin command / exported API), bypassing
// the budget and spawn chance but flowing through the same resolve+diff path as
// Step so the engine applies the result uniformly. intensity <= 0 defaults to
// 0.6. Returns ok=false (state unchanged) for an unknown zone. Pure: the input
// state is not mutated, and no RNG is consumed (admin actions must not perturb
// the deterministic trace).
func ForceSpawn(prev State, g *Graph, cfg Config, wtype WeatherType, zone ZoneId, intensity float64, now Clock) (State, StateDiff, bool) {
	if _, ok := g.Nodes[zone]; !ok {
		return prev, StateDiff{}, false
	}
	if intensity <= 0 {
		intensity = 0.6
	}
	next := prev
	next.Round = now.Round
	next.Fronts = cloneFronts(prev.Fronts)
	next.Fronts = append(next.Fronts, Front{
		Id:        prev.NextID,
		Type:      wtype,
		Zone:      zone,
		Intensity: clamp01(intensity),
		Moisture:  0.5,
		MaxAge:    24,
	})
	next.NextID = prev.NextID + 1
	next.Weather = resolveWeather(g, next.Fronts, cfg)
	return next, diffWeather(prev.Weather, next.Weather), true
}

// ClearZones removes fronts and re-resolves weather. With no zones it removes
// every front. With zones, any front whose coverage projection reaches one of
// them is removed — so the named zone actually clears rather than staying
// covered by a neighboring front. Pure; no RNG consumed.
func ClearZones(prev State, g *Graph, cfg Config, zones []ZoneId, now Clock) (State, StateDiff) {
	next := prev
	next.Round = now.Round

	if len(zones) == 0 {
		next.Fronts = nil
	} else {
		drop := map[FrontId]bool{}
		for _, z := range zones {
			for _, c := range Covering(g, prev.Fronts, cfg, z) {
				drop[c.Front.Id] = true
			}
		}
		keep := make([]Front, 0, len(prev.Fronts))
		for _, f := range prev.Fronts {
			if !drop[f.Id] {
				keep = append(keep, f)
			}
		}
		next.Fronts = keep
	}

	next.Weather = resolveWeather(g, next.Fronts, cfg)
	return next, diffWeather(prev.Weather, next.Weather)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./sim/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add sim/mutate.go sim/mutate_test.go
git commit -m "feat(sim): pure ForceSpawn and ClearZones admin mutations"
```

---

## Task 6: `content` package — climate YAML loading

New pure package owning module data-file parsing. First half: climate overrides (spec §7.3 file schema) merged over `sim.DefaultClimate()`. Adds the `yaml.v2` dev dependency.

**Files:** Create `content/climate.go`, `content/climate_test.go`, `content/arch_test.go`, `content/doc.go`. Modify `go.mod` (+ new `go.sum`).

- [ ] **Step 1: Add the yaml dependency to the standalone dev module**

Run: `go get gopkg.in/yaml.v2`
Expected: `go.mod` gains `require gopkg.in/yaml.v2 v2.4.0` and `go.sum` appears. (The sync script already excludes both files; in-checkout the import resolves via the engine's go.mod, which uses yaml.v2 itself.)

- [ ] **Step 2: Write failing tests in `content/climate_test.go`**

```go
package content

import (
	"testing"
	"testing/fstest"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

const tundraYAML = `biome: tundra
weather:
  snow: 9
  clear: 1
influence:
  intensityDelta: -0.07
  moistureDelta: -0.03
  movementResistance: 0.4
spawnWeight: 0.5
`

func TestParseClimate(t *testing.T) {
	biome, p, err := ParseClimate([]byte(tundraYAML))
	if err != nil {
		t.Fatal(err)
	}
	if biome != "tundra" {
		t.Errorf("biome: %q", biome)
	}
	if p.Weather[sim.WeatherType("snow")] != 9 || p.Weather[sim.WeatherType("clear")] != 1 {
		t.Errorf("weights: %+v", p.Weather)
	}
	if p.Influence.IntensityDelta != -0.07 || p.Influence.MovementResistance != 0.4 {
		t.Errorf("influence: %+v", p.Influence)
	}
	if p.SpawnWeight != 0.5 {
		t.Errorf("spawnWeight: %v", p.SpawnWeight)
	}
}

func TestParseClimateRejectsMissingBiome(t *testing.T) {
	if _, _, err := ParseClimate([]byte("weather:\n  clear: 1\n")); err == nil {
		t.Fatal("a climate file without 'biome' must be rejected")
	}
}

func TestLoadClimateMergesOverDefaults(t *testing.T) {
	fsys := fstest.MapFS{
		"climate/tundra.yaml": {Data: []byte(tundraYAML)},
	}
	c, err := LoadClimate(fsys, "climate")
	if err != nil {
		t.Fatal(err)
	}
	if c["tundra"].Weather[sim.WeatherType("snow")] != 9 {
		t.Error("override profile not applied")
	}
	if _, ok := c["ocean"]; !ok {
		t.Error("non-overridden default profiles must survive the merge")
	}
}

func TestLoadClimateMissingDirIsDefaults(t *testing.T) {
	c, err := LoadClimate(fstest.MapFS{}, "climate")
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != len(sim.DefaultClimate()) {
		t.Error("missing dir should return pure defaults")
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test ./content/`
Expected: FAIL (`undefined: ParseClimate` etc. — the package won't even build yet; create the files in Step 4).

- [ ] **Step 4: Implement `content/doc.go` and `content/climate.go`**

`content/doc.go`:

```go
// Package content parses the weather module's own data files (climate
// profiles, ambient emote tables) from a file system — typically the module's
// embedded files/ tree. It is pure: no engine imports (enforced by
// arch_test.go), so everything here is unit-testable standalone. The only
// non-stdlib dependency is gopkg.in/yaml.v2, which the GoMud engine itself
// depends on.
package content
```

`content/climate.go`:

```go
package content

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
	"gopkg.in/yaml.v2"
)

// climateFile mirrors the on-disk climate schema (spec §7.3). Field names are
// camelCase in the files, so explicit yaml tags are required (yaml.v2 would
// otherwise expect all-lowercase keys).
type climateFile struct {
	Biome     string             `yaml:"biome"`
	Weather   map[string]float64 `yaml:"weather"`
	Influence struct {
		IntensityDelta     float64 `yaml:"intensityDelta"`
		MoistureDelta      float64 `yaml:"moistureDelta"`
		MovementResistance float64 `yaml:"movementResistance"`
	} `yaml:"influence"`
	SpawnWeight float64 `yaml:"spawnWeight"`
}

// ParseClimate parses one climate profile file into its biome id and profile.
func ParseClimate(b []byte) (string, sim.ClimateProfile, error) {
	var cf climateFile
	if err := yaml.Unmarshal(b, &cf); err != nil {
		return "", sim.ClimateProfile{}, err
	}
	if cf.Biome == "" {
		return "", sim.ClimateProfile{}, fmt.Errorf("climate file missing required 'biome' key")
	}
	p := sim.ClimateProfile{
		Weather: make(map[sim.WeatherType]float64, len(cf.Weather)),
		Influence: sim.WeatherInfluence{
			IntensityDelta:     cf.Influence.IntensityDelta,
			MoistureDelta:      cf.Influence.MoistureDelta,
			MovementResistance: cf.Influence.MovementResistance,
		},
		SpawnWeight: cf.SpawnWeight,
	}
	for k, v := range cf.Weather {
		p.Weather[sim.WeatherType(k)] = v
	}
	return cf.Biome, p, nil
}

// LoadClimate returns sim.DefaultClimate() overlaid with every *.yaml climate
// file found under dir in fsys. A file replaces the default profile for its
// biome wholesale (omitted keys become zero values — including spawnWeight, so
// a profile that should spawn fronts must say so). A missing dir simply means
// no overrides. The first malformed file aborts with an error; the caller
// decides whether to fail soft.
func LoadClimate(fsys fs.FS, dir string) (sim.Climate, error) {
	climate := sim.DefaultClimate()
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return climate, nil // dir not shipped — pure defaults
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return climate, fmt.Errorf("%s: %w", e.Name(), err)
		}
		biome, p, err := ParseClimate(b)
		if err != nil {
			return climate, fmt.Errorf("%s: %w", e.Name(), err)
		}
		climate[biome] = p
	}
	return climate, nil
}
```

`content/arch_test.go` — copy of the sim guardrail, adjusted comment:

```go
package content

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestContentPackageStaysPure: content parses module data and must never
// import the GoMud engine — engine access belongs in engine/.
func TestContentPackageStaysPure(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(p, "GoMudEngine/GoMud/internal") {
				t.Errorf("%s imports forbidden engine package %q (content must stay pure)", e.Name(), p)
			}
		}
	}
}
```

- [ ] **Step 5: Run to verify pass**

Run: `go test ./content/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum content/
git commit -m "feat(content): pure climate YAML loader merging over DefaultClimate"
```

---

## Task 7: `content` package — emote tables and `Pick`

**Files:** Create `content/emotes.go`, `content/emotes_test.go`.

- [ ] **Step 1: Write failing tests in `content/emotes_test.go`**

```go
package content

import (
	"testing"
	"testing/fstest"
)

const stormYAML = `weather: storm
outdoor:
  default:
    - "Thunder cracks directly overhead."
    - "A blinding fork of lightning splits the sky."
  forest:
    - "Wind tears at the branches; the whole canopy roars."
indoor:
  default:
    - "Rain hammers against the windows."
`

func loadTestTables(t *testing.T) Tables {
	t.Helper()
	fsys := fstest.MapFS{"emotes/storm.yaml": {Data: []byte(stormYAML)}}
	tables, err := LoadEmotes(fsys, "emotes")
	if err != nil {
		t.Fatal(err)
	}
	return tables
}

func TestPickSelectsByBiomeAndIndoor(t *testing.T) {
	tables := loadTestTables(t)
	first := func(n int) int { return 0 }

	if got := tables.Pick("storm", "forest", false, first); got != "Wind tears at the branches; the whole canopy roars." {
		t.Errorf("forest outdoor: %q", got)
	}
	if got := tables.Pick("storm", "desert", false, first); got != "Thunder cracks directly overhead." {
		t.Errorf("unknown biome should fall back to default: %q", got)
	}
	if got := tables.Pick("storm", "forest", true, first); got != "Rain hammers against the windows." {
		t.Errorf("indoor falls back to indoor default (never outdoor): %q", got)
	}
	if got := tables.Pick("fog", "forest", false, first); got != "" {
		t.Errorf("missing table must yield silence: %q", got)
	}
}

func TestPickUsesRoll(t *testing.T) {
	tables := loadTestTables(t)
	rolled := -1
	got := tables.Pick("storm", "default", false, func(n int) int { rolled = n; return 1 })
	if rolled != 2 {
		t.Errorf("roll should receive the line count, got %d", rolled)
	}
	if got != "A blinding fork of lightning splits the sky." {
		t.Errorf("roll result not honored: %q", got)
	}
}

func TestLoadEmotesRejectsMissingWeatherKey(t *testing.T) {
	fsys := fstest.MapFS{"emotes/bad.yaml": {Data: []byte("outdoor:\n  default: [\"x\"]\n")}}
	if _, err := LoadEmotes(fsys, "emotes"); err == nil {
		t.Fatal("emote table without 'weather' must be rejected")
	}
}

func TestLoadEmotesMissingDir(t *testing.T) {
	tables, err := LoadEmotes(fstest.MapFS{}, "emotes")
	if err != nil || len(tables) != 0 {
		t.Fatalf("missing dir should be empty tables, nil error: %v %v", tables, err)
	}
}
```

(`Pick`'s weather argument is `sim.WeatherType`, but the untyped string constants in these calls convert implicitly — no `sim` import needed in this test file.)

- [ ] **Step 2: Run to verify failure**

Run: `go test ./content/ -run 'TestPick|TestLoadEmotes'`
Expected: FAIL (`undefined: LoadEmotes` etc).

- [ ] **Step 3: Implement `content/emotes.go`**

```go
package content

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
	"gopkg.in/yaml.v2"
)

// Table holds the ambient lines for one weather type, keyed by biome with a
// "default" fallback, split outdoor/indoor (spec §9.4). Lines are uniform
// random picks (the spec's per-line weights are an unneeded refinement for
// shipped defaults; builders wanting bias can repeat a line).
type Table struct {
	Weather string              `yaml:"weather"`
	Outdoor map[string][]string `yaml:"outdoor"`
	Indoor  map[string][]string `yaml:"indoor"`
}

// Tables maps weather type -> emote table.
type Tables map[sim.WeatherType]Table

// ParseEmoteTable parses one emote table file.
func ParseEmoteTable(b []byte) (Table, error) {
	var t Table
	if err := yaml.Unmarshal(b, &t); err != nil {
		return Table{}, err
	}
	if t.Weather == "" {
		return Table{}, fmt.Errorf("emote table missing required 'weather' key")
	}
	return t, nil
}

// LoadEmotes loads every *.yaml emote table under dir in fsys, keyed by the
// table's weather type. A missing dir yields empty tables (silence). The first
// malformed file aborts with an error; the caller decides whether to fail soft.
func LoadEmotes(fsys fs.FS, dir string) (Tables, error) {
	tables := Tables{}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return tables, nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return tables, fmt.Errorf("%s: %w", e.Name(), err)
		}
		t, err := ParseEmoteTable(b)
		if err != nil {
			return tables, fmt.Errorf("%s: %w", e.Name(), err)
		}
		tables[sim.WeatherType(t.Weather)] = t
	}
	return tables, nil
}

// Pick selects one ambient line for (weather, biome, indoor), or "" when
// nothing matches. Fallbacks: exact biome -> "default" biome. Indoor never
// falls back to outdoor — silence beats wrong prose. roll(n) must return a
// value in [0,n); pass the engine's util.Rand (or a stub in tests) — NEVER the
// sim RNG, which must stay isolated from presentation randomness.
func (ts Tables) Pick(weather sim.WeatherType, biome string, indoor bool, roll func(int) int) string {
	t, ok := ts[weather]
	if !ok {
		return ""
	}
	section := t.Outdoor
	if indoor {
		section = t.Indoor
	}
	lines := section[biome]
	if len(lines) == 0 {
		lines = section["default"]
	}
	if len(lines) == 0 {
		return ""
	}
	return lines[roll(len(lines))]
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./content/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add content/emotes.go content/emotes_test.go
git commit -m "feat(content): emote tables with biome/indoor fallback selection"
```

---

## Task 8: Weather mutator spec data files

Eight specs — one per non-clear weather type in `sim.DefaultClimate()` (`overcast`, `rain`, `storm`, `fog`, `snow`, `blizzard`, `dust`, `heatwave`). Conventions verified against upstream (fact 3): id `weather-<type>`, filename `weather_<type>.yaml`, **no `respawnrate`**, `decayrate` as the §9.2 safety net (no `decayintoid` — see Verified facts #2). Buffs: blizzard → 31 (Freezing Snow), heatwave → 33 (Thirsty), players only — the gentle curated default of §9.5; everything else is flavor + light.

**Files:** Create `files/datafiles/mutators/weather_overcast.yaml`, `weather_rain.yaml`, `weather_storm.yaml`, `weather_fog.yaml`, `weather_snow.yaml`, `weather_blizzard.yaml`, `weather_dust.yaml`, `weather_heatwave.yaml`. Also create `content/moduledata_test.go` to validate them.

- [ ] **Step 1: Write the validation test `content/moduledata_test.go`** (it walks the real shipped files via a relative path — this works both standalone and in-checkout because the sync mirrors the whole module dir)

```go
package content

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

// fileNameFor mirrors the engine's util.ConvertForFilename: lowercase,
// apostrophes dropped, any rune outside [a-z0-9] becomes '_'. The plugin
// mutator loader requires each file to be named fileNameFor(mutatorid)+".yaml".
func fileNameFor(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		switch {
		case r == '\'':
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String() + ".yaml"
}

// TestShippedMutatorSpecs validates the data files the engine will load:
// parseable YAML, weather- namespaced ids, loader-compatible filenames, no
// respawnrate (it would fight the orchestrator and block purge-on-remove).
func TestShippedMutatorSpecs(t *testing.T) {
	dir := "../files/datafiles/mutators"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("mutator specs missing: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no mutator specs shipped")
	}
	for _, e := range entries {
		b, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		var spec map[string]any
		if err := yaml.Unmarshal(b, &spec); err != nil {
			t.Errorf("%s: bad YAML: %v", e.Name(), err)
			continue
		}
		id, _ := spec["mutatorid"].(string)
		if !strings.HasPrefix(id, "weather-") {
			t.Errorf("%s: mutatorid %q must be weather- namespaced", e.Name(), id)
		}
		if want := fileNameFor(id); e.Name() != want {
			t.Errorf("%s: engine loader requires filename %q for id %q", e.Name(), want, id)
		}
		if _, has := spec["respawnrate"]; has {
			t.Errorf("%s: weather mutators must not set respawnrate", e.Name())
		}
		if _, has := spec["decayrate"]; !has {
			t.Errorf("%s: weather mutators must set decayrate (self-heal safety net)", e.Name())
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./content/ -run TestShippedMutatorSpecs`
Expected: FAIL ("mutator specs missing").

- [ ] **Step 3: Create the eight spec files**

`files/datafiles/mutators/weather_overcast.yaml`:

```yaml
# Weather module: orchestrator-driven (no respawnrate). decayrate is the
# self-heal safety net only — the weather engine normally removes this first.
mutatorid: weather-overcast
namemodifier:
  behavior: append
  text: (overcast)
  colorpattern: gray
descriptionmodifier:
  behavior: append
  text: A flat gray ceiling of cloud hangs low overhead.
  colorpattern: gray
decayrate: 6 hours
```

`files/datafiles/mutators/weather_rain.yaml`:

```yaml
mutatorid: weather-rain
namemodifier:
  behavior: append
  text: (raining)
  colorpattern: blue
descriptionmodifier:
  behavior: append
  text: A steady rain falls, pattering off every surface.
  colorpattern: blue
decayrate: 4 hours
```

`files/datafiles/mutators/weather_storm.yaml`:

```yaml
mutatorid: weather-storm
namemodifier:
  behavior: append
  text: (storm-wracked)
  colorpattern: mute-dblue
descriptionmodifier:
  behavior: append
  text: Rain lashes down in sheets and thunder rolls across the sky.
  colorpattern: mute-dblue
alertmodifier:
  text: A storm rages overhead.
  colorpattern: mute-dblue
lightmod: -1
decayrate: 3 hours
```

`files/datafiles/mutators/weather_fog.yaml`:

```yaml
mutatorid: weather-fog
namemodifier:
  behavior: append
  text: (foggy)
  colorpattern: gray
descriptionmodifier:
  behavior: append
  text: A thick fog smothers everything beyond arm's reach.
  colorpattern: gray
lightmod: -1
decayrate: 3 hours
```

`files/datafiles/mutators/weather_snow.yaml`:

```yaml
mutatorid: weather-snow
namemodifier:
  behavior: append
  text: (snowing)
  colorpattern: frost
descriptionmodifier:
  behavior: append
  text: Snow drifts down steadily, softening every edge in white.
  colorpattern: frost
decayrate: 4 hours
```

`files/datafiles/mutators/weather_blizzard.yaml`:

```yaml
mutatorid: weather-blizzard
namemodifier:
  behavior: append
  text: (blizzard)
  colorpattern: frost
descriptionmodifier:
  behavior: append
  text: A howling blizzard whites out the world; the cold bites to the bone.
  colorpattern: frost
alertmodifier:
  text: A blizzard rages here!
  colorpattern: frost
lightmod: -1
playerbuffids: [ 31 ] # engine "Freezing Snow" (curated default; BuffsEnabled toggles)
decayrate: 2 hours
```

`files/datafiles/mutators/weather_dust.yaml`:

```yaml
mutatorid: weather-dust
namemodifier:
  behavior: append
  text: (dust-choked)
  colorpattern: brown
descriptionmodifier:
  behavior: append
  text: Wind-borne dust scours the air and grits between your teeth.
  colorpattern: brown
lightmod: -1
decayrate: 3 hours
```

`files/datafiles/mutators/weather_heatwave.yaml`:

```yaml
mutatorid: weather-heatwave
namemodifier:
  behavior: append
  text: (sweltering)
  colorpattern: embers
descriptionmodifier:
  behavior: append
  text: The air shimmers with oppressive, relentless heat.
  colorpattern: embers
playerbuffids: [ 33 ] # engine "Thirsty" (curated default; BuffsEnabled toggles)
decayrate: 6 hours
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./content/ -run TestShippedMutatorSpecs -v`
Expected: PASS (8 files validated).

- [ ] **Step 5: Commit**

```bash
git add files/datafiles/mutators/ content/moduledata_test.go
git commit -m "feat(data): weather mutator specs for the eight default weather types"
```

---

## Task 9: Default emote tables

One table per weather type. Defaults speak through the `default` biome key; `storm` and `rain` include a `forest` variant as the worked example builders copy. Indoor sections fulfill the "you hear it inside" requirement (spec §9.4).

**Files:** Create `files/datafiles/emotes/overcast.yaml`, `rain.yaml`, `storm.yaml`, `fog.yaml`, `snow.yaml`, `blizzard.yaml`, `dust.yaml`, `heatwave.yaml`. Modify `content/moduledata_test.go`.

- [ ] **Step 1: Add a shipped-emotes validation test to `content/moduledata_test.go`**

```go
// TestShippedEmoteTables validates the default emote tables: parseable, the
// weather key matches the filename stem, and every type has at least one
// outdoor-default and one indoor-default line.
func TestShippedEmoteTables(t *testing.T) {
	dir := "../files/datafiles/emotes"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("emote tables missing: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no emote tables shipped")
	}
	for _, e := range entries {
		b, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		table, err := ParseEmoteTable(b)
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		if want := table.Weather + ".yaml"; e.Name() != want {
			t.Errorf("%s: filename should match weather key (%s)", e.Name(), want)
		}
		if len(table.Outdoor["default"]) == 0 {
			t.Errorf("%s: needs at least one outdoor default line", e.Name())
		}
		if len(table.Indoor["default"]) == 0 {
			t.Errorf("%s: needs at least one indoor default line", e.Name())
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./content/ -run TestShippedEmoteTables`
Expected: FAIL ("emote tables missing").

- [ ] **Step 3: Create the eight emote files**

`files/datafiles/emotes/overcast.yaml`:

```yaml
weather: overcast
outdoor:
  default:
    - "The clouds overhead thicken, swallowing what light there was."
    - "A dull gray sky presses down on the world."
indoor:
  default:
    - "The light through the openings dims as clouds gather outside."
```

`files/datafiles/emotes/rain.yaml`:

```yaml
weather: rain
outdoor:
  default:
    - "Rain patters down steadily around you."
    - "A cold runnel of rainwater finds its way down your neck."
    - "Puddles widen and merge underfoot."
  forest:
    - "Rain drips from leaf to leaf, a thousand small drumbeats."
indoor:
  default:
    - "Rain drums on the roof overhead."
    - "Water trickles past outside; somewhere a slow drip finds its rhythm."
```

`files/datafiles/emotes/storm.yaml`:

```yaml
weather: storm
outdoor:
  default:
    - "A blinding fork of lightning splits the sky."
    - "Thunder cracks directly overhead."
    - "The wind rises to a shriek, driving the rain sideways."
  forest:
    - "Wind tears at the branches; the whole canopy roars."
indoor:
  default:
    - "Rain hammers against the walls and shutters."
    - "Thunder rattles the timbers; the whole structure groans."
```

`files/datafiles/emotes/fog.yaml`:

```yaml
weather: fog
outdoor:
  default:
    - "The fog coils and shifts, briefly revealing shapes that aren't there."
    - "Sound arrives strangely muffled through the gray."
indoor:
  default:
    - "Tendrils of fog seep in around the edges of the room."
```

`files/datafiles/emotes/snow.yaml`:

```yaml
weather: snow
outdoor:
  default:
    - "Fat snowflakes spiral down in lazy silence."
    - "Snow creaks and squeaks beneath every footstep."
indoor:
  default:
    - "Snowflakes whirl past outside; the cold radiates from the walls."
```

`files/datafiles/emotes/blizzard.yaml`:

```yaml
weather: blizzard
outdoor:
  default:
    - "The blizzard howls, erasing the world beyond a few paces."
    - "Wind-driven ice crystals sting every inch of exposed skin."
indoor:
  default:
    - "The blizzard screams past outside; drafts knife through every crack."
```

`files/datafiles/emotes/dust.yaml`:

```yaml
weather: dust
outdoor:
  default:
    - "A gritty wall of dust rolls through, stinging your eyes."
    - "The wind hisses, dragging veils of dust across the ground."
indoor:
  default:
    - "Fine dust sifts in from outside, settling over everything."
```

`files/datafiles/emotes/heatwave.yaml`:

```yaml
weather: heatwave
outdoor:
  default:
    - "Heat radiates up from the ground in shimmering waves."
    - "The air is thick and stifling; sweat beads instantly."
indoor:
  default:
    - "The trapped air is stiflingly hot and utterly still."
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./content/`
Expected: PASS (all content tests).

- [ ] **Step 5: Commit**

```bash
git add files/datafiles/emotes/ content/moduledata_test.go
git commit -m "feat(data): default ambient emote tables for the eight weather types"
```

---

## Task 10: Engine state persistence codec

**Files:** Create `engine/state.go`, `engine/state_test.go`.

> **From here on, packages compile only in the checkout.** After editing, sync and test from there:
> `pwsh scripts/sync-to-checkout.ps1 -Checkout "$HOME\workspace\GoMud"` then `Push-Location "$HOME\workspace\GoMud"; go test ./modules/weather/engine/; Pop-Location`

- [ ] **Step 1: Write failing test `engine/state_test.go`**

```go
package engine

import (
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

func TestStateCodecRoundTrip(t *testing.T) {
	s := sim.NewState(42)
	s.Fronts = []sim.Front{{Id: 1, Type: "storm", Zone: "A", Intensity: 0.7, History: []string{"B"}}}
	s.Weather = map[string]sim.WeatherType{"A": "storm", "B": sim.Clear}
	s.Round = 99

	b, err := EncodeState(s)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := DecodeState(b)
	if !ok {
		t.Fatal("round-trip decode failed")
	}
	if got.Round != 99 || got.RNGState != 42 || len(got.Fronts) != 1 || got.Weather["A"] != "storm" {
		t.Fatalf("state mangled: %+v", got)
	}
}

func TestDecodeStateRejectsBadInput(t *testing.T) {
	if _, ok := DecodeState(nil); ok {
		t.Error("nil must not decode")
	}
	if _, ok := DecodeState([]byte("not json")); ok {
		t.Error("garbage must not decode")
	}
	if _, ok := DecodeState([]byte(`{"version":999,"state":{}}`)); ok {
		t.Error("future version must not decode (forces a clean fresh state)")
	}
}
```

- [ ] **Step 2: Sync + run to verify failure**

Run (from the checkout after sync): `go test ./modules/weather/engine/ -run TestStateCodec`
Expected: FAIL (`undefined: EncodeState`).

- [ ] **Step 3: Implement `engine/state.go`**

```go
package engine

import (
	"encoding/json"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// StateIdentifier is the plugin-storage key for the persisted simulation state
// (plugin.WriteBytes / ReadBytes).
const StateIdentifier = "simstate"

// StateVersion is bumped whenever the persisted layout changes; a mismatched
// version is discarded (the module re-seeds) rather than migrated.
const StateVersion = 1

// persistedState wraps sim.State in a versioned envelope so the sim package
// stays free of persistence concerns.
type persistedState struct {
	Version int       `json:"version"`
	State   sim.State `json:"state"`
}

// EncodeState serializes simulation state for plugin storage.
func EncodeState(s sim.State) ([]byte, error) {
	return json.MarshalIndent(persistedState{Version: StateVersion, State: s}, "", "  ")
}

// DecodeState parses persisted state bytes, reporting ok=false for absent,
// unparseable, or version-mismatched data (caller starts fresh).
func DecodeState(b []byte) (sim.State, bool) {
	if len(b) == 0 {
		return sim.State{}, false
	}
	var ps persistedState
	if err := json.Unmarshal(b, &ps); err != nil {
		return sim.State{}, false
	}
	if ps.Version != StateVersion {
		return sim.State{}, false
	}
	return ps.State, true
}
```

- [ ] **Step 4: Sync + run to verify pass**

Run (checkout): `go test ./modules/weather/engine/`
Expected: PASS.

- [ ] **Step 5: Commit (in THIS repo)**

```bash
git add engine/state.go engine/state_test.go
git commit -m "feat(engine): versioned persistence codec for simulation state"
```

---

## Task 11: Mutator application — `Apply`, `Reconcile`, `StripBuffs`

The heart of M3 (spec §9.1 primary strategy): zone-wide mutator add/remove driven by `StateDiff`, plus boot-time reconciliation and the `BuffsEnabled` toggle. The testable core works against a tiny interface satisfied by `*mutators.MutatorList` and by test fakes — the real `MutatorList.Add` consults the global spec registry, which tests can't populate.

**Files:** Create `engine/apply.go`, `engine/apply_test.go`.

- [ ] **Step 1: Write failing tests `engine/apply_test.go`**

```go
package engine

import (
	"reflect"
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// fakeMutatorSet records operations; "known:" prefixed ids Add successfully.
type fakeMutatorSet struct {
	ops  []string
	live map[string]bool
}

func newFake(live ...string) *fakeMutatorSet {
	f := &fakeMutatorSet{live: map[string]bool{}}
	for _, id := range live {
		f.live[id] = true
	}
	return f
}
func (f *fakeMutatorSet) Add(id string) bool {
	f.ops = append(f.ops, "add:"+id)
	f.live[id] = true
	return true
}
func (f *fakeMutatorSet) Remove(id string) bool {
	f.ops = append(f.ops, "remove:"+id)
	delete(f.live, id)
	return true
}
func (f *fakeMutatorSet) Has(id string) bool { return f.live[id] }

func TestMutatorIdFor(t *testing.T) {
	if got := MutatorIdFor("storm"); got != "weather-storm" {
		t.Errorf("storm -> %q", got)
	}
	if MutatorIdFor(sim.Clear) != "" || MutatorIdFor("") != "" {
		t.Error("clear and unset must map to no mutator")
	}
}

func TestApplyChange(t *testing.T) {
	cases := []struct {
		name     string
		live     []string
		from, to sim.WeatherType
		wantOps  []string
	}{
		{"calm to storm", nil, "", "storm", []string{"add:weather-storm"}},
		{"clear to rain", nil, sim.Clear, "rain", []string{"add:weather-rain"}},
		{"storm to rain", []string{"weather-storm"}, "storm", "rain", []string{"remove:weather-storm", "add:weather-rain"}},
		{"storm to clear", []string{"weather-storm"}, "storm", sim.Clear, []string{"remove:weather-storm"}},
		{"already live: no duplicate add", []string{"weather-rain"}, "", "rain", nil},
	}
	for _, c := range cases {
		f := newFake(c.live...)
		applyChange(f, c.from, c.to)
		if !reflect.DeepEqual(f.ops, c.wantOps) {
			t.Errorf("%s: ops = %v, want %v", c.name, f.ops, c.wantOps)
		}
	}
}

func TestReconcileZone(t *testing.T) {
	// Stale storm + stray fog live; target is rain.
	f := newFake("weather-storm", "weather-fog")
	reconcileZone(f, []string{"weather-storm", "weather-fog"}, "rain")
	want := []string{"remove:weather-storm", "remove:weather-fog", "add:weather-rain"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}

	// Target already live: only the stray is removed.
	f = newFake("weather-rain", "weather-fog")
	reconcileZone(f, []string{"weather-rain", "weather-fog"}, "rain")
	want = []string{"remove:weather-fog"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}

	// Calm target: everything weather-* goes.
	f = newFake("weather-snow")
	reconcileZone(f, []string{"weather-snow"}, sim.Clear)
	want = []string{"remove:weather-snow"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}
}
```

- [ ] **Step 2: Sync + run to verify failure**

Run (checkout): `go test ./modules/weather/engine/ -run 'TestMutatorIdFor|TestApplyChange|TestReconcileZone'`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement `engine/apply.go`**

```go
package engine

import (
	"strings"

	"github.com/GoMudEngine/GoMud/internal/mudlog"
	"github.com/GoMudEngine/GoMud/internal/mutators"
	"github.com/GoMudEngine/GoMud/internal/rooms"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// WeatherMutatorPrefix namespaces every mutator this module owns.
const WeatherMutatorPrefix = "weather-"

// mutatorSet is the slice of MutatorList behavior the applier needs; satisfied
// by *mutators.MutatorList and by test fakes (the real Add consults the global
// spec registry, so unit tests fake at this seam).
type mutatorSet interface {
	Add(string) bool
	Remove(string) bool
	Has(string) bool
}

// MutatorIdFor maps a sim weather type to its mutator id; "" for clear/unset
// (calm weather is the absence of a weather mutator).
func MutatorIdFor(w sim.WeatherType) string {
	if w == "" || w == sim.Clear {
		return ""
	}
	return WeatherMutatorPrefix + string(w)
}

// applyChange applies one zone weather transition. Add is guarded by Has
// because MutatorList.Add appends a duplicate entry when the mutator is
// already live. Returns false when the target spec id is unknown (data file
// missing or failed to load).
func applyChange(ms mutatorSet, from, to sim.WeatherType) bool {
	if id := MutatorIdFor(from); id != "" {
		ms.Remove(id)
	}
	if id := MutatorIdFor(to); id != "" {
		if ms.Has(id) {
			return true
		}
		return ms.Add(id)
	}
	return true
}

// reconcileZone forces a zone's weather mutators to exactly match target:
// every live weather-* id except the target is removed; the target is added if
// absent. current must hold the zone's live weather-* mutator ids.
func reconcileZone(ms mutatorSet, current []string, target sim.WeatherType) bool {
	want := MutatorIdFor(target)
	hasWant := false
	for _, id := range current {
		if id == want {
			hasWant = true
			continue
		}
		ms.Remove(id)
	}
	if want == "" || hasWant {
		return true
	}
	return ms.Add(want)
}

// warnedMutators tracks unknown-spec warnings so each id logs once. Touched
// only from the single game-loop goroutine — no mutex (see context.md).
var warnedMutators = map[string]bool{}

func warnUnknownMutator(w sim.WeatherType) {
	id := MutatorIdFor(w)
	if id == "" || warnedMutators[id] {
		return
	}
	warnedMutators[id] = true
	mudlog.Warn("Weather: no mutator spec loaded for weather type", "mutatorId", id)
}

// Apply walks a StateDiff and applies each change to its zone's zone-wide
// mutator list (spec §9.1 primary strategy). Zones missing from the live world
// (stale graph) are skipped.
func Apply(diff sim.StateDiff) {
	for _, ch := range diff.Changes {
		zc := rooms.GetZoneConfig(ch.Zone)
		if zc == nil {
			continue
		}
		if !applyChange(&zc.Mutators, ch.From, ch.To) {
			warnUnknownMutator(ch.To)
		}
	}
}

// Reconcile forces every zone's live weather mutators to match the resolved
// weather map — used at boot after restoring persisted state (zone mutators do
// not survive reboots) and after a graph rebuild.
func Reconcile(weather map[sim.ZoneId]sim.WeatherType) {
	for zone, w := range weather {
		zc := rooms.GetZoneConfig(zone)
		if zc == nil {
			continue
		}
		var current []string
		for _, mut := range zc.Mutators.GetActive() {
			if strings.HasPrefix(mut.MutatorId, WeatherMutatorPrefix) {
				current = append(current, mut.MutatorId)
			}
		}
		if !reconcileZone(&zc.Mutators, current, w) {
			warnUnknownMutator(w)
		}
	}
}

// StripBuffs clears the buff id lists on every loaded weather-* mutator spec —
// the BuffsEnabled=false path. GetMutatorSpec returns the registry's live
// pointer, so this affects all future applications. Returns the count stripped.
func StripBuffs() int {
	n := 0
	for _, id := range mutators.GetAllMutatorIds() {
		if !strings.HasPrefix(id, WeatherMutatorPrefix) {
			continue
		}
		if spec := mutators.GetMutatorSpec(id); spec != nil {
			spec.PlayerBuffIds, spec.MobBuffIds, spec.NativeBuffIds = nil, nil, nil
			n++
		}
	}
	return n
}
```

- [ ] **Step 4: Sync + run to verify pass**

Run (checkout): `go test ./modules/weather/engine/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add engine/apply.go engine/apply_test.go
git commit -m "feat(engine): StateDiff mutator application, boot reconcile, buff stripping"
```

---

## Task 12: Weather clock helpers

**Files:** Create `engine/clock.go`, `engine/clock_test.go`.

- [ ] **Step 1: Write failing test `engine/clock_test.go`** (only the pure formatter is unit-testable; the gametime calls are thin verified glue)

```go
package engine

import "testing"

func TestTickPeriod(t *testing.T) {
	if got := TickPeriod(1); got != "1 hours" {
		t.Errorf("TickPeriod(1) = %q", got)
	}
	if got := TickPeriod(6); got != "6 hours" {
		t.Errorf("TickPeriod(6) = %q", got)
	}
	if got := TickPeriod(0); got != "1 hours" {
		t.Errorf("TickPeriod(0) must clamp to 1, got %q", got)
	}
	if got := TickPeriod(-3); got != "1 hours" {
		t.Errorf("TickPeriod(-3) must clamp to 1, got %q", got)
	}
}
```

- [ ] **Step 2: Sync + run to verify failure**

Run (checkout): `go test ./modules/weather/engine/ -run TestTickPeriod`
Expected: FAIL (`undefined: TickPeriod`).

- [ ] **Step 3: Implement `engine/clock.go`**

```go
package engine

import (
	"fmt"

	"github.com/GoMudEngine/GoMud/internal/gametime"
	"github.com/GoMudEngine/GoMud/internal/util"
)

// TickPeriod renders a game-hour count as a gametime.AddPeriod period string
// ("N hours"); values < 1 clamp to 1. AddPeriod matches units on their first
// three letters, so the plural form is always valid.
func TickPeriod(hours int) string {
	if hours < 1 {
		hours = 1
	}
	return fmt.Sprintf("%d hours", hours)
}

// NextTickRound returns the round number at which the next weather tick is due,
// one period from now (spec §9.3, per engine-author guidance: schedule a target
// round via gametime instead of counting rounds by hand).
func NextTickRound(period string) uint64 {
	return gametime.GetDate().AddPeriod(period)
}

// CurrentRound exposes the live round counter to the module root.
func CurrentRound() uint64 {
	return util.GetRoundCount()
}
```

- [ ] **Step 4: Sync + run to verify pass**

Run (checkout): `go test ./modules/weather/engine/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add engine/clock.go engine/clock_test.go
git commit -m "feat(engine): gametime-based weather tick scheduling helpers"
```

---

## Task 13: Ambient emote emitter

**Files:** Create `engine/emotes.go`.

- [ ] **Step 1: Implement `engine/emotes.go`** (thin engine glue over the tested `content.Tables.Pick` — same verification posture as `WorldReader`: compile + smoke test; the selection logic already has standalone tests)

```go
package engine

import (
	"github.com/GoMudEngine/GoMud/internal/rooms"
	"github.com/GoMudEngine/GoMud/modules/weather/content"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// EmitAmbient sends one ambient weather line into each occupied room whose
// zone currently has non-calm weather (spec §9.4, EmoteMode "module"). The
// room's biome picks the table variant; indoor biomes get the indoor section.
// roll is the presentation RNG (pass util.Rand) — NEVER the sim RNG, which
// must stay isolated from presentation randomness. Returns lines sent.
func EmitAmbient(weather map[sim.ZoneId]sim.WeatherType, tables content.Tables, roll func(int) int) int {
	sent := 0
	for _, roomId := range rooms.GetRoomsWithPlayers() {
		room := rooms.LoadRoom(roomId)
		if room == nil {
			continue
		}
		w := weather[room.Zone]
		if w == "" || w == sim.Clear {
			continue
		}
		biomeId := ""
		if b := room.GetBiome(); b != nil {
			biomeId = b.BiomeId
		}
		line := tables.Pick(w, biomeId, !isOutdoorBiome(biomeId), roll)
		if line == "" {
			continue
		}
		room.SendText(line)
		sent++
	}
	return sent
}
```

- [ ] **Step 2: Sync + verify it compiles and the engine suite stays green**

Run (checkout): `go build ./modules/weather/... ; go test ./modules/weather/engine/`
Expected: builds, PASS.

- [ ] **Step 3: Commit**

```bash
git add engine/emotes.go
git commit -m "feat(engine): ambient emote emitter for occupied rooms"
```

---

## Task 14: Full M3 config

**Files:** Modify `weather_config.go`, `weather_config_test.go`, `files/data-overlays/config.yaml`.

- [ ] **Step 1: Extend the tests in `weather_config_test.go`** (keep existing tests; add)

```go
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
}
```

- [ ] **Step 2: Sync + run to verify failure**

Run (checkout): `go test ./modules/weather/ -run TestBuildConfig`
Expected: FAIL (missing fields / helpers).

- [ ] **Step 3: Rewrite `weather_config.go`**

```go
package weather

import (
	"strconv"
	"strings"

	"github.com/GoMudEngine/GoMud/internal/plugins"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// EmoteModeModule / EmoteModeTagOnly are the two §9.4 delivery modes.
const (
	EmoteModeModule  = "module"   // the module emits ambient lines itself
	EmoteModeTagOnly = "tag-only" // mutator tags only; the world's scripts react
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
	MaxActiveFronts    int     // global front budget
	SpawnRateScale     float64 // multiplier on the default spawn chance
	EmoteMode          string  // EmoteModeModule | EmoteModeTagOnly
	EmoteEveryRounds   int     // ambient emote cadence in rounds (jittered ±25%, >= 5)
	BuffsEnabled       bool    // false strips buff ids from weather mutator specs
	Persist            bool    // save/restore fronts + RNG across reboots
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
	c := Config{
		Enabled:            asBool(get("Enabled")),
		IncludeSecretExits: boolOr(get("IncludeSecretExits"), true),
		RebuildGraphOnBoot: asBool(get("RebuildGraphOnBoot")),

		Seed:               uint64(intOr(get("Seed"), 0)),
		TickEveryGameHours: intOr(get("TickEveryGameHours"), 1),
		MaxActiveFronts:    intOr(get("MaxActiveFronts"), 8),
		SpawnRateScale:     floatOr(get("SpawnRateScale"), 1.0),
		EmoteMode:          strings.ToLower(stringOr(get("EmoteMode"), EmoteModeModule)),
		EmoteEveryRounds:   intOr(get("EmoteEveryRounds"), 20),
		BuffsEnabled:       boolOr(get("BuffsEnabled"), true),
		Persist:            boolOr(get("Persist"), true),
	}
	if c.TickEveryGameHours < 1 {
		c.TickEveryGameHours = 1
	}
	if c.EmoteEveryRounds < 5 {
		c.EmoteEveryRounds = 5
	}
	if c.SpawnRateScale < 0 {
		c.SpawnRateScale = 0
	}
	if c.EmoteMode != EmoteModeModule && c.EmoteMode != EmoteModeTagOnly {
		c.EmoteMode = EmoteModeModule
	}
	return c
}

// simConfig maps module config onto the simulation's tuning knobs.
func (c Config) simConfig() sim.Config {
	sc := sim.DefaultConfig()
	if c.MaxActiveFronts > 0 {
		sc.MaxActiveFronts = c.MaxActiveFronts
	}
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
```

- [ ] **Step 4: Update `files/data-overlays/config.yaml`**

```yaml
# Modules.weather.* defaults. This overlay overrides the base config.yaml; do
# NOT add a Modules: weather: block to config-overrides.yaml (it will not merge).
Enabled: true
IncludeSecretExits: true   # count secret/hidden exits as zone adjacency
RebuildGraphOnBoot: false  # false = use the cached graph if present
Seed: 0                    # 0 = derive a stable seed from the world's zone names
TickEveryGameHours: 1      # weather-simulation cadence (game hours)
MaxActiveFronts: 8         # global front budget
SpawnRateScale: 1.0        # multiplier on spawn pressure (0 disables new fronts)
EmoteMode: module          # module = we emit ambiance | tag-only = your scripts react to mutator tags
EmoteEveryRounds: 20       # ambient emote cadence in rounds (jittered +/-25%)
BuffsEnabled: true         # apply the curated default buffs carried by weather mutators
Persist: true              # save/restore fronts + RNG across reboots
```

- [ ] **Step 5: Sync + run to verify pass**

Run (checkout): `go test ./modules/weather/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add weather_config.go weather_config_test.go files/data-overlays/config.yaml
git commit -m "feat(config): full M3 config surface with coercion, defaults and clamps"
```

---

## Task 15: Lifecycle wiring — startup, tick loop, persistence

**Files:** Create `weather_tick.go`; modify `weather.go`.

- [ ] **Step 1: Extend `weatherModule` and `onLoad`/`onNewRound` in `weather.go`**

Replace the struct and the two functions (leave `init`, `loadOrBuildGraph`, `rebuildGraph`, `sendLine` as they are for now — `rebuildGraph` gains one line):

```go
// weatherModule holds the plugin handle, resolved config, the geography graph,
// and the live simulation (state/climate/emote tables/schedule). All fields are
// touched only from the single game-loop goroutine — no synchronization needed.
type weatherModule struct {
	plug    *plugins.Plugin
	cfg     Config
	graph   *sim.Graph
	started bool

	simReady  bool   // graph + content + state loaded; ticking enabled
	simCfg    sim.Config
	climate   sim.Climate
	tables    content.Tables
	state     sim.State
	nextTick  uint64 // round number when the next weather tick fires
	nextEmote uint64 // round number when the next ambient emote pass fires
}
```

```go
// onLoad loads config and registers the command, exports, and listeners. World
// crawling and sim startup are deferred to the first NewRound (engine-specific
// onLoad timing vs world load).
func (m *weatherModule) onLoad() {
	m.cfg = loadConfig(m.plug)
	if !m.cfg.Enabled {
		return
	}
	m.plug.AddUserCommand(`weather`, m.cmdWeather, false, false) // player command; admin subcommands gated in-handler
	m.registerExports()
	m.plug.Callbacks.SetOnSave(m.onSave)
	events.RegisterListener(events.NewRound{}, m.onNewRound)
}

// onNewRound drives everything round-based: one-time startup, the jittered
// ambient-emote pass, and the coarse weather tick.
func (m *weatherModule) onNewRound(e events.Event) events.ListenerReturn {
	evt, ok := e.(events.NewRound)
	if !ok {
		return events.Continue
	}
	if !m.started {
		m.started = true
		m.loadOrBuildGraph()
		m.startSim(evt.RoundNumber)
	}
	if !m.simReady {
		return events.Continue
	}
	if m.cfg.EmoteMode == EmoteModeModule && evt.RoundNumber >= m.nextEmote {
		engine.EmitAmbient(m.state.Weather, m.tables, util.Rand)
		m.scheduleEmote(evt.RoundNumber)
	}
	if evt.RoundNumber >= m.nextTick {
		m.tick(evt.RoundNumber)
	}
	return events.Continue
}
```

In `rebuildGraph`, after the success log line, add (so a `weather rebuild` that fixes a failed boot crawl also starts the sim):

```go
	m.startSim(util.GetRoundCount())
```

followed by a guarded "if m.simReady { engine.Reconcile(m.state.Weather) }" so a rebuild while running re-asserts mutators against the new graph immediately.

Add `"github.com/GoMudEngine/GoMud/modules/weather/content"` to the imports.

- [ ] **Step 2: Create `weather_tick.go`**

```go
package weather

import (
	"github.com/GoMudEngine/GoMud/internal/mudlog"
	"github.com/GoMudEngine/GoMud/internal/util"
	"github.com/GoMudEngine/GoMud/modules/weather/content"
	"github.com/GoMudEngine/GoMud/modules/weather/engine"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// startSim initializes the simulation once a geography graph exists: load
// content, restore-or-seed state, reconcile the world's mutators to it, and
// schedule the first tick/emote. Safe to call again (no-ops when ready); a
// later successful 'weather rebuild' can start a sim that failed at boot.
// Degrades gracefully: with no graph the module logs once and stays idle
// (spec §2.3.5 / §10 "graceful degradation").
func (m *weatherModule) startSim(round uint64) {
	if m.simReady {
		return
	}
	if m.graph == nil {
		mudlog.Warn("Weather: no geography graph; simulation idle (fix the world and run 'weather rebuild')")
		return
	}
	m.simCfg = m.cfg.simConfig()
	m.loadContent()
	if !m.cfg.BuffsEnabled {
		n := engine.StripBuffs()
		mudlog.Info("Weather: buffs disabled by config", "specsStripped", n)
	}
	m.loadOrInitState(round)
	engine.Reconcile(m.state.Weather)
	m.nextTick = engine.NextTickRound(engine.TickPeriod(m.cfg.TickEveryGameHours))
	m.scheduleEmote(round)
	m.simReady = true
}

// loadContent loads climate overrides and emote tables from the module's
// embedded files. Both fail soft: defaults / silence plus a warning.
func (m *weatherModule) loadContent() {
	climate, err := content.LoadClimate(files, "files/datafiles/climate")
	if err != nil {
		mudlog.Warn("Weather: climate overrides failed to load; using defaults", "error", err)
	}
	m.climate = climate

	tables, err := content.LoadEmotes(files, "files/datafiles/emotes")
	if err != nil {
		mudlog.Warn("Weather: emote tables failed to load", "error", err)
	}
	m.tables = tables
}

// loadOrInitState restores persisted simulation state, or seeds a fresh run
// (configured Seed, else derived stably from the world's zone names).
func (m *weatherModule) loadOrInitState(round uint64) {
	if m.cfg.Persist {
		if b, err := m.plug.ReadBytes(engine.StateIdentifier); err == nil {
			if s, ok := engine.DecodeState(b); ok {
				m.state = s
				mudlog.Info("Weather: restored simulation state",
					"fronts", len(s.Fronts), "savedRound", s.Round)
				return
			}
		}
	}
	seed := m.cfg.Seed
	if seed == 0 {
		seed = sim.DeriveSeed(m.graph)
	}
	m.state = sim.NewState(seed)
	mudlog.Info("Weather: fresh simulation state", "seed", seed, "currentRound", round)
}

// tick advances the simulation one coarse step and applies the diff.
func (m *weatherModule) tick(round uint64) {
	next, diff := sim.Step(m.state, m.graph, m.climate, m.simCfg, sim.Clock{Round: round})
	m.state = next
	_ = diff // per-zone changes are implied by the reconcile below
	engine.Reconcile(m.state.Weather)
	m.persistState()
	m.nextTick = engine.NextTickRound(engine.TickPeriod(m.cfg.TickEveryGameHours))
}

// persistState writes the current state to plugin storage (cheap: a few KB
// once per game hour). Also invoked from the engine's save callback.
func (m *weatherModule) persistState() {
	if !m.cfg.Persist {
		return
	}
	b, err := engine.EncodeState(m.state)
	if err != nil {
		mudlog.Error("Weather: state encode failed", "error", err)
		return
	}
	if err := m.plug.WriteBytes(engine.StateIdentifier, b); err != nil {
		mudlog.Error("Weather: state save failed", "error", err)
	}
}

// onSave is the plugins.Save() hook (autosave, shutdown, copyover).
func (m *weatherModule) onSave() {
	if m.simReady {
		m.persistState()
	}
}

// scheduleEmote picks the next ambient-emote round: the configured cadence
// jittered by ±25% so ambiance doesn't metronome.
func (m *weatherModule) scheduleEmote(round uint64) {
	every := m.cfg.EmoteEveryRounds
	delta := every
	if jitter := every / 4; jitter > 0 {
		delta += util.Rand(2*jitter+1) - jitter
	}
	if delta < 1 {
		delta = 1
	}
	m.nextEmote = round + uint64(delta)
}
```

(Reconcile supersedes Apply everywhere module state reaches the engine — tick, spawn/clear commands, exported SpawnFront, and post-rebuild — so engine-side decayrate drift always self-corrects and there is exactly one application path. Apply(diff) survives only as the tested low-level primitive.)

- [ ] **Step 3: Stub the not-yet-written references so the package compiles** — `registerExports` and the `cmdWeather` rework land in Tasks 16–17. For THIS task's commit, add a minimal `registerExports` placeholder in `weather_tick.go`... **No — placeholders are forbidden.** Instead: implement Tasks 15–17 as one *build unit* but keep commits separate by writing this task's files now and running the build only at the end of Task 17 if `go build` fails here. Practical sequencing: complete Steps 1–2 above, then immediately do Task 16 and Task 17, then run the verification step below once — committing each task's files separately in order (`git add` only that task's files). The existing `cmdWeather` from M1b still compiles against this task alone, and `registerExports` arrives in Task 17 — so after Task 15 alone the build FAILS on the one `m.registerExports()` call. Acceptable mid-sequence state; do NOT push between these commits.

- [ ] **Step 4: Verify (after Task 17 is in place) and commit this task's files**

Run (checkout, after sync): `go build ./modules/weather/...`
Expected: builds clean once Tasks 16–17 are done.

```bash
git add weather.go weather_tick.go
git commit -m "feat(weather): weather clock, sim lifecycle, persistence wiring"
```

---

## Task 16: Command set

Player-facing `weather` plus admin subcommands (spec §9.6). Command code moves to its own file; `weather.go` keeps lifecycle only.

**Files:** Create `weather_commands.go`. Modify `weather.go` (delete `cmdWeather`, `printSummary`, `printGraphForZone` from it).

- [ ] **Step 1: Move and rework commands into `weather_commands.go`**

```go
package weather

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/GoMudEngine/GoMud/internal/events"
	"github.com/GoMudEngine/GoMud/internal/rooms"
	"github.com/GoMudEngine/GoMud/internal/users"
	"github.com/GoMudEngine/GoMud/modules/weather/engine"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

const adminUsage = "Weather admin subcommands: zones | fronts | spawn <type> <zone> [intensity 0..1] | clear [zone] | graph [zone] | rebuild | status"

// cmdWeather is the weather command. Bare `weather` shows local conditions to
// any player; everything else is admin/mod gated (HasRolePermission: admins
// always pass, mods need the granted "weather" permission key).
func (m *weatherModule) cmdWeather(rest string, user *users.UserRecord, room *rooms.Room, flags events.EventFlag) (bool, error) {
	args := strings.Fields(rest)
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	if sub == "" || !user.HasRolePermission(`weather`, true) {
		m.printLocalWeather(user, room)
		return true, nil
	}

	switch sub {
	case "zones":
		m.printZones(user)
	case "fronts":
		m.printFronts(user)
	case "spawn":
		m.cmdSpawn(user, args[1:])
	case "clear":
		m.cmdClear(user, args[1:])
	case "graph":
		zone := strings.TrimSpace(rest[len(args[0]):])
		if zone == "" && room != nil {
			zone = room.Zone
		}
		m.printGraphForZone(user, zone)
	case "rebuild":
		m.rebuildGraph()
		if m.graph == nil {
			sendLine(user, "Weather: graph rebuild failed (see server log).")
			return true, nil
		}
		sendLine(user, fmt.Sprintf("Weather: rebuilt graph — %d zones, %d edges, %d components.",
			len(m.graph.Nodes), len(m.graph.Edges), m.graph.Components))
	case "status":
		m.printStatus(user)
	default:
		sendLine(user, adminUsage)
	}
	return true, nil
}

// printLocalWeather shows the weather where the user stands (the player view).
func (m *weatherModule) printLocalWeather(user *users.UserRecord, room *rooms.Room) {
	if !m.simReady || room == nil {
		sendLine(user, "The weather seems entirely unremarkable.")
		return
	}
	w := m.state.Weather[room.Zone]
	if w == "" {
		w = sim.Clear
	}
	sendLine(user, fmt.Sprintf("The weather in %s is %s.", room.Zone, w))
	if covers := sim.Covering(m.graph, m.state.Fronts, m.simCfg, room.Zone); len(covers) > 0 {
		c := covers[0]
		where := "centered here"
		if c.Hops > 0 {
			where = fmt.Sprintf("%d zone(s) away", c.Hops)
		}
		sendLine(user, fmt.Sprintf("  A %s system (front #%d) is %s — intensity %.2f, felt here at %.2f.",
			c.Front.Type, c.Front.Id, where, c.Front.Intensity, c.Effective))
	}
}

func (m *weatherModule) printZones(user *users.UserRecord) {
	if !m.simReady {
		sendLine(user, "Weather: simulation not running.")
		return
	}
	zones := m.graph.Zones()
	sendLine(user, fmt.Sprintf("Current weather (%d zones):", len(zones)))
	for _, z := range zones {
		sendLine(user, fmt.Sprintf("  %-30s %s", z, m.state.Weather[z]))
	}
}

func (m *weatherModule) printFronts(user *users.UserRecord) {
	if !m.simReady {
		sendLine(user, "Weather: simulation not running.")
		return
	}
	if len(m.state.Fronts) == 0 {
		sendLine(user, "No active weather fronts.")
		return
	}
	sendLine(user, fmt.Sprintf("Active fronts (%d):", len(m.state.Fronts)))
	for _, f := range m.state.Fronts {
		sendLine(user, fmt.Sprintf("  #%-3d %-10s @ %-25s intensity %.2f moisture %.2f age %d/%d",
			f.Id, f.Type, f.Zone, f.Intensity, f.Moisture, f.Age, f.MaxAge))
	}
}

// cmdSpawn: weather spawn <type> <zone words...> [intensity]. Zone names may
// contain spaces; a trailing float is taken as intensity only when at least
// one zone word remains.
func (m *weatherModule) cmdSpawn(user *users.UserRecord, parts []string) {
	if !m.simReady {
		sendLine(user, "Weather: simulation not running.")
		return
	}
	if len(parts) < 2 {
		sendLine(user, "Usage: weather spawn <type> <zone> [intensity 0..1]")
		return
	}
	wtype := strings.ToLower(parts[0])
	rest := parts[1:]
	intensity := 0.0
	if len(rest) > 1 {
		if f, err := strconv.ParseFloat(rest[len(rest)-1], 64); err == nil {
			intensity = f
			rest = rest[:len(rest)-1]
		}
	}
	zone, ok := m.graph.FindZone(strings.Join(rest, " "))
	if !ok {
		sendLine(user, fmt.Sprintf("Weather: zone %q is not in the graph.", strings.Join(rest, " ")))
		return
	}
	next, _, ok := sim.ForceSpawn(m.state, m.graph, m.simCfg, sim.WeatherType(wtype), zone, intensity, sim.Clock{Round: engine.CurrentRound()})
	if !ok {
		sendLine(user, "Weather: spawn failed.")
		return
	}
	m.state = next
	engine.Reconcile(m.state.Weather)
	m.persistState()
	f := m.state.Fronts[len(m.state.Fronts)-1]
	sendLine(user, fmt.Sprintf("Spawned front #%d: %s @ %s, intensity %.2f.", f.Id, f.Type, f.Zone, f.Intensity))
}

// cmdClear: weather clear [zone words...]. No zone = clear everything.
func (m *weatherModule) cmdClear(user *users.UserRecord, parts []string) {
	if !m.simReady {
		sendLine(user, "Weather: simulation not running.")
		return
	}
	var zones []sim.ZoneId
	if len(parts) > 0 {
		zone, ok := m.graph.FindZone(strings.Join(parts, " "))
		if !ok {
			sendLine(user, fmt.Sprintf("Weather: zone %q is not in the graph.", strings.Join(parts, " ")))
			return
		}
		zones = []sim.ZoneId{zone}
	}
	before := len(m.state.Fronts)
	next, diff := sim.ClearZones(m.state, m.graph, m.simCfg, zones, sim.Clock{Round: engine.CurrentRound()})
	m.state = next
	engine.Reconcile(m.state.Weather)
	m.persistState()
	sendLine(user, fmt.Sprintf("Cleared %d front(s); %d zone change(s).", before-len(m.state.Fronts), len(diff.Changes)))
}

func (m *weatherModule) printStatus(user *users.UserRecord) {
	if m.graph == nil {
		sendLine(user, "Weather: no geography graph yet. Try 'weather rebuild'.")
		return
	}
	g := m.graph
	sendLine(user, fmt.Sprintf("Geography: %d zones, %d edges, %d components (built round %d).",
		len(g.Nodes), len(g.Edges), g.Components, g.BuiltAtRound))
	if !m.simReady {
		sendLine(user, "Simulation: NOT running.")
		return
	}
	sendLine(user, fmt.Sprintf("Simulation: %d active front(s); state round %d; next tick at round %d (every %d game hour(s)).",
		len(m.state.Fronts), m.state.Round, m.nextTick, m.cfg.TickEveryGameHours))
	sendLine(user, fmt.Sprintf("Emotes: mode=%s every ~%d rounds; buffs=%v; persist=%v.",
		m.cfg.EmoteMode, m.cfg.EmoteEveryRounds, m.cfg.BuffsEnabled, m.cfg.Persist))
}

// printGraphForZone prints a zone's neighbors (crawler spot-check). NOTE: the
// Neighbors result is a shared index slice — copy before sorting.
func (m *weatherModule) printGraphForZone(user *users.UserRecord, zone string) {
	if m.graph == nil {
		sendLine(user, "Weather: no geography graph yet (built on the first round). Try 'weather rebuild'.")
		return
	}
	canonical, ok := m.graph.FindZone(zone)
	if !ok {
		sendLine(user, fmt.Sprintf("Weather: zone %q is not in the graph.", zone))
		return
	}
	node := m.graph.Nodes[canonical]
	sendLine(user, fmt.Sprintf(
		"Zone %s [biome=%s rooms=%d outdoor=%v]:", node.Zone, node.Biome, node.Rooms, node.HasOutdoor))

	neighbors := append([]sim.Edge(nil), m.graph.Neighbors(canonical)...)
	if len(neighbors) == 0 {
		sendLine(user, "  (no adjacent zones)")
		return
	}
	sort.Slice(neighbors, func(i, j int) bool { return neighbors[i].B < neighbors[j].B })
	for _, e := range neighbors {
		sendLine(user, fmt.Sprintf("  -> %s (weight %d)", e.B, e.Weight))
	}
}
```

- [ ] **Step 2: Delete the moved code from `weather.go`**

Remove `cmdWeather`, `printSummary`, and `printGraphForZone` from `weather.go`, plus the imports that become unused there (`fmt`, `sort`, and `rooms` — the command handler signature now lives in `weather_commands.go`). `printSummary`'s role is replaced by `printStatus`.

- [ ] **Step 3: Verify after Task 17 (single build unit) — see Task 15 Step 3 note**

- [ ] **Step 4: Commit**

```bash
git add weather_commands.go weather.go
git commit -m "feat(weather): player weather command and full admin subcommand set"
```

---

## Task 17: Exported API

**Files:** Create `weather_api.go`.

- [ ] **Step 1: Implement `weather_api.go`** (spec §9.7; mirrors the gmcp module's ExportFunction usage)

```go
package weather

import (
	"github.com/GoMudEngine/GoMud/modules/weather/engine"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

// registerExports exposes the module API to other modules and JS scripts via
// plugin.ExportFunction (spec §9.7). All exports guard simReady so callers
// during boot (or in degraded mode) get empty-but-valid answers.
func (m *weatherModule) registerExports() {
	m.plug.ExportFunction(`GetWeather`, m.exportGetWeather)
	m.plug.ExportFunction(`GetFronts`, m.exportGetFronts)
	m.plug.ExportFunction(`SpawnFront`, m.exportSpawnFront)
}

// exportGetWeather reports a zone's current weather: {"type": string,
// "intensity": float64}. type is "" when the sim isn't running or the zone is
// unknown; intensity is the strongest effective front projection (0 for calm).
func (m *weatherModule) exportGetWeather(zone string) map[string]any {
	out := map[string]any{"type": "", "intensity": 0.0}
	if !m.simReady {
		return out
	}
	canonical, ok := m.graph.FindZone(zone)
	if !ok {
		return out
	}
	w := m.state.Weather[canonical]
	if w == "" {
		w = sim.Clear
	}
	out["type"] = string(w)
	if covers := sim.Covering(m.graph, m.state.Fronts, m.simCfg, canonical); len(covers) > 0 {
		out["intensity"] = covers[0].Effective
	}
	return out
}

// exportGetFronts lists active fronts as plain maps (id, type, zone,
// intensity, moisture, age).
func (m *weatherModule) exportGetFronts() []map[string]any {
	if !m.simReady {
		return nil
	}
	out := make([]map[string]any, 0, len(m.state.Fronts))
	for _, f := range m.state.Fronts {
		out = append(out, map[string]any{
			"id": uint64(f.Id), "type": string(f.Type), "zone": f.Zone,
			"intensity": f.Intensity, "moisture": f.Moisture, "age": f.Age,
		})
	}
	return out
}

// exportSpawnFront programmatically spawns a front (e.g. a quest summoning a
// storm). Returns false when the sim isn't running or the zone is unknown.
func (m *weatherModule) exportSpawnFront(wtype string, zone string, intensity float64) bool {
	if !m.simReady {
		return false
	}
	canonical, ok := m.graph.FindZone(zone)
	if !ok {
		return false
	}
	next, _, ok := sim.ForceSpawn(m.state, m.graph, m.simCfg, sim.WeatherType(wtype), canonical, intensity, sim.Clock{Round: engine.CurrentRound()})
	if !ok {
		return false
	}
	m.state = next
	engine.Reconcile(m.state.Weather)
	m.persistState()
	return true
}
```

- [ ] **Step 2: Full build + test in the checkout (closes the Task 15–17 build unit)**

Run:
```powershell
pwsh scripts/sync-to-checkout.ps1 -Checkout "$HOME\workspace\GoMud"
Push-Location "$HOME\workspace\GoMud"
go build ./...
go vet ./modules/weather/...
go test ./modules/weather/...
Pop-Location
```
Expected: clean build, vet clean, all module tests PASS.

Also rerun standalone: `go test ./sim/... ./crawler/... ./content/...` and `gofmt -l .` (empty).

- [ ] **Step 3: Commit**

```bash
git add weather_api.go
git commit -m "feat(weather): exported GetWeather/GetFronts/SpawnFront API"
```

---

## Task 18: Documentation — context.md files and CONTRIBUTING

Per the repo rule: every package carries a `context.md`, and every plan includes this task.

**Files:** Modify `context.md` (root), `sim/context.md`, `engine/context.md`, `CONTRIBUTING.md`. Create `content/context.md`.

- [ ] **Step 1: Create `content/context.md`** — follow the GoMud convention (Overview / Key Components / Core Functions / Dependencies / Consumers / Testing). Cover: pure package for module data files; `ParseClimate`/`LoadClimate` (merge-over-defaults, wholesale-replace semantics, the spawnWeight-zero gotcha); `Table`/`Tables`/`ParseEmoteTable`/`LoadEmotes`/`Pick` (biome→default fallback, indoor-never-falls-to-outdoor, injected `roll` and WHY the sim RNG is banned here); `moduledata_test.go` validating the shipped YAML under `files/datafiles/` (mutator filename rule mirrors `util.ConvertForFilename`); the yaml.v2 dependency note (engine dep; standalone go.mod carries it; sync excludes go.mod).

- [ ] **Step 2: Update `sim/context.md`** — add: adjacency index in `Neighbors` (shared slice — callers must not mutate; rebuilt lazily after `FromJSON`), `FindZone`, `NewState`/`DeriveSeed` (FNV over sorted zone names), `query.go` (`Coverage`/`Covering` mirroring `resolveWeather`), `mutate.go` (`ForceSpawn`/`ClearZones`: pure, no RNG consumed, coverage-based clear semantics), the strengthened DeepEqual determinism test. Note `sim` remains engine-free; stdlib-only claim still true (yaml stayed out of sim).

- [ ] **Step 3: Update `engine/context.md`** — add: `state.go` (versioned envelope codec, `StateIdentifier`), `apply.go` (`mutatorSet` seam and why — registry-backed `Add` isn't unit-fakeable; `Apply`/`Reconcile`/`StripBuffs`; duplicate-add guard via `Has`; warn-once), `clock.go` (`TickPeriod` clamp, `NextTickRound` via `gametime.AddPeriod` per §9.3), `emotes.go` (`EmitAmbient`; biome-heuristic indoor detection; presentation RNG injected). Update the Consumers/Testing sections (apply/clock/state have in-checkout unit tests; emitter is smoke-verified glue).

- [ ] **Step 4: Update root `context.md`** — rewrite Key Components: `weather.go` (lifecycle only now), `weather_tick.go` (startSim/loadContent/loadOrInitState/tick/persistState/onSave/scheduleEmote), `weather_commands.go` (player + admin gating via `HasRolePermission`), `weather_api.go` (exports), `weather_config.go` (full Config, flat keys rationale). Keep the threading and DOGMud-backport sections; note the `weather` command is no longer admin-only.

- [ ] **Step 5: Update `CONTRIBUTING.md`** — in the Development workflow section: standalone test set is now `go test ./sim/... ./crawler/... ./content/...`; engine-coupled tests run in the checkout (`go test ./modules/weather/...` after sync); mention the yaml.v2 dev dependency and that `go.mod`/`go.sum` never travel to checkouts.

- [ ] **Step 6: Commit**

```bash
git add context.md sim/context.md engine/context.md content/context.md CONTRIBUTING.md
git commit -m "docs: document M3 engine integration across package context files"
```

---

## Task 19: In-checkout verification and smoke test

The OOBE bar (spec §2.3) starts being enforced here: stock world + `Enabled: true` ⇒ live weather, no authoring.

- [ ] **Step 1: Full clean verification**

```powershell
# Standalone
go test ./sim/... ./crawler/... ./content/...
go vet ./sim/... ./crawler/... ./content/...
gofmt -l .   # expect empty

# Checkout
pwsh scripts/sync-to-checkout.ps1 -Checkout "$HOME\workspace\GoMud"
Push-Location "$HOME\workspace\GoMud"
go generate ./...
go build ./...
go vet ./modules/weather/...
go test ./modules/weather/...
Pop-Location
```
Expected: everything green.

- [ ] **Step 2 (optional, M2 carry-over): race detector attempt**

Run: `go test -race ./sim/...`
On Windows this needs CGO + a C toolchain; if it fails with a toolchain error, record "still blocked on toolchain — needs CI/Linux runner" in the M4 notes and move on. If it runs: expect PASS (the sim is single-goroutine pure code).

- [ ] **Step 3: Boot smoke test (manual/driven)**

Start the server in the checkout (`go run .` from `~/workspace/GoMud`; default world). Verify in the boot log:
1. `mutators.LoadDataFiles()` shows a `loadedCount` 8 higher than before this plan (stock world: 10 disk + 8 plugin = 18) and **no** `duplicate mutator id` / `filepath mismatch` errors.
2. On the first round: `Weather: loaded geography cache` (or `built geography graph`), then `Weather: fresh simulation state seed=…`.

Then connect (telnet port from config, default world admin account) and run through:

| Action | Expect |
|---|---|
| `weather` (as admin or player) | "The weather in <zone> is <type>." (+ front detail when covered) |
| `weather status` | graph summary + sim running + next tick round |
| `weather zones` | every zone listed with a weather type (mostly clear at first) |
| `weather spawn storm <your zone> 0.9` | front spawned; `look` shows `(storm-wracked)` on the room name, storm description line, and the alert |
| wait ~EmoteEveryRounds rounds | an ambient storm line arrives; step into an indoor/cave room — indoor variant line |
| `weather fronts` | the storm listed with intensity/age |
| wait a few game hours | fronts move/decay on ticks; `weather zones` changes |
| `weather clear` | fronts removed; `look` no longer shows the mutator |
| `weather spawn storm <zone> 0.9`, then restart the server | boot log says `Weather: restored simulation state fronts=1`; `weather fronts` still shows it; the room still renders the mutator (Reconcile) |
| set `BuffsEnabled: false` + restart, `weather spawn blizzard <zone>` | log `Weather: buffs disabled by config`; no Freezing Snow buff applied to players in the zone |

3. Record any deviation as a bug to fix before closing the milestone — do not paper over.

- [ ] **Step 4: Update the spec status block**

Append to the §6-style status notes in `docs/superpowers/specs/2026-06-08-weather-module-design.md` (after the M1b status note): a short dated note that §8–§10 (M3) are implemented — zone-wide mutator application, plugin-FS mutator specs (upstream now supports module mutator AND buff overlays — R-core-1's contingency landed; v1 still reuses buff ids 31/33), gametime-scheduled ticks, persistence with reconcile-on-boot, indoor-aware emotes, full command set, exported API; per-room mutator refinement + Buffs.Overrides deferred to M4.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-06-08-weather-module-design.md
git commit -m "docs(spec): record M3 engine integration status and verified upstream facts"
```
