# GoMud Weather Module — M4 Polish Design Spec

- **Status:** Implemented on branch worktree-m4-polish — 2026-06-11; smoke-verified
- **Date:** 2026-06-10
- **Baseline:** main `0e8661f` (weather M3 + seasons S1–S3 + admin page AP1,
  merged and pushed). **The next release (v0.2.0 + registry bump) is held
  until M4 lands** — one release ships everything since v0.1.0.
- **Parent docs:** [weather spec](2026-06-08-weather-module-design.md) (§9.1
  refinement, §9.5 buffs, M4 milestone row), [seasons spec](2026-06-10-seasons-design.md),
  [admin page spec](2026-06-10-admin-page-design.md).

> **Smoke status (2026-06-11):** the final OOBE smoke passed all eight checklist
> items on upstream GoMud's stock world: clean boot (32 mutator specs, 46 buff
> specs including the three module buffs, no weather warnings); room-scoped storm
> application in `occupied` mode (`weather-storm` lands on room instance lists,
> indoor rooms render only the sheltered `-indoor` line with no name tag/alert,
> zone-configs never carry weather mutators); refine-on-entry with the documented
> one-render lag; blizzard → 59001 Weather Chilled outdoors, fading within ~2-3
> rounds of shelter with no reapply; live `PerRoomRefinement` occupied↔off
> switching via the admin API with read-back-verified 200s and validation 400s
> (bad enum, out-of-range int, read-only `BuffOverrides.*`); a
> `BuffOverrides: {storm: "59001"}` file round-trip (Chilled with the override,
> Soaked 59002 after removal); persistence across graceful shutdowns
> (`Weather: restored simulation state`, front ids/ages preserved, correct
> weather on re-entry, no stale zone mutators, no warn growth); and zone-wide
> seasonal prose in both modes, including never-refined rooms.
> The smoke campaign surfaced **two engine defects**, both mitigated in-module
> and queued for a separate upstream PR: (1) `configs.AddOverlayOverrides` →
> `Config.OverlayOverrides` REPLACES the inner `Modules.<module>` map instead of
> merging, wiping every operator-set key from the live config on upgrade boots —
> mitigated by the boot self-heal (`healConfigClobber`, 7fe71bf; observed healing
> 12 keys on boot 1 and correctly silent on every subsequent boot); (2) the
> engine's `PluginConfig.Set` swallows `configs.SetVal` errors, so rejected
> writes look identical to successes — mitigated by read-back-verified admin
> config writes (265666f + 7fe71bf). UNVERIFIED: the CI workflow is authored but
> has never executed — it first runs on push to the org repo.

---

## 1. Scope & decisions

Four work streams, locked with Calabe 2026-06-10:

| Stream | Decision |
|---|---|
| **Per-room refinement** | **Room-scoped application** when enabled: weather mutators go on individual rooms (outdoor rooms get `weather-<type>`, indoor rooms get a new `weather-<type>-indoor` variant) instead of zone-wide — true divergence, because the engine renders zone mutators in every room and a room cannot subtract one. `PerRoomRefinement: occupied\|all\|off`, default `occupied`. |
| **Buff completion** | Bespoke module buffs shipped via the upstream buff overlay (landed during M3) in a **documented high-id range (59001+)**, gentle by design; plus `BuffOverrides.<type>` config for worlds to remap. `BuffsEnabled` stays the master switch. |
| **Content & docs** | Per-biome emote variants across the stock biomes (sparse-smart, not the full matrix); OOBE polish; a builder-extension guide section in the README. `DefaultClimate()` stays Go data (no YAML externalization — drift risk for no gain). |
| **Infra & polish** | GitHub Actions CI on the org repo — including the long-blocked `go test -race` on Linux AND the full engine-coupled suite (CI clones upstream GoMud and syncs the module in). `ExcludeZonePatterns` becomes a real config key. The four AP1 polish items (config-value 400s, bool checkboxes, badge color nuance, double publish). |

Out of scope (backlog): `PrevailingWind`, per-zone season-track overrides,
per-biome seasonal *mutator* variants, climate-YAML externalization.

## 2. Per-room refinement (the design section)

### 2.1 The three modes

- **`off`** — exactly today's behavior: zone-wide weather mutators, nothing
  per-room. The safe fallback and the no-cost path.
- **`all`** — when a zone's weather changes, every room in the zone gets a
  room-level mutator: `weather-<type>` on outdoor rooms, `weather-<type>-indoor`
  on indoor rooms (biome heuristic, same set the emote layer uses). **No
  zone-wide weather mutator is applied in this mode.** Highest fidelity,
  highest cost (rooms × zones reconciles per tick).
- **`occupied`** (default) — no zone-wide weather mutator; only rooms that
  currently contain players are refined each reconcile pass, and a
  **room-entry listener** refines a room the moment a player moves into it.
  Cost scales with the online population, not the world.

### 2.2 Mechanics

- **Room mutator surface (verified M3):** `room.Mutators` is a
  `mutators.MutatorList` with the same Add/Remove/Has/Update lifecycle as zone
  lists; the engine merges room+zone mutators at render and updates room
  mutators each round. The existing prefix-scoped `reconcileZone` core is
  already list-agnostic — `ReconcileRooms` reuses it per room. **This is the
  consumer that finally retires/absorbs the dormant `engine.Apply`.**
- **Refine-on-entry:** listener on the engine's room-change event (exact event
  name/shape verified at plan time — the engine emits a room-move event; if it
  somehow doesn't, fallback is refreshing on the player's next `look` via the
  scheduler, noted honestly in the plan). One-render lag is accepted and
  documented: if the engine renders the destination room before our listener
  runs, the first render may lack the weather line; the room is correct from
  the next render on.
- **Mode transitions & boot:** room mutators persist in room files (rooms save
  on autosave/shutdown — M3 finding), so refinement must reconcile at boot the
  same way zone mutators do. `occupied`: boot pass refines occupied rooms and
  STRIPS `weather-*` room mutators found anywhere else (heal). Switching modes
  (incl. via the admin page — the key joins `configKeyMeta` as live) triggers a
  full strip-and-reapply pass. The `decayrate` safety net covers anything a
  crash orphans, exactly as for zones.
- **Indoor variant specs (`weather_<type>_indoor.yaml` ×8):** sheltered prose
  ("rain hammers the roof"), **no `lightmod`** (shelter), **no buffs** (you're
  out of the weather), `decayrate` safety net, the same forbidden-field rules
  (no respawnrate/decayintoid), validated by the shipped-data tests. Ids
  `weather-<type>-indoor` stay inside the module's `weather-` namespace, so
  the existing zone reconciler must learn to ignore room-level ids it doesn't
  own — scoping rule: zone reconcile touches zone lists only, room reconcile
  touches room lists only; the namespaces stay `weather-*` for both (the LIST
  they live on is the isolation boundary, mirroring how `season-*` isolation
  works by prefix).
- **Seasons interaction:** seasonal mutators stay **zone-wide in all modes**
  (a season is zone-truth; the indoor/outdoor distinction for seasons is
  already handled by the emote layer). Only the weather layer goes per-room.
- **Snapshot/admin:** the admin snapshot gains the refinement mode and a
  refined-room count; the config key's badge is "live" (mode switches
  strip-and-reapply on the game loop).

### 2.3 What `occupied` mode means for players (documented honestly)

An unoccupied room has no weather description until someone enters (then it
refines). Ambient emotes are unaffected (already occupied-only). `look` from
an adjacent room into an unrefined room is not a case the engine renders
descriptions for, so the lag is effectively invisible in normal play.

## 3. Buff completion

- **Bespoke specs** shipped via the plugin FS (`buffs.RegisterFS` — verified
  M3; loader filename rule `<id>-<name>.yaml` verified at plan time against
  `fileloader`): a small, gentle set in the documented range, e.g.
  `59001 weather-chilled` (blizzard: minor cold tick), `59002 weather-soaked`
  (storm: minor accuracy/movement nuisance), `59003 weather-parched`
  (heatwave/dust: minor thirst-flavored tick). Exact effects authored at plan
  time against real engine buff capabilities, tuned gentler than the borrowed
  31/33. Weather mutator specs switch their `playerbuffids` to these.
- **Collision posture:** ids 59001+ are documented in the README's
  what-can-break section; the engine ships nothing near that range; a world
  that collides remaps with overrides. (Volte6's eventual id-namespacing
  scheme supersedes this when it exists.)
- **`BuffOverrides.<weatherType>` config:** flat dotted keys (the M3 finding —
  plugin config reads flattened scalar leaves), value = comma-separated buff
  ids; empty string = strip that type's buffs. Read for each known weather
  type at startSim; applied as spec mutation via `GetMutatorSpec` (the
  StripBuffs mechanism), boot-time-only like StripBuffs, badge says so on the
  admin page (the keys join `configKeyMeta` as reboot-badged rows; the admin
  page's config table picks them up from the snapshot automatically).
- `BuffsEnabled: false` continues to strip everything, overrides included.

## 4. Content & docs pass

- **Emote variants:** for each high-traffic stock biome (`city`, `mountains`,
  `shore`, `water`, `snow`, `farmland`, `road`, `swamp`, `desert` + existing
  `forest`), add 1–2 outdoor lines to the weather types that matter most for
  that biome (rain/storm/fog/snow as appropriate — target ~25 new lines, not
  the 8×10 matrix). One or two seasonal-ambience biome variants where they
  sing (e.g. `city` winter). Validated by the existing shipped-data tests.
- **README:** a "Extending the module" builder-guide section consolidating the
  recipes (new weather type, new track, esoteric season, buff override,
  per-room refinement modes, seasonal exits) — much of this exists scattered;
  the pass consolidates and completes it. What-can-break gains the buff-id
  range note and the refinement mode caveats.
- **OOBE:** with refinement defaulting to `occupied`, the
  install-and-it-works bar must re-verify end-to-end in the final smoke.

## 5. Infra & polish

- **CI (GitHub Actions, org repo):** workflow on push/PR — job 1 (standalone):
  gofmt check, `go vet`, `go test` over the four pure packages, **with
  `-race`** (Linux runner closes the long-standing toolchain gap); job 2
  (engine-coupled): clone upstream GoMud master, rsync the module into
  `modules/weather/`, `go generate`, `go build`, `go vet` + `go test
  ./modules/weather/...` (also `-race`). Job 2 tracks upstream master, so it
  doubles as an early-warning for upstream API drift — failures there are
  signal, not noise, and the README/CONTRIBUTING note explains that.
- **`ExcludeZonePatterns` config key:** comma-separated globs, default
  `instance_*,ephemeral_*`, parsed into `crawler.Options`; applies on next
  rebuild (badge: deferred). Closes the spec §10 annotation.
- **AP1 polish:** `handleAdminConfig` validates/coerces values per key type
  and 400s on garbage (the apply table gains a `Validate` func per key); bool
  keys render as checkboxes; the badge styling treats "live to disable…" as
  mixed (amber) rather than green; the redundant double publish on rebuild
  collapses.

## 6. Testing

- Refinement: the room-reconcile core via the existing fake `mutatorSet` seam
  (it's list-agnostic — same tests pattern as zones); mode-transition
  strip-and-reapply unit-tested with fabricated module state; entry-refinement
  listener glue smoke-verified (needs the live room registry). Indoor variant
  specs validated by the shipped-data tests (count rises to 22 mutator specs).
- Buffs: shipped buff specs validated (parse, id range, filename rule);
  override parsing unit-tested (comma lists, empty = strip, garbage ignored
  with warn); applied-spec mutation tested via the registry-seam pattern.
- CI: the workflow itself is validated by being green on the PR that adds it.
- Final smoke: OOBE boot with defaults (`occupied`) — enter a stormy zone's
  indoor room and see sheltered prose with no storm description; admin page
  shows refinement mode and the new config keys; buff 59001 lands on a player
  in a blizzard zone (outdoor) and not indoors; CI green on the pushed branch.

## 7. Risks & open items

| # | Risk | Mitigation |
|---|---|---|
| M4-R1 | Room-entry event shape unknown until plan-time verification | Plan Task gate (like AP1's V-1): identify the event and prove a listener sees room moves before building refinement on it; fallback documented (next-render refresh) |
| M4-R2 | `occupied` boot-heal pass touches many room files' in-memory lists | It only STRIPS where stale ids exist; rooms load lazily in GoMud (`LoadRoom`) — the heal must not force-load the whole world. Design: heal lazily too — strip stale room weather mutators when a room is next loaded/entered, plus the decayrate net. Verified at plan time against room-manager loading semantics |
| M4-R3 | Buff id range collides with some world | Documented range + overrides escape hatch; near-zero practical risk |
| M4-R4 | CI job 2 breaks on upstream churn | By design (early warning); job 1 remains the merge gate |
| M4-R5 | `all` mode cost on big worlds | Documented as the expensive option; `occupied` is default |
