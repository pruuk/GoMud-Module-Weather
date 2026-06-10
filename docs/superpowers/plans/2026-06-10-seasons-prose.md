# Seasons S3 — Seasonal Prose & Content Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **STATUS: AWAITING CALABE'S APPROVAL — do not execute until he signs off (SOP gate).**

**Goal:** Give seasons a *voice*: seasonal ambient emote tables, optional per-season variants on the weather emote tables (freezing winter rain, monsoon downpours), the `jungle`/monsoon default content the spec promised — and the **thorough top-to-bottom README rework Calabe mandated** so the public face of the module covers weather + seasons as one coherent product.

**Architecture:** Pure extension of the existing emote pipeline. `content` gains a season dimension: weather `Table`s get an optional `seasonal:` block (lookup falls through to base lines — sparse by design, spec §6), and a new `SeasonalTables` type carries standalone seasonal ambience keyed by (track, season). `engine.EmitAmbient` becomes the single arbiter: weather emote first (season-variant aware), seasonal ambience only in calm zones at a lower cadence — one line per room per pass, weather wins (spec S-R1).

**Tech Stack:** Go 1.24; same split as always (pure packages standalone, engine-coupled in the checkout via the sync script).

**Spec:** `docs/superpowers/specs/2026-06-10-seasons-design.md` §6, §8 (S3 row), §9, S-R1.

---

## Decisions baked into this plan (veto at review)

1. **Lookup order for weather emotes with a season** (spec §6): `seasonal[season].{biome → default}` → `base.{biome → default}`. The season key matches by NAME across tracks (`winter` means temperate-winter; a monsoon world's tables use `wet`/`dry`) — collisions between tracks sharing a season name are intentional sharing.
2. **Seasonal ambience cadence:** the seasonal layer fires only when the zone's weather is calm (clear/unset) AND a 1-in-3 roll passes on that emote pass — roughly one seasonal line per occupied room per minute on stock timing, strictly quieter than weather. Constant `seasonalEmoteOneIn = 3` in `engine/emotes.go`; promote to config only if the smoke test says the default feels wrong (S-R1 verdict recorded in Task 8).
3. **Seasonal ambience files live in `files/datafiles/emotes/seasons/`** named `<track>_<season>.yaml` — a subdirectory so the existing weather-table loader (non-recursive) never sees them; the filename rule here is OURS (content loader), not the engine's.
4. **Shipped default content stays sparse** (spec §6): four high-value weather variants (rain×winter, rain×wet, storm×winter, heatwave×dry), six seasonal ambience tables (temperate ×4, monsoon ×2, each with outdoor+indoor defaults; winter carries a `forest` variant as the worked example), and the `jungle` biome bound to monsoon. Everything else is builder territory.
5. **EmitAmbient's signature changes** (gains zoneSeasons + seasonal tables); engine + root call site land in ONE task/commit so every commit builds.

## File structure

| File | Responsibility |
|---|---|
| `content/emotes.go` (modify) | `TableSection`, `Table.Seasonal`, season-aware `Pick`; `SeasonalTables` + `LoadSeasonalEmotes` + its `Pick`. |
| `content/emotes_test.go`, `content/moduledata_test.go` (modify) | Season-variant lookup tests; shipped-data validation for variants + ambience files. |
| `sim/climate.go` (modify) | `jungle` biome profile bound to `monsoon`. |
| `files/datafiles/emotes/{rain,storm,heatwave}.yaml` (modify) | `seasonal:` variant blocks. |
| `files/datafiles/emotes/seasons/*.yaml` (new ×6) | Seasonal ambience tables. |
| `engine/emotes.go` (modify) + `weather_tick.go`/`weather.go` (modify) | Emote-pass arbiter rework + root wiring (one commit). |
| `README.md` (REWORK), context.md ×3, `CONTRIBUTING.md` (touch) | The mandated docs pass. |

**Test commands** as before. Commits conventional, ending `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`; no pushes (controller pushes after the merge gate).

---

## Task 1: Season-aware weather emote lookup (`content`)

**Files:** Modify `content/emotes.go`, `content/emotes_test.go`.

- [ ] **Step 1: Write failing tests** (add to `content/emotes_test.go`; extend `stormYAML` first — replace the existing constant with this version, which only ADDS the `seasonal:` block):

```go
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
seasonal:
  winter:
    outdoor:
      default:
        - "Sleet-laden thunder shakes loose flurries of ice."
    indoor:
      default:
        - "Frozen rain rattles the shutters like thrown gravel."
`
```

New tests:

```go
func TestPickSeasonalVariant(t *testing.T) {
	tables := loadTestTables(t)
	first := func(n int) int { return 0 }

	// Season with a variant: seasonal section wins.
	if got := tables.Pick("storm", "default", false, "winter", first); got != "Sleet-laden thunder shakes loose flurries of ice." {
		t.Errorf("winter outdoor variant: %q", got)
	}
	if got := tables.Pick("storm", "default", true, "winter", first); got != "Frozen rain rattles the shutters like thrown gravel." {
		t.Errorf("winter indoor variant: %q", got)
	}
	// Variant section misses the biome -> variant default (not base biome).
	if got := tables.Pick("storm", "forest", false, "winter", first); got != "Sleet-laden thunder shakes loose flurries of ice." {
		t.Errorf("variant default should win over base biome: %q", got)
	}
	// Season without a variant: base lines.
	if got := tables.Pick("storm", "forest", false, "summer", first); got != "Wind tears at the branches; the whole canopy roars." {
		t.Errorf("no-variant season falls to base: %q", got)
	}
	// No season (seasons off / unbound zone): base lines.
	if got := tables.Pick("storm", "default", false, "", first); got != "Thunder cracks directly overhead." {
		t.Errorf("empty season falls to base: %q", got)
	}
	// Variant exists but its sections are empty for indoor+biome: falls to base.
	if got := tables.Pick("storm", "default", true, "summer", first); got != "Rain hammers against the windows." {
		t.Errorf("missing variant indoor falls to base indoor: %q", got)
	}
}
```

Also update every existing `Pick(...)` call in this test file to the new five-argument form (insert `""` as the season argument before `roll`) — `TestPickSelectsByBiomeAndIndoor`, `TestPickUsesRoll`, `TestPickClampsOutOfRangeRoll`.

- [ ] **Step 2: Run `go test ./content/`** — FAIL (signature).

- [ ] **Step 3: Implement.** In `content/emotes.go`:

```go
// TableSection is one outdoor/indoor pair of biome-keyed line lists.
type TableSection struct {
	Outdoor map[string][]string `yaml:"outdoor"`
	Indoor  map[string][]string `yaml:"indoor"`
}
```

Add to `Table`:

```go
	// Seasonal holds optional per-season variants, keyed by season NAME
	// (matching across tracks by design — "winter" is temperate's winter).
	// Missing seasons/sections fall through to the base lines (spec §6).
	Seasonal map[string]TableSection `yaml:"seasonal"`
```

Replace `Pick`:

```go
// Pick selects one ambient line for (weather, biome, indoor, season), or ""
// when nothing matches. Lookup order: the season's variant section (biome ->
// "default") when a variant exists, then the base section (biome ->
// "default"). season "" skips the variant layer (seasons off / unbound zone).
// Indoor never falls back to outdoor — silence beats wrong prose. roll(n)
// must return [0,n); pass the engine's util.Rand — NEVER the sim RNG. An
// out-of-range roll result is clamped to the first line.
func (ts Tables) Pick(weather sim.WeatherType, biome string, indoor bool, season string, roll func(int) int) string {
	t, ok := ts[weather]
	if !ok {
		return ""
	}
	var lines []string
	if season != "" {
		if v, ok := t.Seasonal[season]; ok {
			lines = sectionLines(v.Outdoor, v.Indoor, biome, indoor)
		}
	}
	if len(lines) == 0 {
		lines = sectionLines(t.Outdoor, t.Indoor, biome, indoor)
	}
	if len(lines) == 0 {
		return ""
	}
	i := roll(len(lines))
	if i < 0 || i >= len(lines) {
		i = 0
	}
	return lines[i]
}

// sectionLines resolves biome -> "default" within one outdoor/indoor pair.
func sectionLines(outdoor, indoor map[string][]string, biome string, useIndoor bool) []string {
	section := outdoor
	if useIndoor {
		section = indoor
	}
	lines := section[biome]
	if len(lines) == 0 {
		lines = section["default"]
	}
	return lines
}
```

- [ ] **Step 4: Run `go test ./content/`** — PASS. (The engine package now fails to compile against the new signature — expected until Task 6; standalone suites don't build it.)

- [ ] **Step 5: Commit** `git add content/emotes.go content/emotes_test.go` — `feat(content): optional per-season variants on weather emote tables`

## Task 2: Seasonal ambience tables (`content`)

**Files:** Modify `content/emotes.go`, `content/emotes_test.go`.

- [ ] **Step 1: Write failing tests:**

```go
const winterAmbienceYAML = `track: temperate
season: winter
outdoor:
  default:
    - "A skin of ice creaks at the edges of still water."
  forest:
    - "Snow slides from a burdened bough with a soft thump."
indoor:
  default:
    - "Cold radiates from the walls despite the shelter."
`

func TestLoadSeasonalEmotes(t *testing.T) {
	fsys := fstest.MapFS{"emotes/seasons/temperate_winter.yaml": {Data: []byte(winterAmbienceYAML)}}
	st, err := LoadSeasonalEmotes(fsys, "emotes/seasons")
	if err != nil {
		t.Fatal(err)
	}
	first := func(n int) int { return 0 }
	if got := st.Pick("temperate", "winter", "forest", false, first); got != "Snow slides from a burdened bough with a soft thump." {
		t.Errorf("forest outdoor: %q", got)
	}
	if got := st.Pick("temperate", "winter", "desert", false, first); got != "A skin of ice creaks at the edges of still water." {
		t.Errorf("biome fallback: %q", got)
	}
	if got := st.Pick("temperate", "winter", "default", true, first); got != "Cold radiates from the walls despite the shelter." {
		t.Errorf("indoor: %q", got)
	}
	if got := st.Pick("temperate", "summer", "default", false, first); got != "" {
		t.Errorf("missing season must be silent: %q", got)
	}
	if got := st.Pick("monsoon", "winter", "default", false, first); got != "" {
		t.Errorf("missing track must be silent: %q", got)
	}
}

func TestLoadSeasonalEmotesValidation(t *testing.T) {
	bad := fstest.MapFS{"emotes/seasons/x.yaml": {Data: []byte("season: winter\noutdoor:\n  default: [\"x\"]\n")}}
	if _, err := LoadSeasonalEmotes(bad, "emotes/seasons"); err == nil {
		t.Fatal("missing track must be rejected")
	}
	empty, err := LoadSeasonalEmotes(fstest.MapFS{}, "emotes/seasons")
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing dir: want empty/nil, got %v %v", empty, err)
	}
}
```

- [ ] **Step 2: Run** — FAIL (undefined: LoadSeasonalEmotes).

- [ ] **Step 3: Implement** in `content/emotes.go`:

```go
// SeasonalKey identifies one (track, season) ambience table.
type SeasonalKey struct{ Track, Season string }

// SeasonalTables holds the standalone seasonal-ambience emote tables — the
// persistent voice of a season in CALM weather (the weather tables' seasonal
// variants cover weathered moments). Loaded from emotes/seasons/.
type SeasonalTables map[SeasonalKey]TableSection

// seasonalEmoteFile mirrors the on-disk schema.
type seasonalEmoteFile struct {
	Track   string              `yaml:"track"`
	Season  string              `yaml:"season"`
	Outdoor map[string][]string `yaml:"outdoor"`
	Indoor  map[string][]string `yaml:"indoor"`
}

// LoadSeasonalEmotes loads every *.yaml under dir, keyed by (track, season).
// Missing dir = empty tables; the first malformed file aborts with an error
// (caller fails soft). Requires both 'track' and 'season' keys.
func LoadSeasonalEmotes(fsys fs.FS, dir string) (SeasonalTables, error) {
	out := SeasonalTables{}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return out, nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return out, fmt.Errorf("%s: %w", e.Name(), err)
		}
		var f seasonalEmoteFile
		if err := yaml.Unmarshal(b, &f); err != nil {
			return out, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if f.Track == "" || f.Season == "" {
			return out, fmt.Errorf("%s: missing required 'track' or 'season' key", e.Name())
		}
		out[SeasonalKey{f.Track, f.Season}] = TableSection{Outdoor: f.Outdoor, Indoor: f.Indoor}
	}
	return out, nil
}

// Pick selects one seasonal-ambience line for the zone's exact (track,
// season); "" when no table or no matching lines. Same biome/indoor fallback
// and roll contract as the weather tables.
func (st SeasonalTables) Pick(track, season, biome string, indoor bool, roll func(int) int) string {
	sec, ok := st[SeasonalKey{track, season}]
	if !ok {
		return ""
	}
	lines := sectionLines(sec.Outdoor, sec.Indoor, biome, indoor)
	if len(lines) == 0 {
		return ""
	}
	i := roll(len(lines))
	if i < 0 || i >= len(lines) {
		i = 0
	}
	return lines[i]
}
```

- [ ] **Step 4: Run `go test ./content/`** — PASS.

- [ ] **Step 5: Commit** — `feat(content): standalone seasonal-ambience emote tables`

## Task 3: `jungle` biome bound to monsoon (`sim`)

**Files:** Modify `sim/climate.go`, `sim/climate_test.go`.

- [ ] **Step 1: Failing test:**

```go
func TestDefaultClimateJungleMonsoon(t *testing.T) {
	p, ok := DefaultClimate()["jungle"]
	if !ok {
		t.Fatal("jungle profile missing")
	}
	if p.Track != "monsoon" {
		t.Errorf("jungle should bind to monsoon, got %q", p.Track)
	}
	if p.Weather["rain"] == 0 || p.Weather["storm"] == 0 {
		t.Errorf("jungle should weight rain/storm: %+v", p.Weather)
	}
}
```

- [ ] **Step 2: Run** — FAIL.

- [ ] **Step 3: Implement** (add to `DefaultClimate()`; the spec §3.2 promised this with the content milestone — not a stock-world id, but the default for any world that uses it):

```go
		"jungle": { // dense tropical canopy — monsoon-cycled (spec §3.2)
			Weather:     map[WeatherType]float64{"rain": 5, "storm": 3, "fog": 4, "overcast": 3, "clear": 2},
			Influence:   WeatherInfluence{IntensityDelta: 0.03, MoistureDelta: 0.06, MovementResistance: 0.25},
			SpawnWeight: 1.3,
			Track:       "monsoon",
		},
```

- [ ] **Step 4: Run `go test ./sim/...`** — PASS (golden trace untouched).

- [ ] **Step 5: Commit** — `feat(sim): jungle biome profile on the monsoon track`

## Task 4: Seasonal weather-variant data

**Files:** Modify `files/datafiles/emotes/rain.yaml`, `storm.yaml`, `heatwave.yaml`; modify `content/moduledata_test.go`.

- [ ] **Step 1: Extend the shipped-emotes validation** (in `TestShippedEmoteTables`, after the indoor-default check):

```go
		// Seasonal variant keys must be seasons of a shipped track, and every
		// variant needs at least one line somewhere.
		shippedSeasons := map[string]bool{"winter": true, "spring": true, "summer": true,
			"autumn": true, "wet": true, "dry": true}
		for season, sec := range table.Seasonal {
			if !shippedSeasons[season] {
				t.Errorf("%s: seasonal variant %q is not a season of any shipped track", e.Name(), season)
			}
			if len(sec.Outdoor) == 0 && len(sec.Indoor) == 0 {
				t.Errorf("%s: seasonal variant %q is empty", e.Name(), season)
			}
		}
```

- [ ] **Step 2: Add the variant blocks.** Append to `files/datafiles/emotes/rain.yaml`:

```yaml
seasonal:
  winter:
    outdoor:
      default:
        - "Freezing rain rattles off every surface like thrown gravel."
        - "Each drop stings with cold; the puddles film over with slush."
    indoor:
      default:
        - "Icy rain crackles against the roof in waves."
  wet:
    outdoor:
      default:
        - "The downpour is total, a warm wall of water without seams."
    indoor:
      default:
        - "The wet-season deluge drums on the roof without pause."
```

Append to `storm.yaml`:

```yaml
seasonal:
  winter:
    outdoor:
      default:
        - "Thunder rolls through driving sleet; lightning glares off the ice."
    indoor:
      default:
        - "An ice-storm flogs the walls; the timbers pop with cold."
```

Append to `heatwave.yaml`:

```yaml
seasonal:
  dry:
    outdoor:
      default:
        - "The dry-season heat is merciless; the cracked earth radiates like a forge."
    indoor:
      default:
        - "Even inside, the dry heat sits on your chest like a weight."
```

- [ ] **Step 3: Run `go test ./content/`** — PASS.

- [ ] **Step 4: Commit** — `feat(data): seasonal variants for rain, storm and heatwave emotes`

## Task 5: Seasonal ambience data (×6)

**Files:** Create `files/datafiles/emotes/seasons/temperate_winter.yaml`, `temperate_spring.yaml`, `temperate_summer.yaml`, `temperate_autumn.yaml`, `monsoon_wet.yaml`, `monsoon_dry.yaml`. Modify `content/moduledata_test.go`.

- [ ] **Step 1: Add the validation test:**

```go
// TestShippedSeasonalAmbience validates the seasonal ambience tables: they
// load, cover exactly the shipped tracks' seasons, use <track>_<season>.yaml
// filenames, and each has outdoor+indoor default lines.
func TestShippedSeasonalAmbience(t *testing.T) {
	st, err := LoadSeasonalEmotes(os.DirFS("../files/datafiles"), "emotes/seasons")
	if err != nil {
		t.Fatalf("seasonal ambience failed to load: %v", err)
	}
	want := []SeasonalKey{
		{"temperate", "winter"}, {"temperate", "spring"},
		{"temperate", "summer"}, {"temperate", "autumn"},
		{"monsoon", "wet"}, {"monsoon", "dry"},
	}
	if len(st) != len(want) {
		t.Errorf("expected %d ambience tables, got %d", len(want), len(st))
	}
	for _, k := range want {
		sec, ok := st[k]
		if !ok {
			t.Errorf("missing ambience table for %v", k)
			continue
		}
		if len(sec.Outdoor["default"]) == 0 || len(sec.Indoor["default"]) == 0 {
			t.Errorf("%v: needs outdoor and indoor default lines", k)
		}
	}
	entries, _ := os.ReadDir("../files/datafiles/emotes/seasons")
	for _, e := range entries {
		// Filename rule (ours, not the engine's): <track>_<season>.yaml.
		var f struct{ Track, Season string }
		b, _ := os.ReadFile("../files/datafiles/emotes/seasons/" + e.Name())
		_ = yaml.Unmarshal(b, &f)
		if wantName := f.Track + "_" + f.Season + ".yaml"; e.Name() != wantName {
			t.Errorf("%s: filename should be %s", e.Name(), wantName)
		}
	}
}
```

- [ ] **Step 2: Run** — FAIL (missing files).

- [ ] **Step 3: Create the six files.**

`temperate_winter.yaml`:
```yaml
track: temperate
season: winter
outdoor:
  default:
    - "A skin of ice creaks at the edges of still water."
    - "Your breath plumes white and hangs in the frozen air."
  forest:
    - "Snow slides from a burdened bough with a soft thump."
indoor:
  default:
    - "Deep-winter cold seeps through the walls despite the shelter."
```

`temperate_spring.yaml`:
```yaml
track: temperate
season: spring
outdoor:
  default:
    - "Meltwater chuckles somewhere out of sight."
    - "New green pushes up through last year's mat of leaves."
indoor:
  default:
    - "The smell of wet, waking earth drifts in from outside."
```

`temperate_summer.yaml`:
```yaml
track: temperate
season: summer
outdoor:
  default:
    - "Insects drone in the heavy summer air."
    - "Heat shimmer wobbles above every sunlit surface."
indoor:
  default:
    - "The summer air inside is close and still."
```

`temperate_autumn.yaml`:
```yaml
track: temperate
season: autumn
outdoor:
  default:
    - "Dry leaves scrape past on a chill gust."
    - "The light has gone thin and slanted with the turning year."
indoor:
  default:
    - "A draft carries the cold smell of leaf-rot and coming frost."
```

`monsoon_wet.yaml`:
```yaml
track: monsoon
season: wet
outdoor:
  default:
    - "Everything drips; the air itself feels half water."
    - "Steam rises wherever the sun briefly finds wet ground."
indoor:
  default:
    - "Mould-sweet damp has crept into every corner of the room."
```

`monsoon_dry.yaml`:
```yaml
track: monsoon
season: dry
outdoor:
  default:
    - "Dust devils twist across the cracked hardpan."
    - "The dry wind rasps like something thirsty."
indoor:
  default:
    - "Fine dry dust films every surface, fresh as fast as it's wiped."
```

- [ ] **Step 4: Run `go test ./content/`** — PASS.

- [ ] **Step 5: Commit** — `feat(data): seasonal ambience emote tables for temperate and monsoon`

## Task 6: Emote-pass arbiter — engine + root wiring (one commit)

**Files:** Modify `engine/emotes.go`, `weather.go` (struct field), `weather_tick.go` (loadContent + EmitAmbient call).

- [ ] **Step 1: Rework `engine/emotes.go`:**

```go
// seasonalEmoteOneIn throttles the seasonal-ambience layer: it fires on
// roughly 1 in N emote passes, only in calm zones — strictly quieter than
// weather (spec S-R1). Promote to config only if play feel demands.
const seasonalEmoteOneIn = 3

// EmitAmbient sends at most ONE ambient line per occupied room per pass:
// the weather line when the zone has non-calm weather (season-variant aware),
// else — at reduced cadence — the zone's seasonal ambience. zoneSeasons and
// seasonal may be nil/empty when seasons are off. roll is the presentation
// RNG (pass util.Rand) — NEVER the sim RNG. Returns lines sent.
func EmitAmbient(weather map[sim.ZoneId]sim.WeatherType, zoneSeasons map[sim.ZoneId]seasons.ZoneSeason,
	tables content.Tables, seasonal content.SeasonalTables, roll func(int) int) int {
	sent := 0
	for _, roomId := range rooms.GetRoomsWithPlayers() {
		room := rooms.LoadRoom(roomId)
		if room == nil {
			continue
		}
		biomeId := ""
		if b := room.GetBiome(); b != nil {
			biomeId = b.BiomeId
		}
		indoor := !isOutdoorBiome(biomeId)
		zs, hasSeason := zoneSeasons[room.Zone]

		if w := weather[room.Zone]; w != "" && w != sim.Clear {
			season := ""
			if hasSeason {
				season = zs.Season
			}
			if line := tables.Pick(w, biomeId, indoor, season, roll); line != "" {
				room.SendText(line)
				sent++
			}
			continue // weather wins; one line per room per pass
		}
		if hasSeason && roll(seasonalEmoteOneIn) == 0 {
			if line := seasonal.Pick(zs.Track, zs.Season, biomeId, indoor, roll); line != "" {
				room.SendText(line)
				sent++
			}
		}
	}
	return sent
}
```

- [ ] **Step 2: Root wiring.** In `weather.go`, add to the `weatherModule` struct (after `tables`): `seasonalTables content.SeasonalTables // seasonal-ambience emotes (track,season)-keyed`. In `weather_tick.go`:
  - `loadContent` gains, after the emote-tables block:
    ```go
	seasonalTables, err := content.LoadSeasonalEmotes(files, "files/datafiles/emotes/seasons")
	if err != nil {
		mudlog.Warn("Weather: seasonal emote tables failed to load", "error", err)
	}
	m.seasonalTables = seasonalTables
    ```
  - The `onNewRound` call site (in `weather.go`) becomes:
    ```go
	engine.EmitAmbient(m.state.Weather, m.zoneSeasons, m.tables, m.seasonalTables, util.Rand)
    ```
    (When seasons are off, `m.zoneSeasons` is nil — both season layers stay silent and weather emotes fall back to base lines via `season == ""`.)

- [ ] **Step 3: Sync + verify (checkout): `go build ./modules/weather/... ; go test ./modules/weather/... ; go vet ./modules/weather/...`** — green. Standalone suites green; `gofmt -l .` clean.

- [ ] **Step 4: Commit** `git add engine/emotes.go weather.go weather_tick.go` — `feat(weather): season-aware ambient emotes with a calm-zone seasonal layer`

## Task 7: The README rework (Calabe's standing instruction) + docs

**Files:** REWORK `README.md`; modify `content/context.md`, `engine/context.md`, root `context.md`; touch `CONTRIBUTING.md` if test commands changed (they didn't — verify).

- [ ] **Step 1: README — a genuine top-to-bottom rework, not a patch.** Same quality bar and audience as the M3 rewrite (newcomer to GoMud, possibly to Go), now presenting weather + seasons as ONE product. Required structure (write fresh; reuse good M3 prose where it survives):
  1. Intro + headline — now including the seasonal arc (a storm in deep winter vs the same coast in high summer).
  2. **What it is / What it is NOT** — seasons move from "not" to "is" (calendar-driven, per-biome tracks, esoteric YAML seasons); the NOT list updates (per-room granularity, GMCP, prevailing wind, temperature model still out; v2 remainder honest).
  3. Requirements (Go 1.24+ matching the engine; the **module-manager install is THE path** — keep Volte6's corrections intact).
  4. Installation + quick start — registry install, boot-log walkthrough updated for the seasons lines (`seasons active tracks=2 seasonalZones=8`).
  5. Using it in game — command tables incl. `weather seasons`; what players see across a season boundary.
  6. How it works — extend the one-paragraph pipeline with the seasons transform (effective climate; season mutator layer; emote arbiter: weather wins, seasonal ambience in calm zones).
  7. Configuration — full table (now incl. `SeasonsEnabled`).
  8. Customizing — climate/track binding, track files (incl. **the esoteric `shattering` example** — additions + baseWeightScale, "pure YAML, no Go"), mutator specs (both namespaces, the forbidden-fields rules), emote tables (base + `seasonal:` variants + ambience files + `tag-only`), seasonal exits (frozen-river example), buffs posture.
  9. API for other modules — `GetWeather`/`GetFronts`/`SpawnFront`/`GetSeason` + `WeatherSeasonChanged`.
  10. **What can break it** — keep all eight M3 entries, ADD the seasons-specific ones: non-12-month calendars vs shipped tracks (fail-soft, authoring note), claiming the `season-` namespace, season names in `seasonal:` blocks not matching any track, custom biome ids being season-unbound (and weather-bland) until a climate profile binds them.
  11. Development & testing; Roadmap (M4 polish + v2 leftovers + registry version bump); License.
- [ ] **Step 2: context.md updates** — `content/context.md` (TableSection/Seasonal/Pick order, SeasonalTables/LoadSeasonalEmotes, the seasons/ subdir + filename rule, validation coverage); `engine/context.md` (EmitAmbient arbiter semantics + `seasonalEmoteOneIn`); root `context.md` (seasonalTables field, loadContent, the emote call site). Verify `CONTRIBUTING.md` needs nothing (commands unchanged).
- [ ] **Step 3: Verify docs-only diff for this commit (plus the README); standalone suites green.**
- [ ] **Step 4: Commit** — `docs: rework README for the full weather+seasons product; context updates`

## Task 8: Verification + smoke + spec close-out

- [ ] **Step 1:** Full clean run (standalone suites + vet + gofmt; checkout generate/build/vet/test) — all green.
- [ ] **Step 2: Boot smoke** (checkout; admin/Password123; emote passes are ~80s apart on stock timing — budget patience, use generous waits):
  1. Boot: `loadedCount=24`, `seasons active tracks=2 seasonalZones=8`, no WARN/ERROR.
  2. In a seasonal zone with calm weather: wait through several emote passes; confirm a **seasonal ambience line** arrives (1-in-3 cadence ⇒ expect one within ~4–5 passes) and that it stops appearing once weather is spawned.
  3. `weather spawn rain <zone> 0.9` in a winter-temperate zone: next weather emote should be a **winter variant** line ("Freezing rain rattles…"). Record verbatim.
  4. Indoor room: variant/ambience indoor lines render (not outdoor prose).
  5. `SeasonsEnabled: false` rebuild: weather emotes revert to base lines; no seasonal ambience. Restore via sync, rebuild.
  6. Clean `/shutdown` + reboot: everything re-asserts; no event flood.
  7. **S-R1 verdict:** subjective check — with weather + seasonal layers live, does ambience feel spammy at stock cadence? Record the observation.
- [ ] **Step 3:** Spec updates: S-R1 row gets its dated verdict; §8 gains the S3 status blockquote (dated, honest — what shipped, the cadence observation, anything deviating). The seasons spec's status header (line 3: "Approved design, pre-implementation") updates to reflect S1–S3 implemented.
- [ ] **Step 4: Commit** — `docs(spec): record seasons S3 status; close S-R1`

---

**After this plan completes** (outside its scope, Calabe's call): seasons are feature-complete → natural point for a **v0.2.0 release + registry version bump** (new tarball asset, updated `plugins.New` version, registry PR updating version/url/sha256 — the M3 walkthrough applies verbatim), and for the M4 weather-polish milestone to get its own plan.
