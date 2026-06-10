# GoMud Weather Module — Seasons (v2) Design Spec

- **Status:** Approved design, pre-implementation
- **Date:** 2026-06-10
- **Parent spec:** [Weather module design](2026-06-08-weather-module-design.md) — §11 sketched the seams this spec now fills in. Where the two disagree, this spec wins for seasons.
- **Baseline:** Weather v0.1.0 (M3 complete, registry-listed). Seasons build strictly on top; no behavior change when disabled.

---

## 1. Purpose & decisions

Seasons make the weather's odds, look, and mechanics follow the world's
calendar — winter triples snowfall in the highlands while the jungle swings
between wet and dry — without touching the proven deterministic simulation
core.

Decisions locked during brainstorming (2026-06-10):

| Question | Decision |
|---|---|
| Scope | Full: weather odds + presentation + mechanics (staged S1–S3) |
| Clock | Calendar months (GoMud `gametime`); pacing inherited from world time config |
| Season systems | **Data-defined per-biome tracks** — temperate, monsoon, none, anything; a biome binds to a track; hemispheres = phase-shifted tracks |
| Transitions | Weather odds **blend linearly** across a per-season window; presentation/mechanics/events flip on the boundary day |
| Mechanics in scope | Seasonal ambient mutators, seasonal buffs, seasonal exits (builder-authored), `SeasonChanged` event + `GetSeason` export |
| Prose model | Two layers, sparse: seasonal mutators/emotes carry the persistent look; weather emote tables gain *optional* per-season variants with full fallback |
| Architecture | **A: pure climate transform** — a `seasons/` package computes an effective `sim.Climate` per tick; `sim.Step` is untouched |
| Esoteric seasons (raised by Volte6, 2026-06-10) | Stock cycles ship ready-made; *fantastical* ones (a season where it rains glass, Broken-Earth style) must be YAML-only. Seasons can therefore **add** weather types absent from the base climate (`weatherWeightAdditions`) and **suppress all normal weather** with one knob (`baseWeightScale`) — see §3.1a |

Stock pacing for intuition: 900 rounds/day × 4s rounds = one game day per real
hour; 365-day year over 12 months ⇒ a month ≈ 1.3 real days, a 3-month season
turns ≈ every 4 real days. Worlds that retune time inherit their own pacing.

## 2. Architecture

```
                       gametime (round → day-of-year, days-per-year, month count)
                                        │
files/datafiles/seasons/*.yaml ──► seasons.Tracks ──┐
                                                    ▼
sim.DefaultClimate + climate overrides ──► seasons.EffectiveClimate ──► sim.Step (UNCHANGED)
        (each biome may carry track:)               │
                                                    ▼
                                       seasons.ZoneSeasons (zone → {track, season, blend})
                                                    │
                              ┌─────────────────────┼──────────────────────┐
                              ▼                     ▼                      ▼
                    season-* mutator reconcile   WeatherSeasonChanged   GetSeason export,
                    (descriptions/buffs/exits)   event on flips         command surface,
                                                                        seasonal emotes
```

Principles carried over from v1: the new `seasons/` package is **pure**
(yaml.v2 + `sim` types only; arch-tested); season state is **never persisted**
(always derivable from the round, so reboots are consistent by construction);
`engine.Reconcile`-style application is the single path to engine mutators,
now in two independent namespaces (`weather-*`, `season-*`); everything fails
soft.

## 3. Data model

### 3.1 Track files — `files/datafiles/seasons/<track>.yaml`

One file per cycle (the climate/emotes one-file-per-thing pattern):

```yaml
track: temperate
seasons:
  - name: winter
    months: [12, 1, 2]          # 1-based calendar month numbers
    transitionDays: 6           # blend window entering this season (game days)
    weatherWeightMultipliers: { snow: 3.0, blizzard: 2.0, rain: 0.4, heatwave: 0.0 }
    spawnWeightMultiplier: 0.9
    influence: { intensityDelta: -0.02 }   # ADDED to the biome's own influence
  - name: spring
    months: [3, 4, 5]
    transitionDays: 6
    weatherWeightMultipliers: { rain: 1.5, fog: 1.3 }
  - name: summer
    months: [6, 7, 8]
    transitionDays: 6
    weatherWeightMultipliers: { storm: 1.4, heatwave: 1.5, snow: 0.0, blizzard: 0.0 }
  - name: autumn
    months: [9, 10, 11]
    transitionDays: 6
    weatherWeightMultipliers: { fog: 1.6, overcast: 1.3 }
```

- A monsoon track is the same schema with two seasons (`wet`/`dry`).
- A southern-hemisphere temperate track is the same four seasons with months
  shifted by half the year. No dedicated hemisphere machinery.
- Multiplier semantics: missing weather type ⇒ ×1.0; `0.0` removes the type
  for the season. `spawnWeightMultiplier` defaults 1.0. `influence` deltas are
  added to the biome profile's own values (then the sim's existing clamps
  apply). `transitionDays: 0` means a hard flip for that season.

### 3.1a Esoteric seasons — additions and base suppression

Multipliers can only scale weights the biome already has (`3.0 × 0 = 0`), so
two optional per-season fields make fantastical seasons pure YAML instead of a
base-climate rewrite:

```yaml
  - name: shattering            # a Broken-Earth-style Season
    months: [8, 9]
    transitionDays: 2
    baseWeightScale: 0.0        # suppress ALL normal weather (default 1.0)
    weatherWeightAdditions:     # absolute weights ADDED for this season only —
      glassrain: 8              #   may introduce types absent from the base climate
      ashfall: 3
    spawnWeightMultiplier: 2.0
    influence: { intensityDelta: 0.05 }
```

Effective weight per type: `base × lerp(prevScale×prevMult, curScale×curMult)
+ lerp(prevAdd, curAdd)` (additions lerp from/to 0 when a neighboring season
lacks them, so they blend in and out like everything else). The weather types
referenced by additions follow the existing new-type recipe (a
`weather_<type>.yaml` mutator spec + an emote table) — no Go anywhere. A
season whose effective table is all-zero is well-defined: no new fronts spawn
in those biomes, existing fronts keep their type until they die, zones resolve
`Clear` — a "dead sky" season is one line of YAML.

The stock experience is unaffected: the shipped `temperate`/`monsoon` tracks
use neither field, and standard biomes come pre-bound, so out-of-the-box
setup remains zero-authoring.
- **Validation at load:** every calendar month 1..N (N from the world's
  calendar) must be claimed by exactly one season; months outside 1..N, gaps,
  or overlaps ⇒ the track is rejected with a logged warning (fail-soft: biomes
  bound to a rejected track behave as unbound). Defaults ship assuming the
  stock 12-month calendar; a world with a different month count must adjust
  the track files (a deliberate authoring decision, not a silent remap).

### 3.2 Biome → track binding

Climate profiles (file and `DefaultClimate()` alike) gain one optional key:

```yaml
biome: tundra
track: temperate     # NEW — which season cycle this biome follows
weather: { ... }
```

- **No `track` = no seasons for that biome** (a plane of fire opts out by
  omission; v1 behavior exactly).
- Default bindings shipped in `DefaultClimate()`: `plains`, `forest`,
  `mountain`, `tundra`, `swamp`, `ocean` → `temperate`; `desert`, `default` →
  unbound. (A `jungle` biome + `monsoon` binding ships with the default
  content milestone.)
- Zones inherit their biome's track. **Per-zone track overrides are a
  documented seam, not v2** — which is also the honest hemisphere limitation:
  binding is per-biome, so distinct hemispheres need distinct biome ids until
  zone overrides exist.

### 3.3 Blending

Entering a season, its `transitionDays` window linearly interpolates every
numeric modifier (per-type `scale×multiplier` factors, weight additions,
spawn multiplier, influence deltas) from the outgoing season's values to the
incoming season's. Blend factor is a pure
function of `(track, dayOfYear, daysPerYear)`. Only the *climate math* blends;
mutators, emotes, events, and the reported season flip on the boundary day.
Year wraparound (last season → first) blends the same way.

## 4. The `seasons/` package (pure)

```go
// Load parses every *.yaml track under dir and validates against the
// calendar shape. Invalid tracks are dropped with a reported error list
// (caller logs and continues — fail-soft).
func Load(fsys fs.FS, dir string, monthsPerYear, daysPerYear int) (Tracks, []error)

// Resolve reports where a track stands at a calendar position.
func (t Track) Resolve(dayOfYear int) (current, previous SeasonName, blend float64)

// EffectiveClimate returns a season-adjusted COPY of base for this calendar
// position: per biome with a bound track, weights/spawn/influence are
// multiplied/added per the resolved season (blended in-window). Biomes
// without a track (or with an unknown track) pass through untouched.
func EffectiveClimate(base sim.Climate, tracks Tracks, pos CalendarPos) sim.Climate

// ZoneSeasons maps every zone to its resolved season via its biome's track.
// Zones whose biome has no track are absent from the map.
func ZoneSeasons(g *sim.Graph, base sim.Climate, tracks Tracks, pos CalendarPos) map[sim.ZoneId]ZoneSeason

type ZoneSeason struct{ Track, Season string; Blend float64 }
type CalendarPos struct{ DayOfYear, DaysPerYear int }
```

`CalendarPos` comes from a thin `engine/` gametime helper (round →
day-of-year/days-per-year/month-count); the package itself never touches the
engine. Purity enforced by an arch test, same as `sim`/`crawler`/`content`.

`sim.Step` is **unchanged**: it already takes `Climate` as an input and is
agnostic to the table varying tick-to-tick. Golden traces and the determinism
suite stay valid as-is.

## 5. Runtime integration

Per-tick pipeline in the module root becomes:

1. `pos := engine.CalendarNow()` (new helper)
2. `effClimate := seasons.EffectiveClimate(m.climate, m.tracks, pos)`
3. `next, _ := sim.Step(m.state, m.graph, effClimate, m.simCfg, clock)` — as today
4. `engine.Reconcile(next.Weather)` — as today (weather layer)
5. `zs := seasons.ZoneSeasons(...)`; `engine.ReconcileSeasons(zs)` — NEW layer:
   per zone, exactly one `season-<track>-<season>` mutator, applied through the
   same tested `reconcileZone` core with the `season-` prefix. The two
   namespaces never interact.
6. Zones whose resolved season differs from the previous tick queue a
   module-defined `WeatherSeasonChanged{Zone, Track, From, To}` event on the
   engine bus (other modules listen normally). The previous-tick season map is
   ordinary in-memory module state (not persisted): the first tick after boot
   establishes the baseline and emits no events, so reboots never replay a
   flood of season changes.

Boot/`weather rebuild` reconcile asserts both layers. Nothing about seasons is
persisted. Admin/diagnostic surface: the player `weather` view and
`weather status`/`weather zones` gain the local season; a `weather seasons`
admin subcommand lists tracks and current positions.

### 5.1 Seasonal mutators (mechanics)

`files/datafiles/mutators/season_<track>_<season>.yaml`, ordinary engine
`MutatorSpec`s in the `season-` namespace:

- **Descriptions** are biome-neutral by design (one spec per track×season;
  biome-specific seasonal *prose* lives in emote tables; biome-variant
  seasonal mutators are a seam).
- **Buffs**: curated, gentle, on the existing ids where sensible (deep winter
  outdoor chill → 31), gated by the existing `BuffsEnabled` + `StripBuffs`.
- **Exits** (frozen river, snowed-in pass): the `MutatorSpec.Exits` field —
  shipped defaults carry **no** exits (inherently world-specific); builders
  author per-world `season-*` overrides. Documented with an example.
- Same hard rules as weather specs, enforced by extending the shipped-data
  tests to the `season-` prefix: `decayrate` required (safety net), no
  `respawnrate`, no `decayintoid`.

### 5.2 Exported API

- `GetSeason(zone string) map[string]any` → `{"track": "temperate",
  "season": "winter", "blend": 0.0}` (empty values when unbound/not running).
- `GetWeather` unchanged. `WeatherSeasonChanged` is the push-side complement.

## 6. Prose (two layers, sparse)

1. **Seasonal ambience** — seasonal emote tables reuse the existing
   `content.Table` shape and the existing jittered scheduler, keyed by the
   zone's `(track, season)` (file: `emotes/season_<track>_<season>.yaml`),
   biome/indoor selection identical to weather emotes. They fire at a lower
   rate than weather emotes and never when a weather emote was just sent
   (weather wins; one ambient line per pass per room).
2. **Weather emote seasonal variants** — weather tables gain an optional
   `seasonal:` block:

   ```yaml
   weather: rain
   outdoor:
     default: ["Rain patters down steadily around you."]
   seasonal:
     winter:
       outdoor:
         default: ["Freezing rain rattles off every surface like thrown gravel."]
   ```

   Lookup order: seasonal-variant biome → seasonal-variant default → base
   biome → base default. Season names match across tracks by string (a
   monsoon world's tables use `wet`/`dry`). Missing variants fall through —
   the default ship adds only high-value variants, not the matrix.

## 7. Configuration

One new key: `SeasonsEnabled` (default `true`). Everything else is data.
Fail-soft ladder: seasons disabled, no track files, all tracks invalid, or a
calendar the tracks don't fit ⇒ log once, run exactly as v1 weather.

## 8. Milestones

- **S1 — Season core:** `seasons/` package (+tests), climate `track` key,
  calendar helper, effective-climate transform wired into the tick,
  `GetSeason`, `WeatherSeasonChanged`, command/status surface,
  `SeasonsEnabled`. *Observable: weather odds shift with the calendar.*

  > **2026-06-10 — S1 implemented and smoke-verified.** Standalone + checkout
  > suites green (`go test`/`go vet`/`gofmt`, plus `go generate`/`go build` in
  > the GoMud checkout). Boot on the stock world logs
  > `Weather: seasons active tracks=2 seasonalZones=1` with no season warnings;
  > both tracks load (`temperate`, `monsoon`). Odds shift with the calendar via
  > the EffectiveClimate transform on the tick; `GetSeason`,
  > `WeatherSeasonChanged`, and the `weather`/`weather seasons`/`weather status`
  > command surface are all live. `weather` in the one seasonal zone (Dark
  > Forest, `forest`→`temperate`) reports "The season here is winter," matching
  > calendar day 1/365 = month 1 (Arvalon) under the temperate mapping.
  > `SeasonsEnabled: false` rebuilds to clean v1 behavior (no `seasons active`
  > line, `weather seasons` → off, weather still simulates); re-enabling
  > re-baselines on reboot with the persisted state restored and no
  > `WeatherSeasonChanged` event flood. Notes: only **1** zone is seasonal on the
  > stock world because just `forest` among the crawled biomes maps to a track —
  > other temperate-mapped biomes are absent and several crawler biome names
  > (`mountains`/`shore`/`snow`) don't match the `DefaultClimate` keys
  > (`mountain`/`ocean`/`tundra`), and the `monsoon` track has no biome bound to
  > it; revisit biome→climate coverage in S2. This GoMud build also has no `time`
  > user command, so the month was cross-checked via the calendar directly. S2
  > (seasonal mechanics) is next.
- **S2 — Seasonal mechanics:** `ReconcileSeasons` layer, default
  `season_*` mutator specs (temperate ×4, monsoon ×2) with curated buffs,
  exits seam + builder docs, shipped-data test extension.
- **S3 — Seasonal prose & content:** seasonal emote tables, `seasonal:`
  weather-variant support, default content pass (incl. `jungle`/`monsoon`),
  README/builder-guide updates.

Each milestone is its own plan → review → implement cycle with the standing
approval gates.

## 9. Testing

- `seasons/`: pure unit tests — track validation (gaps/overlaps/out-of-range
  months, non-12 calendars), resolution incl. year wraparound, blend math at
  window edges, effective-climate pass-through for unbound biomes,
  ZoneSeasons mapping. Arch purity test.
- Sim: untouched — golden trace + determinism suite must pass unmodified
  (that's the architecture's regression guarantee).
- Engine: `ReconcileSeasons` over the existing fake `mutatorSet` seam;
  namespace isolation (weather reconcile never strips `season-*` and vice
  versa — this requires scoping the existing weather reconcile's "strip
  foreign weather-* ids" logic to its own prefix, already true by prefix
  match).
- Content: `moduledata_test.go` extended to `season_*` specs and seasonal
  emote tables; `seasonal:` lookup-order unit tests in `content`.
- Smoke: pin the calendar near a boundary with the admin gametime tools,
  observe blend (odds shift) then flip (mutator/event), `GetSeason` and
  command surface, and a reboot mid-season (reconcile re-asserts with no
  persisted state).

## 10. Risks & open questions

| # | Risk / question | Mitigation / answer |
|---|---|---|
| S-R1 | Two ambient-emote sources could feel spammy | Seasonal emotes fire at a lower cadence and yield to weather emotes; both ride one scheduler pass |
| S-R2 | Mutator count per zone grows to 2 (weather + season) | Engine merges zone mutators at render already; verified pattern. Watch render length with both name tags — seasonal specs may omit `namemodifier` if titles get noisy (decide in S2 with real renders) |
| S-R3 | Worlds with non-12-month calendars get no defaults | Deliberate: track files must be authored to the calendar; fail-soft to v1 weather otherwise |
| S-R4 | Hemisphere support limited to biome-id granularity | Documented; per-zone track override is the designed seam |
| S-R5 | Blend window vs `TickEveryGameHours` interaction (long ticks may sample a window once or never) | Blend is sampled per tick; with stock settings (24 ticks/day, 6-day window) ≈ 144 samples. Document that very long tick cadences coarsen blending — cosmetic only |
| S-Q1 | Should `evolveTypes` keep-bias interact with seasons (a summer-born storm crossing into a winter zone)? | No special-casing in v2: type evolution already re-rolls from the (now seasonal) destination climate. Revisit only if play feel demands |

## 11. Deferred seams (explicitly not v2)

Per-zone track overrides (true hemispheres), biome-variant seasonal mutators,
season-aware climate *file* schema changes beyond `track:`, seasonal
`MaxFrontRadius`/coverage modulation, crop/forage content (other modules', via
`WeatherSeasonChanged`/`GetSeason`), GMCP season data.
