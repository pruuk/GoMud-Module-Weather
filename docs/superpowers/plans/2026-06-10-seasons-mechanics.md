# Seasons S2 — Seasonal Mechanics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **STATUS: AWAITING CALABE'S APPROVAL — do not execute until he signs off (SOP gate).**

**Goal:** Make seasons *visible and mechanical* in the world: a `season-<track>-<season>` zone-mutator layer (persistent seasonal descriptions, with buff and seasonal-exit seams), reconciled exactly like weather mutators — plus stock-world biome coverage so it all actually shows up on a default install.

**Architecture:** S1 already resolves every zone's season each tick (`m.zoneSeasons`). S2 adds `engine.ReconcileSeasons` — the same reconcile pattern as weather, in an independent `season-` mutator namespace — driven by that map at boot, per tick, and after rebuilds. Six default mutator specs ship as plugin data files under the same loader rules M3 verified. No sim changes beyond `DefaultClimate()` data.

**Tech Stack:** Go 1.25; engine-coupled work tests in the GoMud checkout (`~/workspace/GoMud`) via `scripts/sync-to-checkout.ps1`.

**Spec:** `docs/superpowers/specs/2026-06-10-seasons-design.md` §5.1, §8 (S2 row), §9, S-R2.

---

## Two scope decisions for Calabe's review (decided in this plan; veto here)

1. **Task 1 is a scope ADDITION from the S1 smoke finding:** the stock world's biome ids (`mountains`, `shore`, `snow`, `cliffs`, `water`, `farmland`, `land`, `road`, `city`, `fort`, `slums`) mostly don't exist in `DefaultClimate()`, so only ONE stock zone is seasonal and most stock zones run the bland `default` weather profile. Task 1 adds profiles + temperate bindings for the outdoor stock ids. Without it, S2's mutator layer is nearly invisible on a default install (and un-smoke-testable). It also materially improves base weather variety on stock worlds — strictly an OOBE win, but it IS new default content originally penciled for M4.
2. **Default seasonal specs ship with NO buffs and NO exits** (both remain documented seams with examples). Spec §8 said "curated buffs," but a zone-wide buff that persists for an entire ~4-real-day season is not "gentle" (buff 31 deals damage per trigger; 33 is −20 all stats). Transient weather buffs stay as shipped in M3; season-long debuffs wait for `Buffs.Overrides` (M4) so worlds opt in deliberately. Also per S-R2, seasonal specs ship **without `namemodifier`** — room titles already carry weather tags; the smoke test (Task 8) renders both layers and records whether that call was right.

## Verified facts (carried from M3/S1 — no new upstream verification needed)

- Plugin-FS mutator loading: id `season-temperate-winter` ⇒ filename `season_temperate_winter.yaml`; duplicate ids rejected; specs must carry `decayrate`, never `respawnrate` (blocks purge) or `decayintoid` (Remove resurrects it — empirically proven in M3).
- `MutatorList.Add` appends duplicates when live (Has-guard required); `Remove`+`Update` purge cleanly without decayintoid. `MutatorSpec.Exits` exists for seasonal exits.
- Engine merges all zone mutators into room renders; two mutators per zone (weather + season) is the engine's normal merge path (spec S-R2 watches title noise only).
- Stock biome ids (read from `_datafiles/world/default/biomes/`): cave, city, cliffs, default, desert, dungeon, farmland, forest, fort, house, land, road, mountains, shore, slums, snow, spiderweb, swamp, water.

## File structure

| File | Responsibility |
|---|---|
| `sim/climate.go` (modify) | Stock-id climate profiles + temperate bindings. |
| `engine/apply.go` (modify) | Generalize `reconcileZone` to a want-id core; add `SeasonMutatorPrefix`, `SeasonMutatorId`, `ReconcileSeasons`; `StripBuffs` covers both namespaces. |
| `files/datafiles/mutators/season_*.yaml` (new ×6) | Default seasonal mutator specs (description-only). |
| `content/moduledata_test.go` (modify) | Validate both `weather-` and `season-` namespaces under the same rules. |
| `weather_tick.go`, `weather.go` (modify) | `ReconcileSeasons` at baseline, per tick, post-rebuild. |
| context.md ×4 + `README.md` + spec (modify) | Docs incl. the frozen-river exits example; S-R2 outcome. |

**Test commands** as S1: standalone `go test ./sim/... ./crawler/... ./content/... ./seasons/...`; engine-coupled via sync → checkout `go test ./modules/weather/...`. Commits conventional, ending `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`; no pushes.

---

## Task 1: Stock-world biome coverage in `DefaultClimate()`

**Files:** Modify `sim/climate.go`, `sim/climate_test.go`.

- [ ] **Step 1: Write the failing test** (extend `sim/climate_test.go`):

```go
func TestDefaultClimateCoversStockBiomes(t *testing.T) {
	c := DefaultClimate()
	// The stock GoMud world's outdoor biome ids (S1 smoke finding: these
	// previously fell through to the bland "default" profile).
	stock := []string{"mountains", "cliffs", "snow", "shore", "water",
		"farmland", "land", "road", "city", "fort", "slums"}
	for _, b := range stock {
		p, ok := c[b]
		if !ok {
			t.Errorf("stock biome %q has no climate profile", b)
			continue
		}
		if len(p.Weather) == 0 {
			t.Errorf("stock biome %q has empty weather table", b)
		}
		if p.Track != "temperate" {
			t.Errorf("stock biome %q should bind to temperate, got %q", b, p.Track)
		}
	}
	// Indoor-ish stock ids deliberately stay unbound/absent (default profile).
	for _, b := range []string{"cave", "dungeon", "house", "spiderweb"} {
		if _, ok := c[b]; ok {
			t.Errorf("indoor biome %q should not have a profile", b)
		}
	}
}
```

- [ ] **Step 2: Run `go test ./sim/ -run TestDefaultClimateCoversStockBiomes`** — FAIL.

- [ ] **Step 3: Implement.** Add these entries to `DefaultClimate()` (values reuse the existing archetypes — comment each group):

```go
		// --- Stock-world biome ids (the default GoMud world uses these; they
		// previously fell through to the "default" profile — S1 smoke finding).
		"mountains": { // = mountain archetype
			Weather:     map[WeatherType]float64{"overcast": 4, "snow": 4, "storm": 2, "fog": 3},
			Influence:   WeatherInfluence{IntensityDelta: -0.15, MoistureDelta: -0.10, MovementResistance: 0.5},
			SpawnWeight: 0.8,
			Track:       "temperate",
		},
		"cliffs": { // exposed high ground: mountain-lite, windier storms
			Weather:     map[WeatherType]float64{"clear": 3, "overcast": 4, "storm": 3, "fog": 3},
			Influence:   WeatherInfluence{IntensityDelta: -0.08, MoistureDelta: -0.04, MovementResistance: 0.3},
			SpawnWeight: 0.9,
			Track:       "temperate",
		},
		"snow": { // = tundra archetype
			Weather:     map[WeatherType]float64{"clear": 5, "overcast": 4, "snow": 6, "blizzard": 2, "fog": 2},
			Influence:   WeatherInfluence{IntensityDelta: -0.05, MoistureDelta: -0.02, MovementResistance: 0.2},
			SpawnWeight: 1.0,
			Track:       "temperate",
		},
		"shore": { // coastal: ocean-fed but calmer
			Weather:     map[WeatherType]float64{"clear": 4, "overcast": 4, "rain": 4, "storm": 3, "fog": 3},
			Influence:   WeatherInfluence{IntensityDelta: 0.04, MoistureDelta: 0.06, MovementResistance: 0.05},
			SpawnWeight: 1.3,
			Track:       "temperate",
		},
		"water": { // = ocean archetype
			Weather:     map[WeatherType]float64{"clear": 3, "overcast": 4, "rain": 4, "storm": 4, "fog": 2},
			Influence:   WeatherInfluence{IntensityDelta: 0.06, MoistureDelta: 0.08, MovementResistance: 0.02},
			SpawnWeight: 1.5,
			Track:       "temperate",
		},
		"farmland": { // = plains archetype
			Weather:     map[WeatherType]float64{"clear": 5, "overcast": 3, "rain": 3, "storm": 2},
			Influence:   WeatherInfluence{IntensityDelta: 0.02, MoistureDelta: 0, MovementResistance: 0.05},
			SpawnWeight: 1.2,
			Track:       "temperate",
		},
		"land": { // generic open ground = plains archetype
			Weather:     map[WeatherType]float64{"clear": 5, "overcast": 3, "rain": 3, "storm": 2},
			Influence:   WeatherInfluence{IntensityDelta: 0.02, MoistureDelta: 0, MovementResistance: 0.05},
			SpawnWeight: 1.2,
			Track:       "temperate",
		},
		"road": { // travelled open ground: plains-lite, low spawn pressure
			Weather:     map[WeatherType]float64{"clear": 6, "overcast": 3, "rain": 2, "fog": 2},
			Influence:   WeatherInfluence{IntensityDelta: 0, MoistureDelta: 0, MovementResistance: 0.05},
			SpawnWeight: 0.8,
			Track:       "temperate",
		},
		"city": { // urban: mild, fog-prone, storms dampened
			Weather:     map[WeatherType]float64{"clear": 5, "overcast": 4, "rain": 3, "fog": 3, "storm": 1},
			Influence:   WeatherInfluence{IntensityDelta: -0.03, MoistureDelta: -0.02, MovementResistance: 0.15},
			SpawnWeight: 0.9,
			Track:       "temperate",
		},
		"fort": { // = city archetype
			Weather:     map[WeatherType]float64{"clear": 5, "overcast": 4, "rain": 3, "fog": 3, "storm": 1},
			Influence:   WeatherInfluence{IntensityDelta: -0.03, MoistureDelta: -0.02, MovementResistance: 0.15},
			SpawnWeight: 0.9,
			Track:       "temperate",
		},
		"slums": { // = city archetype
			Weather:     map[WeatherType]float64{"clear": 5, "overcast": 4, "rain": 3, "fog": 3, "storm": 1},
			Influence:   WeatherInfluence{IntensityDelta: -0.03, MoistureDelta: -0.02, MovementResistance: 0.15},
			SpawnWeight: 0.9,
			Track:       "temperate",
		},
```

- [ ] **Step 4: Run `go test ./sim/...`** — PASS (golden trace untouched: it uses a synthetic graph whose biomes are unchanged; if it FAILS, STOP and report — do not regenerate).

- [ ] **Step 5: Commit** `git add sim/climate.go sim/climate_test.go` — `feat(sim): climate profiles for the stock world's biome ids`

## Task 2: Generalize the reconcile core to a want-id signature

**Files:** Modify `engine/apply.go`, `engine/apply_test.go`.

- [ ] **Step 1: Update the existing tests.** In `engine/apply_test.go`, change every `reconcileZone(f, current, target)` call to pass the mutator id directly: `reconcileZone(f, current, MutatorIdFor(target))` — e.g. `reconcileZone(f, []string{"weather-storm", "weather-fog"}, MutatorIdFor("rain"))` and the calm case `reconcileZone(f, []string{"weather-snow"}, MutatorIdFor(sim.Clear))`. Behavior expectations unchanged.

- [ ] **Step 2: Run (checkout, after sync): `go test ./modules/weather/engine/ -run TestReconcileZone`** — FAIL (signature mismatch).

- [ ] **Step 3: Implement.** In `engine/apply.go`, change `reconcileZone` to take the want id (prefix-agnostic core):

```go
// reconcileZone forces a zone's mutators WITHIN ONE NAMESPACE to exactly
// match want: every id in current except want is removed; want is added if
// absent ("" = remove all). current must hold only ids from the same
// namespace (the caller gathers by prefix).
func reconcileZone(ms mutatorSet, current []string, want string) bool {
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
```

Update `Reconcile` to call `reconcileZone(&zc.Mutators, current, MutatorIdFor(w))` (and keep its warn-once on failure).

- [ ] **Step 4: Run (checkout): `go test ./modules/weather/engine/`** — PASS.

- [ ] **Step 5: Commit** — `refactor(engine): prefix-agnostic reconcile core`

## Task 3: The season mutator layer — `ReconcileSeasons` + `StripBuffs` extension

**Files:** Modify `engine/apply.go`, `engine/apply_test.go`.

- [ ] **Step 1: Write failing tests** (add to `engine/apply_test.go`):

```go
func TestSeasonMutatorId(t *testing.T) {
	if got := SeasonMutatorId("temperate", "winter"); got != "season-temperate-winter" {
		t.Errorf("got %q", got)
	}
	if SeasonMutatorId("", "winter") != "" || SeasonMutatorId("temperate", "") != "" {
		t.Error("empty track or season must map to no mutator")
	}
}

func TestReconcileSeasonZone(t *testing.T) {
	// Same core as weather: stale season swapped for the current one,
	// weather-* ids untouched because the caller gathers season-* only.
	f := newFake("season-temperate-autumn")
	reconcileZone(f, []string{"season-temperate-autumn"}, SeasonMutatorId("temperate", "winter"))
	want := []string{"remove:season-temperate-autumn", "add:season-temperate-winter"}
	if !reflect.DeepEqual(f.ops, want) {
		t.Errorf("ops = %v, want %v", f.ops, want)
	}
}
```

- [ ] **Step 2: Run (checkout) — FAIL** (undefined: SeasonMutatorId).

- [ ] **Step 3: Implement** in `engine/apply.go`:

```go
// SeasonMutatorPrefix namespaces the seasonal-ambience mutators; independent
// of WeatherMutatorPrefix — the two reconcile layers never touch each other's
// ids.
const SeasonMutatorPrefix = "season-"

// SeasonMutatorId maps a zone's resolved (track, season) to its mutator id;
// "" when either part is empty (no seasonal mutator).
func SeasonMutatorId(track, season string) string {
	if track == "" || season == "" {
		return ""
	}
	return SeasonMutatorPrefix + track + "-" + season
}

// ReconcileSeasons forces every zone's season-* mutators to match its
// resolved season — used at boot, each tick, and after a graph rebuild.
// Zones absent from the map (unbound biomes) get their season-* mutators
// removed, so a zone whose biome lost its track binding heals.
func ReconcileSeasons(g *sim.Graph, zoneSeasons map[sim.ZoneId]seasons.ZoneSeason) {
	for _, zone := range g.Zones() {
		zc := rooms.GetZoneConfig(zone)
		if zc == nil {
			continue
		}
		var current []string
		for _, mut := range zc.Mutators.GetActive() {
			if strings.HasPrefix(mut.MutatorId, SeasonMutatorPrefix) {
				current = append(current, mut.MutatorId)
			}
		}
		want := ""
		if zs, ok := zoneSeasons[zone]; ok {
			want = SeasonMutatorId(zs.Track, zs.Season)
		}
		if len(current) == 0 && want == "" {
			continue
		}
		if !reconcileZone(&zc.Mutators, current, want) {
			warnUnknownSeasonMutator(want)
		}
	}
}

// warnedSeasonMutators: warn-once for missing season specs (single goroutine).
var warnedSeasonMutators = map[string]bool{}

func warnUnknownSeasonMutator(id string) {
	if id == "" || warnedSeasonMutators[id] {
		return
	}
	warnedSeasonMutators[id] = true
	mudlog.Warn("Weather: no mutator spec loaded for season", "mutatorId", id)
}
```

Add the `seasons` import. Extend `StripBuffs` so the BuffsEnabled toggle covers both namespaces — change its prefix check to:

```go
		if !strings.HasPrefix(id, WeatherMutatorPrefix) && !strings.HasPrefix(id, SeasonMutatorPrefix) {
			continue
		}
```

(and update its doc comment to say "weather-* and season-* specs").

- [ ] **Step 4: Run (checkout): `go test ./modules/weather/engine/`** — PASS. Also confirm the existing `TestReconcileZone` weather cases still pass (namespace isolation is by-construction: each caller gathers only its own prefix).

- [ ] **Step 5: Commit** — `feat(engine): season mutator namespace with ReconcileSeasons`

## Task 4: Default seasonal mutator specs (×6)

**Files:** Create `files/datafiles/mutators/season_temperate_winter.yaml`, `season_temperate_spring.yaml`, `season_temperate_summer.yaml`, `season_temperate_autumn.yaml`, `season_monsoon_wet.yaml`, `season_monsoon_dry.yaml`. (Validation-test changes land in Task 5.)

- [ ] **Step 1: Create the six files** — description-only (no namemodifier per S-R2; no buffs/exits per the scope decision above), `decayrate: 24 hours` safety net, no respawnrate, no decayintoid:

`season_temperate_winter.yaml`:
```yaml
# Seasonal ambience layer (S2): description-only by design — room titles
# already carry weather tags (spec S-R2). Buffs/exits are builder seams:
# add playerbuffids/exits in a world-specific override of this spec.
mutatorid: season-temperate-winter
descriptionmodifier:
  behavior: append
  text: Winter holds the land; frost rims every edge and breath hangs in the air.
  colorpattern: frost
decayrate: 24 hours
```

`season_temperate_spring.yaml`:
```yaml
mutatorid: season-temperate-spring
descriptionmodifier:
  behavior: append
  text: Spring is underway; green shoots and meltwater are everywhere.
  colorpattern: mute-green
decayrate: 24 hours
```

`season_temperate_summer.yaml`:
```yaml
mutatorid: season-temperate-summer
descriptionmodifier:
  behavior: append
  text: High summer lies heavy here, the air thick and warm.
  colorpattern: gold
decayrate: 24 hours
```

`season_temperate_autumn.yaml`:
```yaml
mutatorid: season-temperate-autumn
descriptionmodifier:
  behavior: append
  text: Autumn has turned the land to rust and amber; the air carries a chill edge.
  colorpattern: rust
decayrate: 24 hours
```

`season_monsoon_wet.yaml`:
```yaml
mutatorid: season-monsoon-wet
descriptionmodifier:
  behavior: append
  text: The wet season saturates everything; the ground squelches and the air drips.
  colorpattern: mute-dblue
decayrate: 24 hours
```

`season_monsoon_dry.yaml`:
```yaml
mutatorid: season-monsoon-dry
descriptionmodifier:
  behavior: append
  text: The dry season has baked the earth to cracked hardpan.
  colorpattern: brown
decayrate: 24 hours
```

(Color patterns `frost`, `mute-green`, `gold`, `rust`, `mute-dblue`, `brown` all verified to exist in the stock `color-patterns.yaml` during M3.)

- [ ] **Step 2: Commit** — `feat(data): default seasonal ambience mutator specs`

## Task 5: Shipped-data validation for both namespaces

**Files:** Modify `content/moduledata_test.go`.

- [ ] **Step 1: Update `TestShippedMutatorSpecs`.** Replace the namespace assertion (currently requires every id to be `weather-` prefixed) with:

```go
		id, _ := spec["mutatorid"].(string)
		if !strings.HasPrefix(id, "weather-") && !strings.HasPrefix(id, "season-") {
			t.Errorf("%s: mutatorid %q must be weather- or season- namespaced", e.Name(), id)
		}
```

Everything else (filename rule, `respawnrate` forbidden, `decayintoid` forbidden, `decayrate` required) applies to both namespaces unchanged. Add one count assertion so a sync/copy mistake is loud:

```go
	if len(entries) < 14 { // 8 weather + 6 season specs
		t.Errorf("expected at least 14 shipped mutator specs, found %d", len(entries))
	}
```

- [ ] **Step 2: Run `go test ./content/ -run TestShippedMutatorSpecs -v`** — PASS (14 files validated).

- [ ] **Step 3: Commit** — `test(content): validate season mutator specs under the shared namespace rules`

## Task 6: Wire `ReconcileSeasons` into the lifecycle

**Files:** Modify `weather_tick.go`, `weather.go`.

- [ ] **Step 1: `weather_tick.go`** — three call sites:
  - In `loadSeasons`, after the baseline `m.zoneSeasons = seasons.ZoneSeasons(...)` line, add:
    ```go
	engine.ReconcileSeasons(m.graph, m.zoneSeasons) // assert season mutators at boot
    ```
  - In `resolveSeasons`, after `m.zoneSeasons = zs`, add:
    ```go
	engine.ReconcileSeasons(m.graph, zs)
    ```
  - In the seasons-off path nothing changes (no season mutators are ever added; a world that toggles `SeasonsEnabled` off mid-life heals via the 24-hour decayrate).
- [ ] **Step 2: `weather.go`** — in `rebuildGraph`'s existing `if m.simReady { engine.Reconcile(...) }` block, extend:
    ```go
	if m.simReady {
		engine.Reconcile(m.state.Weather)
		if m.seasonsOn {
			m.zoneSeasons = seasons.ZoneSeasons(m.graph, m.climate, m.tracks, engine.CalendarNow())
			engine.ReconcileSeasons(m.graph, m.zoneSeasons)
		}
	}
    ```
    (Recomputing the map here also prevents stale-zone seasons surviving a rebuild; `seasons` is already imported in weather.go.)
- [ ] **Step 3: Sync + verify (checkout): build, `go test ./modules/weather/...`, vet — green; standalone suites green; `gofmt -l .` clean.**
- [ ] **Step 4: Commit** — `feat(weather): reconcile seasonal mutators at boot, per tick, and post-rebuild`

## Task 7: Documentation

**Files:** Modify `engine/context.md`, root `context.md`, `content/context.md`, `sim/context.md`, `README.md`, `docs/superpowers/specs/2026-06-10-seasons-design.md`.

- [ ] **Step 1:** `engine/context.md` — SeasonMutatorPrefix/SeasonMutatorId/ReconcileSeasons (independent namespace; absent-from-map ⇒ removal; warn-once), reconcileZone's generalized signature, StripBuffs covering both namespaces.
- [ ] **Step 2:** Root `context.md` — the three ReconcileSeasons call sites and why (boot assert, per-tick, rebuild heal). `sim/context.md` — stock-biome coverage note. `content/context.md` — moduledata test now validates both namespaces (14 specs).
- [ ] **Step 3:** `README.md` — extend the Seasons subsection: seasonal ambience layer (description line per season), the **frozen-river example** for builder-authored seasonal exits (an override of `season_temperate_winter.yaml` adding `exits: { "across the ice": { roomid: 123 } }` — note the exact engine `RoomExit` yaml shape should be copied from an engine example at implementation time), and the no-default-buffs decision with the override path. Update the config table if anything changed (nothing should).
- [ ] **Step 4:** Seasons spec — record the S-R2 decision (no namemodifier on seasonal specs) as a dated note under §10's S-R2 row, marked "validated by the S2 smoke render" after Task 8 confirms.
- [ ] **Step 5: Commit** — `docs: document the seasonal mutator layer`

## Task 8: Verification + smoke test

- [ ] **Step 1:** Full clean run (standalone suites + vet + gofmt; checkout generate/build/vet/test) — all green.
- [ ] **Step 2:** Boot smoke (checkout; admin/Password123): boot log shows `seasons active tracks=2 seasonalZones=N` with **N substantially higher than 1** (Task 1 coverage — expect most outdoor stock zones); `look` in a seasonal zone shows the seasonal description line UNDER normal conditions and renders acceptably WITH a weather mutator active (`weather spawn storm <zone> 0.9` → title shows only the weather tag, description carries both lines — record the actual render for the S-R2 note); `weather seasons`/`weather status` consistent; `SeasonsEnabled: false` rebuild → no season mutators applied and existing ones decay (verify via `look` after a few minutes OR accept the decayrate design); restore, rebuild; clean `/shutdown` + reboot → season mutators re-asserted by the boot reconcile without an event flood. Kill server, clean helpers.
- [ ] **Step 3:** Append the S2 status blockquote to the seasons spec §8 (dated, honest, including the observed N and the S-R2 render verdict).
- [ ] **Step 4: Commit** — `docs(spec): record seasons S2 status`
