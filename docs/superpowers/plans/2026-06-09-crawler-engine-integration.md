# Geography Crawler — Engine Integration (M1b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the pure crawler core (M1a) run against a live GoMud world — an engine-backed `WorldReader`, a versioned on-disk graph cache, a boot-time build, and an admin `weather` command to inspect/rebuild the zone graph.

**Architecture:** A new `engine/` package is the ONLY package that imports the GoMud engine (`internal/rooms`, etc.); it implements `crawler.WorldReader` and a pure cache decoder. The module root (`weather.go`, package `weather`) registers the plugin, loads config, builds-or-loads the graph on `SetOnLoad`, persists it via `plugin.WriteBytes`, and serves the `weather` admin command. The pure `sim`/`crawler` packages from M1a are unchanged except for two small test-coverage follow-ups.

**Tech Stack:** Go 1.25; GoMud/DOGMud engine packages (`internal/plugins`, `internal/rooms`, `internal/events`, `internal/users`, `internal/messaging`, `internal/mudlog`, `internal/util`); the module's own `sim`/`crawler`.

**Spec:** Completes §6 (Geography Crawler) of `docs/superpowers/specs/2026-06-08-weather-module-design.md` — the engine-backed reader, cache persistence, and the `weather graph`/`weather rebuild` commands deferred from M1a. Also resolves the M1a final-review follow-ups (see memory `m1b-followups`).

---

## CRITICAL: where this code builds and how it's tested

Unlike M1a (pure, standalone), the `engine/` and `weather` packages import `internal/*` and **only compile inside a GoMud/DOGMud checkout**, where the module lives at `modules/weather/` as part of the engine module (no `go.mod`). Consequences for every engine task below:

- **Source of truth = this repo** (`C:/Users/Calabe Davis/workspace/weather-module`). Author and commit here.
- **Build/test = a checkout** = `C:/Users/Calabe Davis/workspace/DOGMud`. The module is synced into `DOGMud/modules/weather/` (WITHOUT `go.mod`), then `go generate ./... && go build && go test ./modules/weather/...`.
- **Standalone repo tooling changes:** once `engine/` exists, `go test ./...` in the repo will fail to compile the engine package (no engine available). The repo's pure-package command is now **`go test ./sim/... ./crawler/...`**. Editor/LSP will show unresolved-import errors on `engine/`/`weather.go` in the standalone repo — that is expected; they resolve in the checkout.
- The sync script created in Task 2 makes "push repo → checkout, build, test" a one-liner. The per-task loop for engine tasks is: **edit in repo → `git commit` in repo → run sync script → build/test in the DOGMud checkout → if red, fix in repo and repeat.**

---

## File Structure

| File | Responsibility | Builds where |
|---|---|---|
| `crawler/arch_test.go` | Purity guardrail: `crawler` imports no `internal/*`. | standalone |
| `sim/graph_test.go` (modify) | Add `FromJSON` malformed-JSON test. | standalone |
| `scripts/sync-to-checkout.ps1` | Mirror module source into a checkout's `modules/weather/` (excludes `go.mod`). | n/a (tooling) |
| `engine/doc.go` | Package doc for `engine`. | checkout |
| `engine/cache.go` | `CacheIdentifier` const + pure `DecodeCache` (version-checked). | checkout |
| `engine/cache_test.go` | `DecodeCache` tests. | checkout |
| `engine/worldreader.go` | `WorldReader` impl over `internal/rooms` + `isOutdoorBiome`. | checkout |
| `engine/worldreader_test.go` | `isOutdoorBiome` tests. | checkout |
| `engine/context.md` | Package documentation (GoMud convention). | n/a (docs) |
| `weather_config.go` | `Config`, `buildConfig(getter)`, `loadConfig(plugin)`. | checkout |
| `weather_config_test.go` | `buildConfig` test. | checkout |
| `weather.go` | Plugin registration, `onLoad`, graph build/load/persist, `weather` command. | checkout |
| `files/data-overlays/config.yaml` | `Modules.weather.*` defaults. | checkout |

---

## Task 1: M1a follow-ups (pure, standalone)

Knock out the two pure follow-ups from the M1a review before touching engine code. These run in the standalone repo.

**Files:** Create `crawler/arch_test.go`; modify `sim/graph_test.go`.

- [ ] **Step 1: Write the crawler purity guardrail `crawler/arch_test.go`**

```go
package crawler

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestCrawlerPackageStaysPure mirrors sim's guardrail: the crawler package may
// depend on sim and the standard library, but never on the GoMud engine
// (internal/*). Engine access belongs in the engine/ adapter package.
func TestCrawlerPackageStaysPure(t *testing.T) {
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
				t.Errorf("%s imports forbidden engine package %q (crawler must stay pure)", e.Name(), p)
			}
		}
	}
}
```

- [ ] **Step 2: Add the `FromJSON` error test to `sim/graph_test.go`**

```go
func TestFromJSONError(t *testing.T) {
	if _, err := FromJSON([]byte("{not valid json")); err == nil {
		t.Error("FromJSON should return an error for malformed JSON")
	}
}
```

- [ ] **Step 3: Run the pure tests**

Run: `go test ./sim/... ./crawler/...`
Expected: PASS (both packages; the new tests included).

- [ ] **Step 4: Commit**

```bash
git add crawler/arch_test.go sim/graph_test.go
git commit -m "test: add crawler purity guardrail and FromJSON error coverage"
```

---

## Task 2: Checkout sync script + verify the module builds in DOGMud

Create the tooling that mirrors the module into a checkout, and confirm the engine compiles the (currently pure) module end-to-end. This proves the in-checkout workflow before any engine code exists.

**Files:** Create `scripts/sync-to-checkout.ps1`.

- [ ] **Step 1: Create `scripts/sync-to-checkout.ps1`**

```powershell
# Mirror the weather module source into a GoMud/DOGMud checkout's modules/weather/.
# The repo's go.mod is deliberately EXCLUDED: in-checkout modules have no go.mod
# (they are part of the engine module). Run from the repo root.
#
#   pwsh scripts/sync-to-checkout.ps1 -Checkout C:\Users\Calabe Davis\workspace\DOGMud
param([Parameter(Mandatory = $true)][string]$Checkout)

$dest = Join-Path $Checkout "modules\weather"
New-Item -ItemType Directory -Force -Path $dest | Out-Null

# /MIR mirrors (so deletions propagate); exclude repo-only dirs/files. go.mod and
# go.sum MUST NOT travel. robocopy returns 0-7 on success (>=8 is an error).
robocopy "." $dest /MIR `
  /XD .git docs scripts .worktrees `
  /XF go.mod go.sum "*.png" "Screenshot*" `
  /NFL /NDL /NJH /NJS | Out-Null
if ($LASTEXITCODE -ge 8) { throw "robocopy failed with code $LASTEXITCODE" }

Write-Host "Synced module source to $dest (go.mod excluded)."
```

- [ ] **Step 2: Sync into the DOGMud checkout**

Run: `pwsh scripts/sync-to-checkout.ps1 -Checkout "C:\Users\Calabe Davis\workspace\DOGMud"`
Expected: prints `Synced module source to ...\modules\weather (go.mod excluded).` Confirm `C:\Users\Calabe Davis\workspace\DOGMud\modules\weather\` contains `sim/`, `crawler/`, and NO `go.mod`.

- [ ] **Step 3: Regenerate the module registry and build in the checkout**

Run (in `C:\Users\Calabe Davis\workspace\DOGMud`):
```
go generate ./...
go build ./...
go test ./modules/weather/...
```
Expected: `go generate` rewrites `modules/all-modules.go` to include `_ "github.com/GoMudEngine/GoMud/modules/weather"`; `go build` succeeds; `go test ./modules/weather/...` passes (the pure `sim`/`crawler` tests run inside the checkout too). If `all-modules.go` does not list `weather` after generate, STOP and report.

- [ ] **Step 4: Commit the script**

```bash
git add scripts/sync-to-checkout.ps1
git commit -m "build: add sync-to-checkout script for in-checkout module builds"
```

> For all remaining tasks: after editing in the repo, re-run the Step 2 sync, then build/test in the DOGMud checkout as in Step 3.

---

## Task 3: `engine` package — pure cache decoder

Start the engine package with the one piece that's pure logic (sim-only): the version-checked cache decoder. This resolves the M1a follow-up that the `GraphVersion` staleness check had no consumer.

**Files:** Create `engine/doc.go`, `engine/cache.go`, `engine/cache_test.go`.

- [ ] **Step 1: Create `engine/doc.go`**

```go
// Package engine adapts the GoMud engine to the weather module's pure core. It
// is the ONLY package in the module permitted to import the GoMud engine
// (internal/*): it implements crawler.WorldReader over internal/rooms and
// provides the on-disk graph-cache codec. Keeping all engine calls here is what
// makes the module portable across GoMud and DOGMud.
package engine
```

- [ ] **Step 2: Write the failing test `engine/cache_test.go`**

```go
package engine

import (
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

func TestDecodeCache(t *testing.T) {
	// Absent / empty / malformed -> not ok, no panic.
	if _, ok := DecodeCache(nil); ok {
		t.Error("nil bytes should decode as not-ok")
	}
	if _, ok := DecodeCache([]byte("{not json")); ok {
		t.Error("malformed json should decode as not-ok")
	}

	// Stale version -> not ok (signals rebuild).
	stale := &sim.Graph{Version: sim.GraphVersion + 1}
	sb, err := stale.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := DecodeCache(sb); ok {
		t.Error("stale GraphVersion should decode as not-ok")
	}

	// Current version -> ok, returns the graph.
	good := &sim.Graph{Version: sim.GraphVersion, Nodes: map[string]sim.ZoneNode{"A": {Zone: "A"}}}
	gb, err := good.ToJSON()
	if err != nil {
		t.Fatal(err)
	}
	g, ok := DecodeCache(gb)
	if !ok || g == nil || len(g.Nodes) != 1 {
		t.Fatalf("current-version cache should decode ok; got ok=%v g=%v", ok, g)
	}
}
```

- [ ] **Step 3: Sync + run the test in the checkout to verify it fails**

Sync (Task 2 Step 2), then in the DOGMud checkout run: `go test ./modules/weather/engine/`
Expected: FAIL — `undefined: DecodeCache`.

- [ ] **Step 4: Implement `engine/cache.go`**

```go
package engine

import "github.com/GoMudEngine/GoMud/modules/weather/sim"

// CacheIdentifier is the plugin-storage key for the geography graph cache
// (written via plugin.WriteBytes / read via plugin.ReadBytes).
const CacheIdentifier = "geography"

// DecodeCache parses cached graph bytes and reports whether they are usable.
// It returns ok=false (without an error) for absent, empty, unparseable, or
// stale-version data, signaling the caller to rebuild the graph.
func DecodeCache(b []byte) (*sim.Graph, bool) {
	if len(b) == 0 {
		return nil, false
	}
	g, err := sim.FromJSON(b)
	if err != nil {
		return nil, false
	}
	if g.Version != sim.GraphVersion {
		return nil, false
	}
	return g, true
}
```

- [ ] **Step 5: Sync + run the test in the checkout to verify it passes**

Sync, then: `go test ./modules/weather/engine/`
Expected: PASS.

- [ ] **Step 6: Commit (in the repo)**

```bash
git add engine/doc.go engine/cache.go engine/cache_test.go
git commit -m "feat(engine): versioned graph-cache decoder"
```

---

## Task 4: `engine` package — engine-backed `WorldReader`

**Files:** Create `engine/worldreader.go`, `engine/worldreader_test.go`.

- [ ] **Step 1: Write the failing test `engine/worldreader_test.go`**

```go
package engine

import "testing"

func TestIsOutdoorBiome(t *testing.T) {
	if !isOutdoorBiome("forest") {
		t.Error("forest should be outdoor")
	}
	if isOutdoorBiome("cave") {
		t.Error("cave should be indoor")
	}
	if !isOutdoorBiome("") {
		t.Error("unknown/empty biome should default to outdoor")
	}
}
```

- [ ] **Step 2: Sync + run in checkout to verify it fails**

`go test ./modules/weather/engine/ -run TestIsOutdoorBiome`
Expected: FAIL — `undefined: isOutdoorBiome`.

- [ ] **Step 3: Implement `engine/worldreader.go`**

```go
package engine

import (
	"github.com/GoMudEngine/GoMud/internal/rooms"
	"github.com/GoMudEngine/GoMud/modules/weather/crawler"
)

// indoorBiomes are biome ids treated as indoors/underground, so the crawler
// records their zones as having no outdoor rooms. GoMud has no explicit
// indoor/outdoor room flag, so this is a heuristic; a later milestone can make
// the set configurable when weather presentation needs finer control.
var indoorBiomes = map[string]bool{
	"cave":        true,
	"underground": true,
	"dungeon":     true,
	"indoor":      true,
	"tunnel":      true,
	"sewer":       true,
}

// isOutdoorBiome reports whether a biome id is considered outdoors. An unknown
// or empty biome defaults to outdoors.
func isOutdoorBiome(biomeId string) bool {
	return !indoorBiomes[biomeId]
}

// WorldReader implements crawler.WorldReader over the live GoMud engine.
type WorldReader struct{}

// NewWorldReader returns a crawler.WorldReader backed by internal/rooms.
func NewWorldReader() crawler.WorldReader { return WorldReader{} }

func (WorldReader) ZoneNames() []string { return rooms.GetAllZoneNames() }

func (WorldReader) ZoneBiome(zone string) string { return rooms.GetZoneBiome(zone) }

func (WorldReader) RoomIDs(zone string) []int { return rooms.GetAllZoneRoomsIds(zone) }

func (WorldReader) Room(id int) (crawler.RoomView, bool) {
	r := rooms.LoadRoom(id)
	if r == nil {
		return crawler.RoomView{}, false
	}
	exits := make([]crawler.ExitView, 0, len(r.Exits))
	for _, ex := range r.Exits {
		exits = append(exits, crawler.ExitView{ToRoom: ex.RoomId, Secret: ex.Secret})
	}
	biomeID := ""
	if b := r.GetBiome(); b != nil {
		biomeID = b.BiomeId
	}
	return crawler.RoomView{
		ID:      r.RoomId,
		Zone:    r.Zone,
		Outdoor: isOutdoorBiome(biomeID),
		Exits:   exits,
	}, true
}
```

- [ ] **Step 4: Sync + verify it passes, and verify the interface is satisfied**

`go test ./modules/weather/engine/`
Expected: PASS. (Compilation alone proves `WorldReader` satisfies `crawler.WorldReader`, because `NewWorldReader` returns that interface type.)

- [ ] **Step 5: Commit (in the repo)**

```bash
git add engine/worldreader.go engine/worldreader_test.go
git commit -m "feat(engine): WorldReader over internal/rooms with biome-based outdoor heuristic"
```

---

## Task 5: Module config

**Files:** Create `weather_config.go`, `weather_config_test.go`, `files/data-overlays/config.yaml`.

- [ ] **Step 1: Write the failing test `weather_config_test.go`**

```go
package weather

import "testing"

func TestBuildConfig(t *testing.T) {
	vals := map[string]any{
		"Enabled":            true,
		"IncludeSecretExits": true,
		"RebuildGraphOnBoot": false,
	}
	c := buildConfig(func(k string) any { return vals[k] })

	if !c.Enabled {
		t.Error("Enabled should be true")
	}
	if !c.IncludeSecretExits {
		t.Error("IncludeSecretExits should be true")
	}
	if c.RebuildGraphOnBoot {
		t.Error("RebuildGraphOnBoot should be false")
	}
}
```

- [ ] **Step 2: Sync + run in checkout to verify it fails**

`go test ./modules/weather/ -run TestBuildConfig`
Expected: FAIL — `undefined: buildConfig` (and `package weather` has no non-test files yet; that's fine — the test names the package).

- [ ] **Step 3: Implement `weather_config.go`**

```go
package weather

import "github.com/GoMudEngine/GoMud/internal/plugins"

// Config is the resolved module configuration (keys live under
// Modules.weather.* and default from files/data-overlays/config.yaml).
type Config struct {
	Enabled            bool
	IncludeSecretExits bool
	RebuildGraphOnBoot bool
}

// getter abstracts plugin.Config.Get for testability.
type getter func(string) any

func asBool(v any) bool { b, _ := v.(bool); return b }

// buildConfig resolves config from a getter.
func buildConfig(get getter) Config {
	return Config{
		Enabled:            asBool(get("Enabled")),
		IncludeSecretExits: asBool(get("IncludeSecretExits")),
		RebuildGraphOnBoot: asBool(get("RebuildGraphOnBoot")),
	}
}

// loadConfig reads the module's live config via the plugin API.
func loadConfig(p *plugins.Plugin) Config {
	return buildConfig(func(k string) any { return p.Config.Get(k) })
}
```

- [ ] **Step 4: Create `files/data-overlays/config.yaml`**

```yaml
# Modules.weather.* defaults. This overlay overrides the base config.yaml; do
# NOT add a Modules: weather: block to config-overrides.yaml (it will not merge).
Enabled: true
IncludeSecretExits: true   # count secret/hidden exits as zone adjacency
RebuildGraphOnBoot: false  # false = use the cached graph if present
```

- [ ] **Step 5: Sync + verify it passes**

`go test ./modules/weather/ -run TestBuildConfig`
Expected: PASS.

- [ ] **Step 6: Commit (in the repo)**

```bash
git add weather_config.go weather_config_test.go files/data-overlays/config.yaml
git commit -m "feat(weather): module config (Enabled/IncludeSecretExits/RebuildGraphOnBoot)"
```

---

## Task 6: Plugin registration + boot-time graph build/load/persist

**Files:** Create `weather.go`.

> This task has no standalone unit test (it's plugin wiring verified by build + the smoke test in Task 8). The `go build ./...` in the checkout is the gate; the runtime behavior is verified in Task 8.

- [ ] **Step 1: Implement `weather.go`**

```go
package weather

import (
	"embed"

	"github.com/GoMudEngine/GoMud/internal/mudlog"
	"github.com/GoMudEngine/GoMud/internal/plugins"
	"github.com/GoMudEngine/GoMud/internal/util"
	"github.com/GoMudEngine/GoMud/modules/weather/crawler"
	"github.com/GoMudEngine/GoMud/modules/weather/engine"
	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

//go:embed files/*
var files embed.FS

// weatherModule holds the plugin handle, resolved config, and the current
// geography graph.
type weatherModule struct {
	plug  *plugins.Plugin
	cfg   Config
	graph *sim.Graph
}

var module weatherModule

func init() {
	module = weatherModule{plug: plugins.New(`weather`, `0.1.0`)}
	if err := module.plug.AttachFileSystem(files); err != nil {
		panic(err)
	}
	module.plug.Callbacks.SetOnLoad(module.onLoad)
}

// onLoad runs after the world is loaded. It registers the admin command and
// makes the geography graph available (from cache or a fresh crawl).
func (m *weatherModule) onLoad() {
	m.cfg = loadConfig(m.plug)
	if !m.cfg.Enabled {
		return
	}
	m.plug.AddUserCommand(`weather`, m.cmdWeather, false, true) // admin-only for M1b
	m.loadOrBuildGraph()
}

// loadOrBuildGraph uses the cached graph when present and current, otherwise
// crawls the world and persists the result.
func (m *weatherModule) loadOrBuildGraph() {
	if !m.cfg.RebuildGraphOnBoot {
		if b, err := m.plug.ReadBytes(engine.CacheIdentifier); err == nil {
			if g, ok := engine.DecodeCache(b); ok {
				m.graph = g
				mudlog.Info("weather", "msg", "loaded geography cache",
					"zones", len(g.Nodes), "edges", len(g.Edges))
				return
			}
		}
	}
	m.rebuildGraph()
}

// rebuildGraph crawls the live world, stores the graph in memory, and writes
// the cache.
func (m *weatherModule) rebuildGraph() {
	opts := crawler.DefaultOptions()
	opts.IncludeSecretExits = m.cfg.IncludeSecretExits
	opts.BuiltAtRound = util.GetRoundCount()

	g, err := crawler.Build(engine.NewWorldReader(), opts)
	if err != nil {
		mudlog.Error("weather", "msg", "graph build failed", "error", err)
		return
	}
	m.graph = g

	if b, err := g.ToJSON(); err == nil {
		if err := m.plug.WriteBytes(engine.CacheIdentifier, b); err != nil {
			mudlog.Error("weather", "msg", "graph cache write failed", "error", err)
		}
	}
	mudlog.Info("weather", "msg", "built geography graph",
		"zones", len(g.Nodes), "edges", len(g.Edges), "components", g.Components)
}
```

- [ ] **Step 2: Sync + build in the checkout**

`go build ./...` (in the DOGMud checkout)
Expected: success. (`cmdWeather` is referenced but defined in Task 7 — so this step will FAIL to compile with `m.cmdWeather undefined` until Task 7 is done. That is expected; proceed to Task 7 and build there. If you prefer a compiling checkpoint, do Tasks 6 and 7 back-to-back before building.)

- [ ] **Step 3: Commit (in the repo)**

```bash
git add weather.go
git commit -m "feat(weather): plugin registration + boot-time graph build/load/persist"
```

---

## Task 7: The `weather` admin command

**Files:** Modify `weather.go` (add `cmdWeather` and helpers).

- [ ] **Step 1: Add the command + helpers to `weather.go`**

Add these imports to `weather.go`'s import block: `"fmt"`, `"sort"`, `"strings"`, `"github.com/GoMudEngine/GoMud/internal/events"`, `"github.com/GoMudEngine/GoMud/internal/messaging"`, `"github.com/GoMudEngine/GoMud/internal/rooms"`, `"github.com/GoMudEngine/GoMud/internal/users"`. Then append:

```go
// cmdWeather is the admin command. Subcommands:
//   weather                -> graph summary
//   weather graph [zone]   -> neighbors of a zone (default: caller's zone)
//   weather rebuild        -> re-crawl the world and rewrite the cache
func (m *weatherModule) cmdWeather(rest string, user *users.UserRecord, room *rooms.Room, flags events.EventFlag) (bool, error) {
	args := strings.Fields(rest)
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
	}

	switch sub {
	case "rebuild":
		m.rebuildGraph()
		if m.graph == nil {
			user.SendText(messaging.CategorySystem, "Weather: graph rebuild failed (see server log).")
			return true, nil
		}
		user.SendText(messaging.CategorySystem, fmt.Sprintf(
			"Weather: rebuilt graph — %d zones, %d edges, %d components.",
			len(m.graph.Nodes), len(m.graph.Edges), m.graph.Components))
	case "graph":
		zone := strings.TrimSpace(rest[len(args[0]):])
		if zone == "" {
			zone = user.Character.Zone
		}
		m.printGraphForZone(user, zone)
	default:
		m.printSummary(user)
	}
	return true, nil
}

func (m *weatherModule) printSummary(user *users.UserRecord) {
	if m.graph == nil {
		user.SendText(messaging.CategorySystem, "Weather: no geography graph yet. Run 'weather rebuild'.")
		return
	}
	g := m.graph
	user.SendText(messaging.CategorySystem, fmt.Sprintf(
		"Weather geography: %d zones, %d edges, %d components (built round %d).",
		len(g.Nodes), len(g.Edges), g.Components, g.BuiltAtRound))
}

func (m *weatherModule) printGraphForZone(user *users.UserRecord, zone string) {
	if m.graph == nil {
		user.SendText(messaging.CategorySystem, "Weather: no geography graph yet. Run 'weather rebuild'.")
		return
	}
	node, ok := m.graph.Nodes[zone]
	if !ok {
		user.SendText(messaging.CategorySystem, fmt.Sprintf("Weather: zone %q is not in the graph.", zone))
		return
	}
	user.SendText(messaging.CategorySystem, fmt.Sprintf(
		"Zone %s [biome=%s rooms=%d outdoor=%v]:", node.Zone, node.Biome, node.Rooms, node.HasOutdoor))

	neighbors := m.graph.Neighbors(zone)
	if len(neighbors) == 0 {
		user.SendText(messaging.CategorySystem, "  (no adjacent zones)")
		return
	}
	sort.Slice(neighbors, func(i, j int) bool { return neighbors[i].B < neighbors[j].B })
	for _, e := range neighbors {
		user.SendText(messaging.CategorySystem, fmt.Sprintf("  -> %s (weight %d)", e.B, e.Weight))
	}
}
```

- [ ] **Step 2: Sync + build and run package tests in the checkout**

```
go build ./...
go test ./modules/weather/...
go vet ./modules/weather/...
```
Expected: build succeeds; all module tests pass; vet clean.

- [ ] **Step 3: Commit (in the repo)**

```bash
git add weather.go
git commit -m "feat(weather): admin 'weather' command (summary/graph/rebuild)"
```

---

## Task 8: Smoke test, docs, and spec reconciliation

**Files:** Create `engine/context.md`; modify `README.md`, `docs/superpowers/specs/2026-06-08-weather-module-design.md`.

- [ ] **Step 1: Manual smoke test in the DOGMud checkout**

Sync, then in `C:\Users\Calabe Davis\workspace\DOGMud`: `go generate ./... && go build` and start the server. Log in as an **admin** character and verify:

1. `weather` → prints `Weather geography: N zones, M edges, K components (built round R).` (N > 0 on the default world).
2. `weather graph` → prints the current zone's biome/rooms/outdoor line and its adjacent zones with weights (or `(no adjacent zones)`).
3. `weather graph <someZone>` → prints that zone's neighbors; an unknown zone prints the "not in the graph" message.
4. `weather rebuild` → prints the rebuilt summary.
5. Confirm the cache file exists on disk: a file matching `*weather-v0-1-0*/geography.plugin.dat` under the engine's plugin write folder (search the checkout's data/write directory). Restart the server and confirm the log shows `loaded geography cache` (not a rebuild), proving persistence + load.

Record the observed `weather` summary output in the commit message or PR notes. If any step misbehaves, STOP and report.

- [ ] **Step 2: Create `engine/context.md`**

````markdown
# engine Package Context

## Overview
`engine` is the weather module's adapter to the GoMud engine. It is the ONLY
package in the module that imports the engine (`internal/*`). Keeping every
engine call here is what makes the rest of the module (`sim`, `crawler`) pure
and portable across GoMud and DOGMud.

## Key Components
### Core Files
- **worldreader.go**: `WorldReader` implements `crawler.WorldReader` over
  `internal/rooms` (`GetAllZoneNames`, `GetZoneBiome`, `GetAllZoneRoomsIds`,
  `LoadRoom`). `NewWorldReader()` returns it as the interface. `isOutdoorBiome`
  derives a room's outdoor flag from its biome id (GoMud has no explicit
  indoor/outdoor flag), using the `indoorBiomes` heuristic set.
- **cache.go**: `CacheIdentifier` (the plugin-storage key) and `DecodeCache`,
  a pure, version-checked decoder that returns ok=false for absent/empty/
  unparseable/stale data so the caller knows to rebuild.

## Dependencies
- `internal/rooms` (engine) — the live world.
- `github.com/GoMudEngine/GoMud/modules/weather/{sim,crawler}` — pure types.

## Consumers
- The module root (`weather.go`) uses `NewWorldReader()` to crawl and
  `DecodeCache`/`CacheIdentifier` to load/save the graph cache.

## Testing
- `cache_test.go` covers `DecodeCache` (pure). `worldreader_test.go` covers
  `isOutdoorBiome`. The `WorldReader` engine methods are thin glue verified by
  the module's boot-time build and the `weather` command smoke test. These tests
  compile only inside a GoMud/DOGMud checkout (engine imports).

## Build note
This package compiles only inside a checkout (it imports `internal/*`). In the
standalone repo, test the pure core with `go test ./sim/... ./crawler/...`.
````

- [ ] **Step 3: Update `README.md` Status + Development sections**

In `README.md`, set the `## Status` block to reflect M1b and add the checkout workflow. Replace the current Status body with:

```
**M1b complete — crawler runs against a live world.** On top of the pure core
(`sim/`, `crawler/`), the `engine/` adapter reads the live world, the module
builds a geography graph on boot, caches it to disk, and exposes an admin
`weather` command (summary / `graph [zone]` / `rebuild`). Next: M2 (weather
simulation core).
```

And replace the `## Development` body with:

```
The pure core (`sim/`, `crawler/`) is tested standalone in this repo:
`go test ./sim/... ./crawler/...` (note: NOT `./...`, which now includes the
engine-coupled packages). The `engine/` and root `weather` packages import the
GoMud engine and compile only inside a checkout. To build/test them, sync the
module into a checkout and build there:

    pwsh scripts/sync-to-checkout.ps1 -Checkout <path-to-GoMud-or-DOGMud>
    # then, in the checkout:
    go generate ./... && go build && go test ./modules/weather/...

The sync excludes this repo's `go.mod` (in-checkout modules have none). See
[CONTRIBUTING.md](CONTRIBUTING.md) for the module/engine boundary.
```

- [ ] **Step 4: Fix the stale spec wording (M1a review follow-up #5/#6)**

In `docs/superpowers/specs/2026-06-08-weather-module-design.md`:
- In §4.2's directory-layout block, the `crawler/` comment currently reads "Imports the engine adapter, not sim." Change it to: `# geography crawler (zone adjacency) — pure; consumes a WorldReader, imports sim`.
- In the same block, ensure the `engine/` line notes it implements `crawler.WorldReader` (it already says it's the only package importing the engine).
- If §14 (Testing) repeats "crawler imports the engine adapter," correct it to state the crawler is pure and the engine-backed `WorldReader` lives in `engine/`.
- Append to §6.5: `> **Status (2026-06-09, M1b):** engine-backed WorldReader, versioned cache persistence, boot-time build, and the `weather` admin command (summary/graph/rebuild) are implemented and smoke-tested in a DOGMud checkout. §6 is now complete.`

- [ ] **Step 5: Run pure tests in the repo, then commit**

Run (repo): `go test ./sim/... ./crawler/...`
Expected: PASS.

```bash
git add engine/context.md README.md docs/superpowers/specs/2026-06-08-weather-module-design.md
git commit -m "docs(engine): add engine context.md; reconcile README/spec for M1b"
```

---

## Self-Review Notes (author)

**Spec coverage (§6 remainder):** engine-backed traversal (§6.2 via `engine.WorldReader` over `LoadRoom`/`GetAllZone*`) → Task 4; biome/outdoor metadata source → Task 4 (`isOutdoorBiome`); cost/timing — boot crawl + cache, rebuild on demand (§6.3) → Tasks 6/7; cache format read/write + version check (§6.4) → Tasks 3/6; `weather graph` spot-check (§6.5 acceptance) → Task 7; deterministic/no-RNG and standalone-testable core already done in M1a. M1a follow-ups (crawler purity test, FromJSON error test, version-check consumer, spec wording) → Tasks 1/3/8.

**Type/name consistency:** `engine.CacheIdentifier`/`engine.DecodeCache`/`engine.NewWorldReader` are defined in Tasks 3–4 and consumed in Task 6. `crawler.DefaultOptions`/`Build`/`WorldReader`/`RoomView`/`ExitView` and `sim.Graph`/`Neighbors`/`FromJSON`/`GraphVersion`/`ToJSON` match the M1a APIs exactly. `Config`/`buildConfig`/`loadConfig` (Task 5) are used by `onLoad` (Task 6). The command handler signature matches the engine's `usercommands.UserCommand` (`func(string, *users.UserRecord, *rooms.Room, events.EventFlag) (bool, error)`), verified against `modules/playtest/commands.go`.

**Placeholders:** none — every step has complete code or an exact command + expected result. Tasks 6/7 are wiring with no unit test by design; their gate is the checkout build plus the Task 8 smoke test (explicit, with observable expectations), not a hand-waved "test it."

**Known sharp edges (called out in-plan, not hidden):** (1) Task 6 alone won't compile until Task 7 adds `cmdWeather` — flagged; do 6+7 together for a green build. (2) Once `engine/` exists, repo `go test ./...` breaks; the repo command is `go test ./sim/... ./crawler/...` — flagged in the CRITICAL section, README, and Task 1/8. (3) `Outdoor` is a biome heuristic (no engine flag exists); documented as such and deferred-configurable to M3.
