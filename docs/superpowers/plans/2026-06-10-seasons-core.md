# Seasons S1 — Season Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make weather odds follow the world's calendar: a pure `seasons/` package resolves data-defined per-biome season tracks and produces an effective `sim.Climate` each tick, plus the season surface (per-zone season map, `WeatherSeasonChanged` event, `GetSeason` export, command output) — with `sim.Step` untouched.

**Architecture:** Seasons are a **pure climate transform** (spec §2): `seasons.EffectiveClimate(base, tracks, pos)` returns a season-adjusted copy of the climate before each `sim.Step` call; `seasons.ZoneSeasons` maps every zone to its `{track, season, blend}` via its biome. Nothing is persisted — season state is derivable from the round. Mutators/prose are S2/S3; S1 ships the math, data, and API.

**Tech Stack:** Go 1.25; `gopkg.in/yaml.v2` (already a dep). New pure package `seasons/` (arch-tested like `sim`/`content`). Engine-coupled glue compiles only in the GoMud checkout at `~/workspace/GoMud`.

**Spec:** `docs/superpowers/specs/2026-06-10-seasons-design.md` §§2–5, §7, §8 (S1 row), §9.

---

## Verified engine facts (upstream master, fast-forwarded 2026-06-10)

1. `gametime.GetDate()` → `GameDate` with `Day` = **day-of-year** (1-based, year already subtracted — `gametime.go:260-273`), `Month` (1-based, clamped to the month count), `Year`, and `Calendar` (the active calendar name).
2. `gametime.GetCalendar(name) (CalendarConfig, bool)` (`admin.go:26`) is public; `CalendarConfig{DaysPerYear int, Months []string, ...}` — month count = `len(Months)`. Stock default world: 365 days, 12 months.
3. The engine's month-of-day formula (`gametime.go:268-271`): `month = 1 + floor(day*24 / hoursPerMonth)` with `hoursPerMonth = daysPerYear*24/numMonths`, clamped to `numMonths`. The seasons package MUST mirror this exactly so the season's month claims agree with what the `time` command shows players.
4. Module-defined events: `events.Event` is just `Type() string`; `events.AddToQueue(e Event, priority ...int)`. Other modules listen by importing our exported event type.
5. Buff ids 31/33 and all M3 touchpoints unchanged by the three new upstream commits (buff-flags externalization; doesn't affect us).

## Design decisions (read before starting)

- **Blend contract:** a season's `transitionDays` window sits at the **start** of that season. `blend = clamp(daysIntoSeason / transitionDays, 0, 1)` (0-based `daysIntoSeason`; `transitionDays <= 0` ⇒ blend 1 immediately). Day 0 of winter ⇒ blend 0.0 ⇒ effective odds still 100% autumn — the curve is **continuous across the boundary** and reaches full winter after `transitionDays` days. The *reported* season flips on day 0 (presentation flips at the boundary, odds blend — spec §3.3); `GetSeason` returning `blend: 0.0` on the boundary day matches the spec §5.2 example.
- **Season months must be contiguous** modulo the year (validated): `[12,1,2]` is one run that wraps; `[1,7]` is rejected. This is what makes "days into season" well-defined.
- **Weight math (incl. esoteric seasons — spec §3.1a):** per weather type, `eff(w) = base(w) × lerp(prevScale×prevMult(w), curScale×curMult(w), blend) + lerp(prevAdd(w), curAdd(w), blend)`. Missing multiplier ⇒ 1.0; missing addition ⇒ 0.0; `baseWeightScale` defaults 1.0. **Additions may introduce types absent from the base climate** (that's the point — a glass-rain season is YAML-only), and `baseWeightScale: 0` suppresses all normal weather. Lerping the *products* (not the factors independently) keeps the per-type factor itself linear across the window. Spawn weight: `base × lerp(prevSpawnMult, curSpawnMult)`. Influence deltas are lerped then **added** to the biome's own values; `MovementResistance` is clamped to [0,1] after addition (the sim reads it as a probability). An all-zero effective table is well-defined sim behavior: no spawns there, existing fronts keep their type until death, zones resolve Clear.
- **Unbound = untouched:** a biome with no `track` (or an unknown/rejected track) passes through `EffectiveClimate` unchanged and is absent from `ZoneSeasons`. The `default` profile ships unbound, so unknown biomes don't accidentally get seasons via the `Climate.For` fallback.
- **S1 ships two track files** (`temperate`, `monsoon`) but binds only the standard biomes → `temperate`. Monsoon is exercised by tests and available to builders; the `jungle` biome arrives with S3 content.
- **Fail-soft ladder (spec §7):** `SeasonsEnabled: false`, no track files, all tracks rejected, or an unresolvable calendar ⇒ log once, run exactly as v1. `m.seasonsOn` is the runtime gate.
- **First tick emits no events:** `startSim` establishes the baseline `zoneSeasons` map; only later diffs queue `WeatherSeasonChanged` (spec §5 step 6).

## File structure

| File | Responsibility |
|---|---|
| `sim/climate.go` (modify) | `ClimateProfile.Track` field (data only — `Step` ignores it); default bindings in `DefaultClimate()`. |
| `content/climate.go` (modify) | `track:` passthrough in climate file parsing. |
| `seasons/track.go` (new) | `Season`/`Track`/`Tracks` types, YAML parsing, `Load`, validation (coverage, contiguity, ranges). |
| `seasons/resolve.go` (new) | Engine-mirroring month math, `(Track).Resolve(dayOfYear)`, blend. |
| `seasons/apply.go` (new) | `CalendarPos`, `ZoneSeason`, `EffectiveClimate`, `ZoneSeasons`. |
| `seasons/arch_test.go`, `seasons/moduledata_test.go` (new) | Purity guard; shipped track-file validation. |
| `files/datafiles/seasons/temperate.yaml`, `monsoon.yaml` (new) | Default track data. |
| `engine/calendar.go` (new) | `CalendarShape()`, `CalendarNow()` over gametime. |
| `weather_config.go` (modify) | `SeasonsEnabled` (default true) + overlay key. |
| `weather_events.go` (new) | Exported `WeatherSeasonChanged` event type. |
| `weather_tick.go` (modify) | Track loading in `startSim`, per-tick effective climate, season map diff + events. |
| `weather_api.go`, `weather_commands.go` (modify) | `GetSeason` export; season in `weather`/`status` + `weather seasons` subcommand. |
| `seasons/context.md` (new) + sim/content/engine/root `context.md` (modify) | Documentation. |

**Test commands.** Standalone: `go test ./sim/... ./crawler/... ./content/... ./seasons/...`. Engine-coupled (after every edit to `engine/` or root): `pwsh scripts/sync-to-checkout.ps1 -Checkout "$HOME\workspace\GoMud"` then, in the checkout, `go test ./modules/weather/...`. Never `go test ./...` in this repo. Commits: conventional style, each message ending `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`. Do NOT push.

---

## Task 1: `Track` field on climate profiles

**Files:** Modify `sim/climate.go`, `sim/climate_test.go`.

- [ ] **Step 1: Write failing test in `sim/climate_test.go`**

```go
func TestDefaultClimateTrackBindings(t *testing.T) {
	c := DefaultClimate()
	for _, biome := range []string{"plains", "forest", "mountain", "tundra", "swamp", "ocean"} {
		if c[biome].Track != "temperate" {
			t.Errorf("%s should bind to temperate, got %q", biome, c[biome].Track)
		}
	}
	for _, biome := range []string{"desert", "default"} {
		if c[biome].Track != "" {
			t.Errorf("%s should be unbound, got %q", biome, c[biome].Track)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./sim/ -run TestDefaultClimateTrackBindings`
Expected: FAIL (`c[biome].Track undefined`).

- [ ] **Step 3: Implement.** In `sim/climate.go`, add the field to `ClimateProfile` (after `SpawnWeight`):

```go
	// Track names the season cycle this biome follows (seasons package);
	// "" = no seasons for this biome. Carried as data — Step ignores it.
	Track string `json:"track,omitempty"`
```

In `DefaultClimate()`, add `Track: "temperate",` to the `plains`, `forest`, `mountain`, `tundra`, `swamp`, and `ocean` profiles (NOT `default`, NOT `desert`).

- [ ] **Step 4: Run `go test ./sim/...`** — PASS (the golden-trace and determinism tests must be untouched: the field is inert data).

- [ ] **Step 5: Commit**

```bash
git add sim/climate.go sim/climate_test.go
git commit -m "feat(sim): ClimateProfile.Track season-cycle binding (inert data)"
```

## Task 2: `track:` passthrough in climate file parsing

**Files:** Modify `content/climate.go`, `content/climate_test.go`.

- [ ] **Step 1: Add failing test in `content/climate_test.go`**

```go
func TestParseClimateTrack(t *testing.T) {
	_, p, err := ParseClimate([]byte("biome: jungle\ntrack: monsoon\nweather:\n  rain: 5\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Track != "monsoon" {
		t.Errorf("track not parsed: %q", p.Track)
	}
	// Omitted track stays empty (unbound).
	_, p2, err := ParseClimate([]byte("biome: cave\nweather:\n  clear: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p2.Track != "" {
		t.Errorf("omitted track should be empty: %q", p2.Track)
	}
}
```

- [ ] **Step 2: Run `go test ./content/ -run TestParseClimateTrack`** — FAIL.

- [ ] **Step 3: Implement.** In `content/climate.go`: add `Track string \`yaml:"track"\`` to the `climateFile` struct, and `Track: cf.Track,` to the `sim.ClimateProfile` literal in `ParseClimate`.

- [ ] **Step 4: Run `go test ./content/...`** — PASS.

- [ ] **Step 5: Commit**

```bash
git add content/climate.go content/climate_test.go
git commit -m "feat(content): parse optional track binding from climate files"
```

## Task 3: `seasons` package — types, parsing, validation

**Files:** Create `seasons/track.go`, `seasons/track_test.go`, `seasons/doc.go`, `seasons/arch_test.go`.

- [ ] **Step 1: Write failing tests `seasons/track_test.go`**

```go
package seasons

import (
	"strings"
	"testing"
	"testing/fstest"
)

const temperateYAML = `track: temperate
seasons:
  - name: winter
    months: [12, 1, 2]
    transitionDays: 6
    weatherWeightMultipliers: { snow: 3.0, heatwave: 0.0 }
    spawnWeightMultiplier: 0.9
    influence: { intensityDelta: -0.02 }
  - name: spring
    months: [3, 4, 5]
    transitionDays: 6
  - name: summer
    months: [6, 7, 8]
    transitionDays: 6
    weatherWeightMultipliers: { storm: 1.4 }
  - name: autumn
    months: [9, 10, 11]
    transitionDays: 6
`

func loadOne(t *testing.T, yaml string) Tracks {
	t.Helper()
	fsys := fstest.MapFS{"seasons/x.yaml": {Data: []byte(yaml)}}
	tracks, errs := Load(fsys, "seasons", 12, 365)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	return tracks
}

func TestLoadTemperate(t *testing.T) {
	tracks := loadOne(t, temperateYAML)
	tr, ok := tracks["temperate"]
	if !ok {
		t.Fatalf("track not keyed by name: %v", tracks)
	}
	if len(tr.Seasons) != 4 {
		t.Fatalf("expected 4 seasons, got %d", len(tr.Seasons))
	}
	w := tr.Seasons[0]
	if w.Name != "winter" || w.TransitionDays != 6 || w.SpawnWeightMultiplier != 0.9 {
		t.Errorf("winter mis-parsed: %+v", w)
	}
	if w.WeatherWeightMultipliers["snow"] != 3.0 || w.WeatherWeightMultipliers["heatwave"] != 0.0 {
		t.Errorf("multipliers mis-parsed: %+v", w.WeatherWeightMultipliers)
	}
	if w.Influence.IntensityDelta != -0.02 {
		t.Errorf("influence mis-parsed: %+v", w.Influence)
	}
	// Unset spawn multiplier defaults to 1.0.
	if tr.Seasons[1].SpawnWeightMultiplier != 1.0 {
		t.Errorf("unset spawnWeightMultiplier should default 1.0: %v", tr.Seasons[1].SpawnWeightMultiplier)
	}
}

func TestLoadEsotericFields(t *testing.T) {
	yaml := `track: stillness
seasons:
  - name: calm
    months: [1,2,3,4,5,6,7,8,9,10]
  - name: shattering
    months: [11,12]
    transitionDays: 2
    baseWeightScale: 0.0
    weatherWeightAdditions: { glassrain: 8, ashfall: 3 }
`
	tr := loadOne(t, yaml)["stillness"]
	calm, shat := tr.Seasons[0], tr.Seasons[1]
	if calm.BaseWeightScale != 1.0 {
		t.Errorf("unset baseWeightScale should default 1.0: %v", calm.BaseWeightScale)
	}
	if shat.BaseWeightScale != 0.0 {
		t.Errorf("explicit baseWeightScale 0 not honored: %v", shat.BaseWeightScale)
	}
	if shat.WeatherWeightAdditions["glassrain"] != 8 || shat.WeatherWeightAdditions["ashfall"] != 3 {
		t.Errorf("additions mis-parsed: %+v", shat.WeatherWeightAdditions)
	}
	if len(calm.WeatherWeightAdditions) != 0 {
		t.Errorf("calm should have no additions: %+v", calm.WeatherWeightAdditions)
	}
}

func TestLoadValidation(t *testing.T) {
	cases := []struct{ name, yaml, wantErr string }{
		{"gap", "track: t\nseasons:\n  - name: a\n    months: [1,2,3,4,5,6]\n", "claimed by no season"},
		{"overlap", "track: t\nseasons:\n  - name: a\n    months: [1,2,3,4,5,6,7]\n  - name: b\n    months: [7,8,9,10,11,12]\n", "claimed twice"},
		{"out of range", "track: t\nseasons:\n  - name: a\n    months: [1,2,3,4,5,6,13]\n  - name: b\n    months: [7,8,9,10,11,12]\n", "outside 1..12"},
		{"non-contiguous", "track: t\nseasons:\n  - name: a\n    months: [1,7,2,3,4,5]\n  - name: b\n    months: [6,8,9,10,11,12]\n", "contiguous"},
		{"no name", "track: t\nseasons:\n  - months: [1,2,3,4,5,6,7,8,9,10,11,12]\n", "missing a name"},
		{"no track key", "seasons:\n  - name: a\n    months: [1,2,3,4,5,6,7,8,9,10,11,12]\n", "missing required 'track'"},
		{"negative addition", "track: t\nseasons:\n  - name: a\n    months: [1,2,3,4,5,6,7,8,9,10,11,12]\n    weatherWeightAdditions: { glassrain: -1 }\n", "negative"},
		{"negative scale", "track: t\nseasons:\n  - name: a\n    months: [1,2,3,4,5,6,7,8,9,10,11,12]\n    baseWeightScale: -0.5\n", "negative"},
	}
	for _, c := range cases {
		fsys := fstest.MapFS{"seasons/x.yaml": {Data: []byte(c.yaml)}}
		tracks, errs := Load(fsys, "seasons", 12, 365)
		if len(errs) == 0 || !strings.Contains(errs[0].Error(), c.wantErr) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.wantErr, errs)
		}
		if len(tracks) != 0 {
			t.Errorf("%s: invalid track must be dropped, got %v", c.name, tracks)
		}
	}
}

func TestLoadMissingDirIsEmpty(t *testing.T) {
	tracks, errs := Load(fstest.MapFS{}, "seasons", 12, 365)
	if len(tracks) != 0 || len(errs) != 0 {
		t.Fatalf("missing dir: want empty/no errors, got %v %v", tracks, errs)
	}
}

func TestLoadRejectsDuplicateTrackNames(t *testing.T) {
	fsys := fstest.MapFS{
		"seasons/a.yaml": {Data: []byte("track: t\nseasons:\n  - name: a\n    months: [1,2,3,4,5,6,7,8,9,10,11,12]\n")},
		"seasons/b.yaml": {Data: []byte("track: t\nseasons:\n  - name: b\n    months: [1,2,3,4,5,6,7,8,9,10,11,12]\n")},
	}
	tracks, errs := Load(fsys, "seasons", 12, 365)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "duplicate track") {
		t.Errorf("want duplicate-track error, got %v", errs)
	}
	if len(tracks) != 1 {
		t.Errorf("first track wins; got %d tracks", len(tracks))
	}
}
```

- [ ] **Step 2: Run `go test ./seasons/`** — FAIL (package doesn't exist).

- [ ] **Step 3: Implement.** `seasons/doc.go`:

```go
// Package seasons resolves data-defined season tracks (temperate, monsoon,
// anything a builder writes) against the game calendar and applies them as a
// pure transform over sim.Climate. It is the architecture's regression
// guarantee for v2: sim.Step never changes — it just receives this tick's
// effective climate. Pure: no engine imports (enforced by arch_test.go);
// season state is never persisted because it is always derivable from the
// calendar position.
package seasons
```

`seasons/track.go`:

```go
package seasons

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
	"gopkg.in/yaml.v2"
)

// Season is one phase of a track's cycle.
type Season struct {
	Name                     string
	Months                   []int // 1-based calendar months, one contiguous run (may wrap)
	TransitionDays           int   // blend window at the START of this season; 0 = hard flip
	WeatherWeightMultipliers map[sim.WeatherType]float64
	WeatherWeightAdditions   map[sim.WeatherType]float64 // absolute weights ADDED — may introduce types absent from the base climate (esoteric seasons, spec §3.1a)
	BaseWeightScale          float64                     // scales ALL base weights before additions; 1.0 = neutral, 0 = suppress normal weather
	SpawnWeightMultiplier    float64
	Influence                sim.WeatherInfluence // deltas ADDED to the biome's own

	startMonth int // derived at load: first month of the contiguous run
}

// Track is one season cycle plus the calendar shape it was validated against.
type Track struct {
	Name    string
	Seasons []Season

	monthsPerYear int
	daysPerYear   int
}

// Tracks maps track name -> track.
type Tracks map[string]Track

// trackFile mirrors the on-disk schema (design spec §3.1).
type trackFile struct {
	Track   string `yaml:"track"`
	Seasons []struct {
		Name                     string             `yaml:"name"`
		Months                   []int              `yaml:"months"`
		TransitionDays           int                `yaml:"transitionDays"`
		WeatherWeightMultipliers map[string]float64 `yaml:"weatherWeightMultipliers"`
		WeatherWeightAdditions   map[string]float64 `yaml:"weatherWeightAdditions"`
		BaseWeightScale          *float64           `yaml:"baseWeightScale"`
		SpawnWeightMultiplier    *float64           `yaml:"spawnWeightMultiplier"`
		Influence                struct {
			IntensityDelta     float64 `yaml:"intensityDelta"`
			MoistureDelta      float64 `yaml:"moistureDelta"`
			MovementResistance float64 `yaml:"movementResistance"`
		} `yaml:"influence"`
	} `yaml:"seasons"`
}

// Load parses every *.yaml track under dir and validates each against the
// world's calendar shape. Invalid tracks are dropped and reported; valid ones
// are returned — the caller logs the errors and continues (fail-soft). A
// missing dir is no tracks, no errors.
func Load(fsys fs.FS, dir string, monthsPerYear, daysPerYear int) (Tracks, []error) {
	tracks := Tracks{}
	var errs []error
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return tracks, nil // dir not shipped
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		tr, err := parseTrack(b, monthsPerYear, daysPerYear)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		if _, dup := tracks[tr.Name]; dup {
			errs = append(errs, fmt.Errorf("%s: duplicate track %q (first wins)", e.Name(), tr.Name))
			continue
		}
		tracks[tr.Name] = tr
	}
	return tracks, errs
}

// parseTrack parses and validates one track file against the calendar shape.
func parseTrack(b []byte, monthsPerYear, daysPerYear int) (Track, error) {
	var tf trackFile
	if err := yaml.Unmarshal(b, &tf); err != nil {
		return Track{}, err
	}
	if tf.Track == "" {
		return Track{}, fmt.Errorf("missing required 'track' key")
	}
	if monthsPerYear < 1 || daysPerYear < 1 {
		return Track{}, fmt.Errorf("invalid calendar shape (%d months, %d days)", monthsPerYear, daysPerYear)
	}

	tr := Track{Name: tf.Track, monthsPerYear: monthsPerYear, daysPerYear: daysPerYear}
	claimed := make(map[int]string, monthsPerYear) // month -> season name

	for _, s := range tf.Seasons {
		if s.Name == "" {
			return Track{}, fmt.Errorf("track %q: a season is missing a name", tf.Track)
		}
		if len(s.Months) == 0 {
			return Track{}, fmt.Errorf("track %q: season %q claims no months", tf.Track, s.Name)
		}
		for _, m := range s.Months {
			if m < 1 || m > monthsPerYear {
				return Track{}, fmt.Errorf("track %q: season %q month %d outside 1..%d", tf.Track, s.Name, m, monthsPerYear)
			}
			if prev, dup := claimed[m]; dup {
				return Track{}, fmt.Errorf("track %q: month %d claimed twice (%s and %s)", tf.Track, m, prev, s.Name)
			}
			claimed[m] = s.Name
		}
		start, contiguous := contiguousStart(s.Months, monthsPerYear)
		if !contiguous {
			return Track{}, fmt.Errorf("track %q: season %q months must form one contiguous run (modulo the year)", tf.Track, s.Name)
		}
		spawnMult := 1.0
		if s.SpawnWeightMultiplier != nil {
			spawnMult = *s.SpawnWeightMultiplier
		}
		baseScale := 1.0
		if s.BaseWeightScale != nil {
			baseScale = *s.BaseWeightScale
		}
		if baseScale < 0 {
			return Track{}, fmt.Errorf("track %q: season %q baseWeightScale is negative", tf.Track, s.Name)
		}
		mults := make(map[sim.WeatherType]float64, len(s.WeatherWeightMultipliers))
		for k, v := range s.WeatherWeightMultipliers {
			if v < 0 {
				return Track{}, fmt.Errorf("track %q: season %q multiplier for %q is negative", tf.Track, s.Name, k)
			}
			mults[sim.WeatherType(k)] = v
		}
		adds := make(map[sim.WeatherType]float64, len(s.WeatherWeightAdditions))
		for k, v := range s.WeatherWeightAdditions {
			if v < 0 {
				return Track{}, fmt.Errorf("track %q: season %q addition for %q is negative", tf.Track, s.Name, k)
			}
			adds[sim.WeatherType(k)] = v
		}
		tr.Seasons = append(tr.Seasons, Season{
			Name:                     s.Name,
			Months:                   append([]int(nil), s.Months...),
			TransitionDays:           max(0, s.TransitionDays),
			WeatherWeightMultipliers: mults,
			WeatherWeightAdditions:   adds,
			BaseWeightScale:          baseScale,
			SpawnWeightMultiplier:    spawnMult,
			Influence: sim.WeatherInfluence{
				IntensityDelta:     s.Influence.IntensityDelta,
				MoistureDelta:      s.Influence.MoistureDelta,
				MovementResistance: s.Influence.MovementResistance,
			},
			startMonth: start,
		})
	}
	if len(tr.Seasons) == 0 {
		return Track{}, fmt.Errorf("track %q: no seasons defined", tf.Track)
	}
	for m := 1; m <= monthsPerYear; m++ {
		if _, ok := claimed[m]; !ok {
			return Track{}, fmt.Errorf("track %q: month %d claimed by no season", tf.Track, m)
		}
	}
	return tr, nil
}

// contiguousStart reports whether months form one contiguous run modulo the
// year, and returns the run's first month. A run wraps when month n is
// followed by month 1 (e.g. [12 1 2]).
func contiguousStart(months []int, monthsPerYear int) (start int, ok bool) {
	in := make(map[int]bool, len(months))
	for _, m := range months {
		in[m] = true
	}
	if len(in) == monthsPerYear { // whole year = trivially contiguous
		return months[0], true
	}
	// The start month is the unique claimed month whose predecessor is unclaimed.
	start = 0
	for m := range in {
		prev := m - 1
		if prev < 1 {
			prev = monthsPerYear
		}
		if !in[prev] {
			if start != 0 {
				return 0, false // two run-starts = not contiguous
			}
			start = m
		}
	}
	return start, start != 0
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
```

`seasons/arch_test.go` — copy `content/arch_test.go` verbatim, changing the package clause to `seasons`, the function name to `TestSeasonsPackageStaysPure`, and the error text to `(seasons must stay pure)`.

- [ ] **Step 4: Run `go test ./seasons/`** — PASS.

- [ ] **Step 5: Commit**

```bash
git add seasons/
git commit -m "feat(seasons): track types, YAML parsing and calendar validation"
```

## Task 4: Resolution and blending

**Files:** Create `seasons/resolve.go`, `seasons/resolve_test.go`.

- [ ] **Step 1: Write failing tests `seasons/resolve_test.go`**

```go
package seasons

import (
	"math"
	"testing"
)

// Stock calendar: 365 days, 12 months, hoursPerMonth = 365*24/12 = 730.
// Engine month formula: month = 1 + floor(day*24/730), clamped to 12.
// Month 1 = days 1..30, month 12 = days 335..365 (clamp).
func TestMonthOfDayMirrorsEngine(t *testing.T) {
	cases := []struct{ day, want int }{
		{1, 1}, {30, 1}, {31, 2}, {304, 10}, {305, 11}, {334, 11}, {335, 12}, {365, 12},
	}
	for _, c := range cases {
		if got := monthOfDay(c.day, 365, 12); got != c.want {
			t.Errorf("day %d: month %d, want %d", c.day, got, c.want)
		}
	}
}

func TestResolveSeasonsAndBlend(t *testing.T) {
	tr := loadOne(t, temperateYAML)["temperate"]

	// Mid-summer (month 7 ≈ days 183..212): fully summer.
	cur, prev, blend := tr.Resolve(200)
	if cur != "summer" || prev != "spring" || blend != 1.0 {
		t.Errorf("day 200: %s/%s/%v", cur, prev, blend)
	}

	// Winter starts at month 12 (day 335). transitionDays=6:
	// day 335 -> daysInto 0 -> blend 0.0 (continuous with autumn).
	cur, prev, blend = tr.Resolve(335)
	if cur != "winter" || prev != "autumn" || blend != 0.0 {
		t.Errorf("day 335: %s/%s/%v", cur, prev, blend)
	}
	// day 338 -> daysInto 3 -> blend 0.5.
	if _, _, b := tr.Resolve(338); math.Abs(b-0.5) > 1e-9 {
		t.Errorf("day 338 blend: %v", b)
	}
	// day 341+ -> blend 1.0.
	if _, _, b := tr.Resolve(341); b != 1.0 {
		t.Errorf("day 341 blend: %v", b)
	}

	// Wraparound: winter spans 12,1,2 — day 10 is still winter, fully blended
	// (daysInto = 10-1 + (365-335+1) = 40 >= 6).
	cur, prev, blend = tr.Resolve(10)
	if cur != "winter" || prev != "autumn" || blend != 1.0 {
		t.Errorf("day 10: %s/%s/%v", cur, prev, blend)
	}

	// Spring starts month 3 (day 61): blend 0 at start, prev = winter.
	cur, prev, blend = tr.Resolve(61)
	if cur != "spring" || prev != "winter" || blend != 0.0 {
		t.Errorf("day 61: %s/%s/%v", cur, prev, blend)
	}
}

func TestResolveHardFlip(t *testing.T) {
	yaml := "track: t\nseasons:\n  - name: a\n    months: [1,2,3,4,5,6]\n  - name: b\n    months: [7,8,9,10,11,12]\n"
	tr := loadOne(t, yaml)["t"]
	// transitionDays 0 => blend 1.0 immediately on the boundary day.
	// Month 7's first day: smallest d with 1+floor(d*24/730) == 7, i.e.
	// d*24/730 >= 6 → d >= 182.5 → day 183.
	if cur, _, blend := tr.Resolve(183); cur != "b" || blend != 1.0 {
		t.Errorf("hard flip: %s/%v", cur, blend)
	}
}

func TestSingleSeasonTrack(t *testing.T) {
	yaml := "track: t\nseasons:\n  - name: always\n    months: [1,2,3,4,5,6,7,8,9,10,11,12]\n"
	tr := loadOne(t, yaml)["t"]
	cur, prev, blend := tr.Resolve(100)
	if cur != "always" || prev != "always" || blend != 1.0 {
		t.Errorf("single season: %s/%s/%v", cur, prev, blend)
	}
}
```

- [ ] **Step 2: Run `go test ./seasons/ -run 'TestMonthOfDay|TestResolve|TestSingleSeason'`** — FAIL.

- [ ] **Step 3: Implement `seasons/resolve.go`**

```go
package seasons

// monthOfDay mirrors the engine's month formula EXACTLY
// (internal/gametime/gametime.go:268-271): month = 1 + floor(day*24 /
// hoursPerMonth), hoursPerMonth = daysPerYear*24/numMonths, clamped to
// numMonths — so a season's month claims always agree with the 'time' command.
func monthOfDay(dayOfYear, daysPerYear, monthsPerYear int) int {
	hoursPerMonth := float64(daysPerYear) * 24.0 / float64(monthsPerYear)
	m := 1 + int(float64(dayOfYear)*24.0/hoursPerMonth)
	if m > monthsPerYear {
		m = monthsPerYear
	}
	return m
}

// firstDayOfMonth is the smallest day-of-year d with monthOfDay(d) == m.
func firstDayOfMonth(m, daysPerYear, monthsPerYear int) int {
	if m <= 1 {
		return 1
	}
	hoursPerMonth := float64(daysPerYear) * 24.0 / float64(monthsPerYear)
	d := int(float64(m-1)*hoursPerMonth/24.0) + 1
	// Guard float edges: walk to the exact boundary.
	for d > 1 && monthOfDay(d-1, daysPerYear, monthsPerYear) >= m {
		d--
	}
	for monthOfDay(d, daysPerYear, monthsPerYear) < m {
		d++
	}
	return d
}

// Resolve reports where this track stands on a day of the year: the current
// season, the season it is transitioning FROM, and the blend factor.
// blend = clamp(daysIntoSeason / transitionDays, 0, 1); 0 on the boundary day
// (odds still fully the previous season's — continuous across the flip), 1
// once the window has elapsed. transitionDays <= 0 means an immediate 1.
func (t Track) Resolve(dayOfYear int) (current, previous string, blend float64) {
	if len(t.Seasons) == 1 {
		s := t.Seasons[0].Name
		return s, s, 1.0
	}
	m := monthOfDay(dayOfYear, t.daysPerYear, t.monthsPerYear)

	curIdx := -1
	for i, s := range t.Seasons {
		for _, sm := range s.Months {
			if sm == m {
				curIdx = i
			}
		}
	}
	cur := t.Seasons[curIdx]

	// Previous season = the one claiming the month before cur's start month.
	prevMonth := cur.startMonth - 1
	if prevMonth < 1 {
		prevMonth = t.monthsPerYear
	}
	prevIdx := curIdx
	for i, s := range t.Seasons {
		for _, sm := range s.Months {
			if sm == prevMonth {
				prevIdx = i
			}
		}
	}

	daysInto := dayOfYear - firstDayOfMonth(cur.startMonth, t.daysPerYear, t.monthsPerYear)
	if daysInto < 0 { // season wraps the year boundary
		daysInto += t.daysPerYear
	}
	blend = 1.0
	if cur.TransitionDays > 0 {
		blend = float64(daysInto) / float64(cur.TransitionDays)
		if blend > 1 {
			blend = 1
		}
	}
	return cur.Name, t.Seasons[prevIdx].Name, blend
}

// season returns the named season (load-time validation guarantees presence).
func (t Track) season(name string) Season {
	for _, s := range t.Seasons {
		if s.Name == name {
			return s
		}
	}
	return Season{SpawnWeightMultiplier: 1.0, BaseWeightScale: 1.0}
}
```

- [ ] **Step 4: Run `go test ./seasons/`** — PASS.

- [ ] **Step 5: Commit**

```bash
git add seasons/resolve.go seasons/resolve_test.go
git commit -m "feat(seasons): engine-mirroring season resolution with blend windows"
```

## Task 5: `EffectiveClimate` and `ZoneSeasons`

**Files:** Create `seasons/apply.go`, `seasons/apply_test.go`.

- [ ] **Step 1: Write failing tests `seasons/apply_test.go`**

```go
package seasons

import (
	"math"
	"testing"

	"github.com/GoMudEngine/GoMud/modules/weather/sim"
)

func testClimate() sim.Climate {
	return sim.Climate{
		"tundra": {
			Weather:     map[sim.WeatherType]float64{"snow": 2, "clear": 4, "heatwave": 1},
			Influence:   sim.WeatherInfluence{IntensityDelta: -0.05, MovementResistance: 0.2},
			SpawnWeight: 1.0,
			Track:       "temperate",
		},
		"desert": { // unbound — must pass through untouched
			Weather:     map[sim.WeatherType]float64{"dust": 3},
			SpawnWeight: 0.7,
		},
	}
}

func TestEffectiveClimateFullSeason(t *testing.T) {
	tracks := loadOne(t, temperateYAML)
	// Day 200 = mid-summer, blend 1.0; summer multiplies storm only (x1.4),
	// snow/heatwave/clear untouched by summer.
	eff := EffectiveClimate(testClimate(), tracks, CalendarPos{DayOfYear: 200, DaysPerYear: 365})
	if eff["tundra"].Weather["snow"] != 2 || eff["tundra"].Weather["clear"] != 4 {
		t.Errorf("summer should not change snow/clear: %+v", eff["tundra"].Weather)
	}
	// Mid-winter (day 20, blend 1.0): snow x3, heatwave x0.
	eff = EffectiveClimate(testClimate(), tracks, CalendarPos{DayOfYear: 20, DaysPerYear: 365})
	if eff["tundra"].Weather["snow"] != 6 {
		t.Errorf("winter snow: want 6, got %v", eff["tundra"].Weather["snow"])
	}
	if eff["tundra"].Weather["heatwave"] != 0 {
		t.Errorf("winter heatwave: want 0, got %v", eff["tundra"].Weather["heatwave"])
	}
	if got := eff["tundra"].SpawnWeight; math.Abs(got-0.9) > 1e-9 {
		t.Errorf("winter spawn weight: want 0.9, got %v", got)
	}
	if got := eff["tundra"].Influence.IntensityDelta; math.Abs(got-(-0.07)) > 1e-9 {
		t.Errorf("winter influence: want -0.07 (-0.05 biome + -0.02 season), got %v", got)
	}
}

func TestEffectiveClimateBlends(t *testing.T) {
	tracks := loadOne(t, temperateYAML)
	// Day 338: winter, blend 0.5. Autumn (prev) has no snow multiplier (=1.0),
	// winter has 3.0 -> effective 2.0 -> weight 2*2 = 4.
	eff := EffectiveClimate(testClimate(), tracks, CalendarPos{DayOfYear: 338, DaysPerYear: 365})
	if got := eff["tundra"].Weather["snow"]; math.Abs(got-4) > 1e-9 {
		t.Errorf("blended snow: want 4, got %v", got)
	}
}

func TestEffectiveClimateLeavesUnboundAndBase(t *testing.T) {
	base := testClimate()
	tracks := loadOne(t, temperateYAML)
	eff := EffectiveClimate(base, tracks, CalendarPos{DayOfYear: 20, DaysPerYear: 365})
	// Unbound biome untouched.
	if eff["desert"].Weather["dust"] != 3 || eff["desert"].SpawnWeight != 0.7 {
		t.Errorf("desert must pass through: %+v", eff["desert"])
	}
	// Base climate not mutated (deep copy).
	if base["tundra"].Weather["snow"] != 2 {
		t.Errorf("base climate mutated: %v", base["tundra"].Weather["snow"])
	}
	// Unknown track behaves as unbound.
	base2 := sim.Climate{"x": {Weather: map[sim.WeatherType]float64{"rain": 1}, Track: "nope"}}
	eff2 := EffectiveClimate(base2, tracks, CalendarPos{DayOfYear: 20, DaysPerYear: 365})
	if eff2["x"].Weather["rain"] != 1 {
		t.Errorf("unknown track must pass through: %+v", eff2["x"])
	}
}

const shatteringYAML = `track: stillness
seasons:
  - name: calm
    months: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
  - name: shattering
    months: [11, 12]
    transitionDays: 2
    baseWeightScale: 0.0
    weatherWeightAdditions: { glassrain: 8 }
`

func TestEffectiveClimateEsotericSeason(t *testing.T) {
	tracks := loadOne(t, shatteringYAML)
	base := sim.Climate{"plateau": {
		Weather:     map[sim.WeatherType]float64{"clear": 5, "rain": 2},
		SpawnWeight: 1.0,
		Track:       "stillness",
	}}
	// Month 11 starts at day 305; transitionDays=2 => day 307 is fully shattering.
	eff := EffectiveClimate(base, tracks, CalendarPos{DayOfYear: 307, DaysPerYear: 365})
	w := eff["plateau"].Weather
	if w["clear"] != 0 || w["rain"] != 0 {
		t.Errorf("baseWeightScale 0 must suppress normal weather: %+v", w)
	}
	if w["glassrain"] != 8 {
		t.Errorf("addition must introduce the new type: %+v", w)
	}
	// Mid-window (day 306, blend 0.5): base halved, addition half-strength.
	eff = EffectiveClimate(base, tracks, CalendarPos{DayOfYear: 306, DaysPerYear: 365})
	w = eff["plateau"].Weather
	if math.Abs(w["clear"]-2.5) > 1e-9 || math.Abs(w["glassrain"]-4) > 1e-9 {
		t.Errorf("esoteric blend wrong: %+v", w)
	}
	// Back in calm (day 100): no glassrain entry leaks.
	eff = EffectiveClimate(base, tracks, CalendarPos{DayOfYear: 100, DaysPerYear: 365})
	if v, ok := eff["plateau"].Weather["glassrain"]; ok && v != 0 {
		t.Errorf("glassrain must not persist outside its season: %v", v)
	}
}

func TestZoneSeasons(t *testing.T) {
	g := &sim.Graph{Nodes: map[string]sim.ZoneNode{
		"Frost": {Zone: "Frost", Biome: "tundra"},
		"Dune":  {Zone: "Dune", Biome: "desert"},
	}}
	zs := ZoneSeasons(g, testClimate(), loadOne(t, temperateYAML), CalendarPos{DayOfYear: 20, DaysPerYear: 365})
	got, ok := zs["Frost"]
	if !ok || got.Track != "temperate" || got.Season != "winter" || got.Blend != 1.0 {
		t.Errorf("Frost: %+v ok=%v", got, ok)
	}
	if _, ok := zs["Dune"]; ok {
		t.Error("unbound zone must be absent from the map")
	}
}
```

- [ ] **Step 2: Run `go test ./seasons/`** — FAIL (undefined: EffectiveClimate, ...).

- [ ] **Step 3: Implement `seasons/apply.go`**

```go
package seasons

import "github.com/GoMudEngine/GoMud/modules/weather/sim"

// CalendarPos is the calendar position the engine resolves from the round.
type CalendarPos struct {
	DayOfYear   int
	DaysPerYear int
}

// ZoneSeason is one zone's resolved season.
type ZoneSeason struct {
	Track  string
	Season string
	Blend  float64
}

func lerp(p, c, blend float64) float64 { return p + (c-p)*blend }

// mult returns a season's multiplier for one weather type (missing = 1.0).
func mult(m map[sim.WeatherType]float64, key sim.WeatherType) float64 {
	if v, ok := m[key]; ok {
		return v
	}
	return 1.0
}

// EffectiveClimate returns a season-adjusted COPY of base for this calendar
// position. Biomes without a track (or with an unknown track) pass through
// unchanged. Per weather type:
//
//	eff(w) = base(w) × lerp(prevScale×prevMult(w), curScale×curMult(w))
//	         + lerp(prevAdd(w), curAdd(w))
//
// Additions may introduce types absent from the base climate (esoteric
// seasons, spec §3.1a) and lerp from/to 0 across the window. Spawn weight is
// multiplied by the blended seasonal multiplier; influence deltas are blended
// then ADDED to the biome's own values; MovementResistance is clamped to
// [0,1] afterwards (the sim treats it as a probability).
func EffectiveClimate(base sim.Climate, tracks Tracks, pos CalendarPos) sim.Climate {
	out := make(sim.Climate, len(base))
	for biome, p := range base {
		tr, ok := tracks[p.Track]
		if p.Track == "" || !ok {
			out[biome] = p // pass-through; maps shared but never written below
			continue
		}
		curName, prevName, blend := tr.Resolve(pos.DayOfYear)
		cur, prev := tr.season(curName), tr.season(prevName)

		np := p // copy struct
		np.Weather = make(map[sim.WeatherType]float64, len(p.Weather)+len(cur.WeatherWeightAdditions)+len(prev.WeatherWeightAdditions))
		for w, weight := range p.Weather {
			factor := lerp(prev.BaseWeightScale*mult(prev.WeatherWeightMultipliers, w),
				cur.BaseWeightScale*mult(cur.WeatherWeightMultipliers, w), blend)
			np.Weather[w] = weight * factor
		}
		// Additions: union of both seasons' addition keys, lerped (missing = 0).
		for w := range prev.WeatherWeightAdditions {
			np.Weather[w] += lerp(prev.WeatherWeightAdditions[w], cur.WeatherWeightAdditions[w], blend)
		}
		for w := range cur.WeatherWeightAdditions {
			if _, done := prev.WeatherWeightAdditions[w]; done {
				continue // already lerped above
			}
			np.Weather[w] += lerp(0, cur.WeatherWeightAdditions[w], blend)
		}
		np.SpawnWeight = p.SpawnWeight * lerp(prev.SpawnWeightMultiplier, cur.SpawnWeightMultiplier, blend)
		np.Influence.IntensityDelta = p.Influence.IntensityDelta + lerp(prev.Influence.IntensityDelta, cur.Influence.IntensityDelta, blend)
		np.Influence.MoistureDelta = p.Influence.MoistureDelta + lerp(prev.Influence.MoistureDelta, cur.Influence.MoistureDelta, blend)
		mr := p.Influence.MovementResistance + lerp(prev.Influence.MovementResistance, cur.Influence.MovementResistance, blend)
		if mr < 0 {
			mr = 0
		} else if mr > 1 {
			mr = 1
		}
		np.Influence.MovementResistance = mr
		out[biome] = np
	}
	return out
}

// ZoneSeasons maps every zone to its resolved season via its biome's track.
// Zones whose biome has no (known) track are absent. Biome resolution uses
// base.For's default fallback, matching how the sim resolves climate.
func ZoneSeasons(g *sim.Graph, base sim.Climate, tracks Tracks, pos CalendarPos) map[sim.ZoneId]ZoneSeason {
	out := map[sim.ZoneId]ZoneSeason{}
	for _, z := range g.Zones() {
		p := base.For(g.Nodes[z].Biome)
		tr, ok := tracks[p.Track]
		if p.Track == "" || !ok {
			continue
		}
		cur, _, blend := tr.Resolve(pos.DayOfYear)
		out[z] = ZoneSeason{Track: tr.Name, Season: cur, Blend: blend}
	}
	return out
}
```

- [ ] **Step 4: Run `go test ./seasons/` and `go test ./sim/...`** — PASS (sim untouched).

- [ ] **Step 5: Commit**

```bash
git add seasons/apply.go seasons/apply_test.go
git commit -m "feat(seasons): pure effective-climate transform and per-zone season map"
```

## Task 6: Default track data + shipped-data validation

**Files:** Create `files/datafiles/seasons/temperate.yaml`, `files/datafiles/seasons/monsoon.yaml`, `seasons/moduledata_test.go`.

- [ ] **Step 1: Write the validation test `seasons/moduledata_test.go`** (reads the real shipped files relative to the package, like `content/moduledata_test.go`):

```go
package seasons

import (
	"os"
	"testing"
)

// TestShippedTracks validates the default track files against the stock
// 12-month/365-day calendar (worlds with other calendars must author their
// own — design spec S-R3).
func TestShippedTracks(t *testing.T) {
	tracks, errs := Load(os.DirFS("../files/datafiles"), "seasons", 12, 365)
	if len(errs) != 0 {
		t.Fatalf("shipped tracks invalid: %v", errs)
	}
	tr, ok := tracks["temperate"]
	if !ok || len(tr.Seasons) != 4 {
		t.Fatalf("temperate track missing or wrong shape: %+v", tracks)
	}
	if mo, ok := tracks["monsoon"]; !ok || len(mo.Seasons) != 2 {
		t.Fatalf("monsoon track missing or wrong shape: %+v", tracks)
	}
	// Every multiplier references a weather type some default climate knows,
	// so a typo'd type can't silently no-op.
	known := map[string]bool{"clear": true, "overcast": true, "rain": true, "storm": true,
		"fog": true, "snow": true, "blizzard": true, "dust": true, "heatwave": true}
	for name, track := range tracks {
		for _, s := range track.Seasons {
			for w := range s.WeatherWeightMultipliers {
				if !known[string(w)] {
					t.Errorf("track %s season %s: unknown weather type %q", name, s.Name, w)
				}
			}
			// Shipped tracks must not add types we ship no mutator/emote for.
			for w := range s.WeatherWeightAdditions {
				if !known[string(w)] {
					t.Errorf("track %s season %s: addition for unshipped weather type %q", name, s.Name, w)
				}
			}
		}
	}
}
```

- [ ] **Step 2: Run `go test ./seasons/ -run TestShippedTracks`** — FAIL (no files).

- [ ] **Step 3: Create the data files.**

`files/datafiles/seasons/temperate.yaml`:

```yaml
# Four-season cycle for the stock 12-month calendar. Months are 1-based; a
# season's blend window (transitionDays) sits at its START, interpolating the
# multipliers from the previous season's values.
track: temperate
seasons:
  - name: winter
    months: [12, 1, 2]
    transitionDays: 6
    weatherWeightMultipliers: { snow: 3.0, blizzard: 2.0, rain: 0.4, storm: 0.6, heatwave: 0.0 }
    spawnWeightMultiplier: 0.9
    influence: { intensityDelta: -0.02 }
  - name: spring
    months: [3, 4, 5]
    transitionDays: 6
    weatherWeightMultipliers: { rain: 1.5, fog: 1.3, snow: 0.3, blizzard: 0.0 }
    spawnWeightMultiplier: 1.1
  - name: summer
    months: [6, 7, 8]
    transitionDays: 6
    weatherWeightMultipliers: { storm: 1.4, heatwave: 1.5, snow: 0.0, blizzard: 0.0 }
    influence: { intensityDelta: 0.01 }
  - name: autumn
    months: [9, 10, 11]
    transitionDays: 6
    weatherWeightMultipliers: { fog: 1.6, overcast: 1.3, heatwave: 0.3 }
```

`files/datafiles/seasons/monsoon.yaml`:

```yaml
# Two-season wet/dry cycle (equatorial/jungle worlds). No standard biome binds
# to it by default — builders bind via 'track: monsoon' in a climate file.
track: monsoon
seasons:
  - name: wet
    months: [5, 6, 7, 8, 9, 10]
    transitionDays: 10
    weatherWeightMultipliers: { rain: 2.5, storm: 2.0, fog: 1.3, clear: 0.4, dust: 0.0 }
    spawnWeightMultiplier: 1.4
    influence: { moistureDelta: 0.03 }
  - name: dry
    months: [11, 12, 1, 2, 3, 4]
    transitionDays: 10
    weatherWeightMultipliers: { rain: 0.2, storm: 0.3, clear: 1.6, heatwave: 1.4, dust: 1.5 }
    spawnWeightMultiplier: 0.8
    influence: { moistureDelta: -0.03 }
```

- [ ] **Step 4: Run `go test ./seasons/`** — PASS.

- [ ] **Step 5: Commit**

```bash
git add files/datafiles/seasons/ seasons/moduledata_test.go
git commit -m "feat(data): default temperate and monsoon season tracks"
```

## Task 7: Engine calendar helper

> Engine-coupled from here: sync + test in the checkout after every edit (commands in the header). Build will be green after each task in this plan — there is no multi-task build unit this time.

**Files:** Create `engine/calendar.go`, `engine/calendar_test.go`.

- [ ] **Step 1: Write failing test `engine/calendar_test.go`** (the gametime-backed functions are thin verified glue; the testable core is the fallback logic):

```go
package engine

import "testing"

func TestCalendarShapeFallsBackToDefault(t *testing.T) {
	// shapeFor consults gametime.GetCalendar; unknown names fall back to the
	// "default" calendar, and a world with no calendars yields (0, 0) so the
	// caller disables seasons.
	months, days := shapeFor("no-such-calendar")
	defMonths, defDays := shapeFor("default")
	if months != defMonths || days != defDays {
		t.Errorf("unknown calendar should fall back to default: got (%d,%d) want (%d,%d)",
			months, days, defMonths, defDays)
	}
}
```

- [ ] **Step 2: Sync + run (checkout): `go test ./modules/weather/engine/ -run TestCalendarShape`** — FAIL.

- [ ] **Step 3: Implement `engine/calendar.go`**

```go
package engine

import (
	"github.com/GoMudEngine/GoMud/internal/gametime"
	"github.com/GoMudEngine/GoMud/modules/weather/seasons"
)

// shapeFor returns (monthsPerYear, daysPerYear) for a named calendar, falling
// back to the "default" calendar, then to (0, 0) — the caller treats a zero
// shape as "no usable calendar" and disables seasons (fail-soft).
func shapeFor(name string) (int, int) {
	if cfg, ok := gametime.GetCalendar(name); ok && len(cfg.Months) > 0 && cfg.DaysPerYear > 0 {
		return len(cfg.Months), cfg.DaysPerYear
	}
	if name != `default` {
		if cfg, ok := gametime.GetCalendar(`default`); ok && len(cfg.Months) > 0 && cfg.DaysPerYear > 0 {
			return len(cfg.Months), cfg.DaysPerYear
		}
	}
	return 0, 0
}

// CalendarShape reports the active calendar's (monthsPerYear, daysPerYear) —
// the shape seasons.Load validates track files against.
func CalendarShape() (monthsPerYear, daysPerYear int) {
	return shapeFor(gametime.GetDate().Calendar)
}

// CalendarNow is the current calendar position for season resolution.
// GameDate.Day is the day-of-year (1-based; the engine subtracts whole years).
func CalendarNow() seasons.CalendarPos {
	gd := gametime.GetDate()
	_, days := shapeFor(gd.Calendar)
	return seasons.CalendarPos{DayOfYear: gd.Day, DaysPerYear: days}
}
```

- [ ] **Step 4: Sync + run (checkout): `go test ./modules/weather/...`** — PASS.

- [ ] **Step 5: Commit**

```bash
git add engine/calendar.go engine/calendar_test.go
git commit -m "feat(engine): calendar shape and position helpers for seasons"
```

## Task 8: `SeasonsEnabled` config

**Files:** Modify `weather_config.go`, `weather_config_test.go`, `files/data-overlays/config.yaml`.

- [ ] **Step 1: Add failing test in `weather_config_test.go`**

```go
func TestSeasonsEnabledConfig(t *testing.T) {
	if cfg := buildConfig(func(string) any { return nil }); !cfg.SeasonsEnabled {
		t.Error("SeasonsEnabled must default true")
	}
	off := buildConfig(func(k string) any {
		return map[string]any{"SeasonsEnabled": false}[k]
	})
	if off.SeasonsEnabled {
		t.Error("SeasonsEnabled override ignored")
	}
}
```

- [ ] **Step 2: Sync + run (checkout): `go test ./modules/weather/ -run TestSeasonsEnabled`** — FAIL.

- [ ] **Step 3: Implement.** In `weather_config.go`: add `SeasonsEnabled bool // master switch for the seasons layer` to `Config` (after `Persist`), and `SeasonsEnabled: boolOr(get("SeasonsEnabled"), true),` to the `buildConfig` literal. In `files/data-overlays/config.yaml` append:

```yaml
SeasonsEnabled: true       # seasonal modulation of weather odds (v2); false = v1 behavior
```

- [ ] **Step 4: Sync + run (checkout): `go test ./modules/weather/`** — PASS.

- [ ] **Step 5: Commit**

```bash
git add weather_config.go weather_config_test.go files/data-overlays/config.yaml
git commit -m "feat(config): SeasonsEnabled switch (default on)"
```

## Task 9: Tick integration — tracks, effective climate, season events

**Files:** Create `weather_events.go`. Modify `weather.go` (struct), `weather_tick.go`.

- [ ] **Step 1: Create `weather_events.go`**

```go
package weather

// WeatherSeasonChanged is queued on the engine event bus when a zone's
// resolved season flips (never on the baseline-establishing first resolution
// after boot). Other modules listen by importing this type:
//
//	events.RegisterListener(weather.WeatherSeasonChanged{}, handler)
type WeatherSeasonChanged struct {
	Zone  string
	Track string
	From  string
	To    string
}

// Type implements events.Event.
func (WeatherSeasonChanged) Type() string { return `WeatherSeasonChanged` }
```

- [ ] **Step 2: Extend the `weatherModule` struct in `weather.go`.** After the `nextEmote` field, add:

```go
	tracks      seasons.Tracks                     // loaded season tracks (nil/empty = seasons off)
	seasonsOn   bool                               // SeasonsEnabled && tracks loaded && calendar usable
	zoneSeasons map[sim.ZoneId]seasons.ZoneSeason  // previous tick's resolution (event diffing)
```

Add `"github.com/GoMudEngine/GoMud/modules/weather/seasons"` to weather.go's imports.

- [ ] **Step 3: Wire `startSim` in `weather_tick.go`.** After the `loadContent()` call (and before the Buffs block), insert:

```go
	m.loadSeasons()
```

and add the method (plus the seasons import to weather_tick.go):

```go
// loadSeasons loads season tracks and establishes the baseline per-zone
// season map. Fail-soft ladder (design spec §7): disabled by config, no
// usable calendar, no/invalid track files => m.seasonsOn stays false and
// weather runs exactly as v1.
func (m *weatherModule) loadSeasons() {
	m.seasonsOn = false
	if !m.cfg.SeasonsEnabled {
		return
	}
	months, days := engine.CalendarShape()
	if months < 1 || days < 1 {
		mudlog.Warn("Weather: no usable calendar; seasons disabled")
		return
	}
	tracks, errs := seasons.Load(files, "files/datafiles/seasons", months, days)
	for _, err := range errs {
		mudlog.Warn("Weather: season track rejected", "error", err)
	}
	if len(tracks) == 0 {
		mudlog.Warn("Weather: no season tracks loaded; seasons disabled")
		return
	}
	m.tracks = tracks
	m.seasonsOn = true
	// Baseline resolution: establishes zoneSeasons WITHOUT emitting events,
	// so reboots never replay a flood of season changes.
	m.zoneSeasons = seasons.ZoneSeasons(m.graph, m.climate, m.tracks, engine.CalendarNow())
	mudlog.Info("Weather: seasons active", "tracks", len(tracks),
		"seasonalZones", len(m.zoneSeasons))
}
```

- [ ] **Step 4: Make the tick seasonal.** In `weather_tick.go`, replace the body of `tick` with:

```go
func (m *weatherModule) tick(round uint64) {
	climate := m.climate
	if m.seasonsOn {
		climate = seasons.EffectiveClimate(m.climate, m.tracks, engine.CalendarNow())
	}
	next, diff := sim.Step(m.state, m.graph, climate, m.simCfg, sim.Clock{Round: round})
	m.state = next
	_ = diff // per-zone changes are implied by the reconcile below
	engine.Reconcile(m.state.Weather)
	if m.seasonsOn {
		m.resolveSeasons()
	}
	m.persistState()
	m.nextTick = engine.NextTickRound(engine.TickPeriod(m.cfg.TickEveryGameHours))
}

// resolveSeasons re-resolves every zone's season and queues a
// WeatherSeasonChanged event for each flip since the previous tick.
func (m *weatherModule) resolveSeasons() {
	zs := seasons.ZoneSeasons(m.graph, m.climate, m.tracks, engine.CalendarNow())
	for zone, cur := range zs {
		if prev, ok := m.zoneSeasons[zone]; ok && prev.Season != cur.Season {
			events.AddToQueue(WeatherSeasonChanged{
				Zone: zone, Track: cur.Track, From: prev.Season, To: cur.Season,
			})
		}
	}
	m.zoneSeasons = zs
}
```

Add `"github.com/GoMudEngine/GoMud/internal/events"` to weather_tick.go's imports.

- [ ] **Step 5: Sync + verify (checkout): `go build ./modules/weather/... ; go test ./modules/weather/... ; go vet ./modules/weather/...`** — all green. Standalone `go test ./sim/... ./content/... ./seasons/...` green; `gofmt -l .` clean.

- [ ] **Step 6: Commit**

```bash
git add weather_events.go weather.go weather_tick.go
git commit -m "feat(weather): seasonal effective climate in the tick + WeatherSeasonChanged events"
```

## Task 10: `GetSeason` export and command surface

**Files:** Modify `weather_api.go`, `weather_commands.go`.

- [ ] **Step 1: Add the export to `weather_api.go`.** In `registerExports`, add:

```go
	m.plug.ExportFunction(`GetSeason`, m.exportGetSeason)
```

and the function:

```go
// exportGetSeason reports a zone's current season: {"track": string,
// "season": string, "blend": float64}. Empty strings when seasons are off,
// the zone is unknown, or its biome is unbound.
func (m *weatherModule) exportGetSeason(zone string) map[string]any {
	out := map[string]any{"track": "", "season": "", "blend": 0.0}
	if !m.simReady || !m.seasonsOn {
		return out
	}
	canonical, ok := m.graph.FindZone(zone)
	if !ok {
		return out
	}
	zs, ok := m.zoneSeasons[canonical]
	if !ok {
		return out
	}
	out["track"] = zs.Track
	out["season"] = zs.Season
	out["blend"] = zs.Blend
	return out
}
```

- [ ] **Step 2: Command surface in `weather_commands.go`.**

In `printLocalWeather`, after the weather line (`sendLine(user, fmt.Sprintf("The weather in %s is %s.", ...))`), add:

```go
	if m.seasonsOn {
		if zs, ok := m.zoneSeasons[room.Zone]; ok {
			sendLine(user, fmt.Sprintf("  The season here is %s.", zs.Season))
		}
	}
```

In `printStatus`, after the Emotes line, add:

```go
	if m.seasonsOn {
		sendLine(user, fmt.Sprintf("Seasons: %d track(s) loaded; %d zone(s) seasonal.",
			len(m.tracks), len(m.zoneSeasons)))
	} else {
		sendLine(user, "Seasons: off.")
	}
```

Add a `seasons` case to `cmdWeather`'s switch (before `default:`), update `adminUsage` to include ` | seasons`, and add:

```go
	case "seasons":
		m.printSeasons(user)
```

```go
// printSeasons lists each loaded track and where it stands right now.
func (m *weatherModule) printSeasons(user *users.UserRecord) {
	if !m.seasonsOn {
		sendLine(user, "Seasons: off (disabled, no tracks, or no usable calendar).")
		return
	}
	pos := engine.CalendarNow()
	sendLine(user, fmt.Sprintf("Season tracks (day %d of %d):", pos.DayOfYear, pos.DaysPerYear))
	for _, name := range sortedTrackNames(m.tracks) {
		tr := m.tracks[name]
		cur, prev, blend := tr.Resolve(pos.DayOfYear)
		line := fmt.Sprintf("  %-12s %s", name, cur)
		if blend < 1.0 {
			line += fmt.Sprintf(" (blending from %s, %.0f%%)", prev, blend*100)
		}
		sendLine(user, line)
	}
}

// sortedTrackNames returns track names sorted for stable output.
func sortedTrackNames(ts seasons.Tracks) []string {
	names := make([]string, 0, len(ts))
	for n := range ts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
```

Add `"github.com/GoMudEngine/GoMud/modules/weather/seasons"` to weather_commands.go's imports (`sort` is already imported).

- [ ] **Step 3: Sync + verify (checkout): build, `go test ./modules/weather/...`, vet** — green; `gofmt -l .` clean.

- [ ] **Step 4: Commit**

```bash
git add weather_api.go weather_commands.go
git commit -m "feat(weather): GetSeason export and season command surface"
```

## Task 11: Documentation

**Files:** Create `seasons/context.md`. Modify `sim/context.md`, `content/context.md`, `engine/context.md`, root `context.md`, `README.md`.

- [ ] **Step 1: Create `seasons/context.md`** following the house structure (Overview / Key Components / Core Functions / Dependencies / Consumers / Testing). It must cover: the pure-transform architecture (sim untouched — the regression guarantee); track file schema and validation rules (full month coverage, contiguity modulo the year, ranges, duplicate handling); the blend contract (window at season start, `blend = clamp(daysInto/transitionDays, 0, 1)`, continuous across the boundary, reported season flips at day 0); `monthOfDay` mirroring the engine formula and why; `EffectiveClimate` semantics (lerp of multipliers with missing = 1.0, influence added then resistance clamped, unbound pass-through, deep-copy guarantee); `ZoneSeasons` (absent = unbound); no persistence by design; yaml.v2 dependency note (same as content).

- [ ] **Step 2: Update the others** to match the code as it now exists:
  - `sim/context.md`: `ClimateProfile.Track` (inert data; which defaults bind to temperate).
  - `content/context.md`: `track:` passthrough in `ParseClimate`.
  - `engine/context.md`: `calendar.go` (`CalendarShape`/`CalendarNow`, default-calendar fallback, zero shape = seasons off).
  - Root `context.md`: `weather_events.go` (exported `WeatherSeasonChanged`, listened to by importing the type), `loadSeasons`/`resolveSeasons` in the tick lifecycle, `seasonsOn` gate, `GetSeason` export, `weather seasons` subcommand, `SeasonsEnabled` config.
  - `README.md`: add `seasons/` to the layout block; add `SeasonsEnabled` to the config table; one short "Seasons (v2, S1)" subsection under "How it works" stating what S1 does (odds shift with the calendar; tracks are YAML; S2/S3 bring mutators and prose), linking the seasons design spec, and including the esoteric-season recipe in three lines: custom track YAML with `weatherWeightAdditions`/`baseWeightScale` (show the `shattering` example from spec §3.1a) + the existing new-weather-type recipe (mutator spec + emote table) — emphasize: a glass-rain season is pure YAML, no Go.

- [ ] **Step 3: Verify docs-only:** `git status` shows only the six doc files; standalone tests still green.

- [ ] **Step 4: Commit**

```bash
git add seasons/context.md sim/context.md content/context.md engine/context.md context.md README.md
git commit -m "docs: document the seasons core across context files and README"
```

## Task 12: Full verification + smoke test

- [ ] **Step 1: Clean run of everything.**

```powershell
# Standalone:
go test ./sim/... ./crawler/... ./content/... ./seasons/...
go vet ./sim/... ./crawler/... ./content/... ./seasons/...
gofmt -l .   # expect empty
# Checkout:
pwsh scripts/sync-to-checkout.ps1 -Checkout "$HOME\workspace\GoMud"
Push-Location "$HOME\workspace\GoMud"
go generate ./... ; go build ./... ; go vet ./modules/weather/... ; go test ./modules/weather/...
Pop-Location
```
All green.

- [ ] **Step 2: Boot smoke test** (server from the checkout; admin login `admin`/`Password123`):
1. Boot log shows `Weather: seasons active tracks=2 seasonalZones=N` (N > 0 on the stock world) and no season warnings.
2. `weather` in a temperate-biome zone shows "The season here is <name>." matching what `time`'s month implies for the temperate track.
3. `weather seasons` lists `temperate` and `monsoon` with the current season (and blend % when inside a window).
4. `weather status` shows the Seasons line.
5. Determinism spot-check: `weather zones` twice a few ticks apart — weather still evolves normally (seasonal odds, no errors).
6. Set `SeasonsEnabled: false` in the module overlay, rebuild, reboot: log says seasons disabled, `weather seasons` reports off, weather runs as v1.
7. Restore `SeasonsEnabled: true`, rebuild; clean `/shutdown`; verify reboot re-baselines without an event flood (no errors; `weather seasons` consistent).
Record outcomes; report deviations honestly.

- [ ] **Step 3: Update the seasons design spec status.** Append under §8's S1 bullet in `docs/superpowers/specs/2026-06-10-seasons-design.md`: a dated blockquote noting S1 is implemented and smoke-verified (tracks loaded, odds shift, events/export/command live; S2 next).

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-06-10-seasons-design.md
git commit -m "docs(spec): record seasons S1 status"
```
