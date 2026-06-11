# GoMud Weather Module — Admin Page Design Spec

- **Status:** Implemented (AP1 complete, smoke-verified 2026-06-10); awaiting the combined seasons+AP1 merge
- **Date:** 2026-06-10
- **Origin:** Volte6's v0.1.0 review suggestion — "add admin pages. Not strictly
  necessary, but super useful for overriding configs."
- **Baseline:** weather M3 + seasons S1–S3 (the held `worktree-s3-seasons-prose`
  branch — this work stacks on it; one merge ships seasons + admin page).
- **Parent docs:** [weather spec](2026-06-08-weather-module-design.md),
  [seasons spec](2026-06-10-seasons-design.md).

---

## 1. Purpose & decisions

One admin web page for the whole module: edit `Modules.weather.*` config with
honest live-apply semantics, watch the simulation (fronts, per-zone
weather/season, graph), and fire the admin actions (spawn/clear/rebuild) —
through the engine's standard module admin-page mechanism, zero engine
changes.

Decisions locked during brainstorming (2026-06-10):

| Question | Decision |
|---|---|
| Scope | Full: config + status + actions on one page |
| Config apply | **Live where safe, reboot-flagged otherwise** — the page must never lie about when a change takes effect |
| Shape | **A: one page, API-driven** — embedded HTML + vanilla JS polling permission-gated JSON endpoints (the engine's own `gametime.html` pattern) |
| Threading | HTTP handlers NEVER touch module state — snapshot/event bridges only (§3) |
| Branch | Implemented on the held S3 branch; merges with seasons |

## 2. Surface (verified against engine source 2026-06-10)

### 2.1 Registration — in `init()` (the M3 harvest rule applies to web surface too: `plugins.Load()` registers pages/endpoints in the same pre-`onLoad` loop as commands)

- `module.plug.Web.AdminPage("Weather", "weather", "weather.html", true, "Modules", "", "Weather & seasons simulation", "", dataFunc)` — HTML path is relative to `datafiles/html/admin/`, so the file ships at `files/datafiles/html/admin/weather.html`. Page serves at `/admin/weather`, nav entry under the Modules group.
- `module.plug.Web.AdminAPIEndpoint(method, slug, handler, permissionKey...)` — serves at `/admin/api/v1/<slug>`. Handler signature: `func(*http.Request) (status int, success bool, data any)`, wrapped in the engine's `APIResponse`.
- `module.plug.Web.RegisterPermissions(plugins.ModulePermission{Key: "weather.write", Description: "Modify weather module config and fire weather actions", Category: "Modules"})`.

### 2.2 Permission model

Engine convention (documented in `webconfig.go`): mutating endpoints carry a
`<module>.write` key; GET/read endpoints carry none (admin session required
regardless). We register **`weather.write`** for the three mutating endpoints.
The in-game admin commands stay gated on `weather` — and the engine's prefix
permission semantics mean a mod granted `weather` automatically satisfies
`weather.write`, so one grant covers both surfaces; `weather.write` alone
grants page-write without the in-game commands.

### 2.3 Endpoints

| Method | Slug | Permission | Body → Result |
|---|---|---|---|
| GET | `weather/status` | (admin session) | → the current `AdminSnapshot` (§3.1) |
| POST | `weather/config` | `weather.write` | `{key, value}` → validated write + `WeatherConfigChanged` queued; responds with the per-key apply verdict ("live" / "reboot" / error) |
| POST | `weather/action` | `weather.write` | `{action: "spawn"\|"clear"\|"rebuild", type?, zone?, intensity?}` → shape-validated, queued as `WeatherAdminAction`; responds accepted (results visible via the next snapshot) |

Actions are **asynchronous by design** (executed on the game loop); the page
re-polls status after firing one. Clear/rebuild get a JS confirmation.

### 2.4 The page

Single `weather.html` (engine admin look-and-feel, vanilla JS):
- **Status panel** — sim state (running/idle, seasons on/off, next tick round,
  seed), active fronts table, per-zone weather+season table, graph summary.
  Polls `GET status` every ~5s.
- **Config panel** — one row per key: current value, input control, an apply
  badge (`live` / `takes effect on reboot` / key-specific note), Save per key.
  Badge text comes from the snapshot — the UI cannot drift from the module's
  actual semantics.
- **Actions panel** — spawn (type/zone/intensity inputs), clear (optional
  zone), rebuild graph.

## 3. Threading model (the load-bearing section)

Admin API handlers run on the web server's goroutines; the module is
mutex-free by the MainWorker single-goroutine invariant. Handlers therefore
never read or write module state. Three one-way bridges:

### 3.1 Status: atomic snapshot

`AdminSnapshot` — a plain, deep-copied struct (sim running/seasons state,
config view + per-key apply semantics + pending-reboot flags, fronts, per-zone
weather/season rows, graph summary, next tick/emote rounds, build/version
info). Built on the game loop at the end of `startSim`, every `tick`, and
after every admin action/config apply; published via `atomic.Pointer
[AdminSnapshot]`; `GET status` returns the current pointer's contents. Staleness
is at most one tick — the same contract as `GetSeason`. A nil pointer (before
first build) returns a valid "sim not started" snapshot.

### 3.2 Actions: module-defined events

`WeatherAdminAction{Action, WeatherType, Zone, Intensity}` queued via
`events.AddToQueue` (thread-safe). A module listener (registered in `onLoad`
alongside `NewRound`) executes on MainWorker through the SAME paths as the
in-game commands — `sim.ForceSpawn`/`sim.ClearZones` + `engine.Reconcile` +
`persistState`, or `rebuildGraph` — then refreshes the snapshot. Zone
resolution and validation against the live graph happen in the listener
(handler validates shape only); a failed action surfaces in the snapshot's
`LastActionResult` field rather than the HTTP response.

### 3.3 Config: engine config layer + change event

`POST config` → validate key against a whitelist of the module's known keys +
coerce value → `plugin.Config.Set(key, value)` (engine `configs.SetVal`:
**verified to persist** — merges into the overrides map, saves the override
file to disk, overlays in-memory, fires engine callbacks) → queue
`WeatherConfigChanged{}`. The module's listener re-runs `loadConfig` on the
game loop and applies per §4.

**Plan-time verification item (V-1):** `configs.SetVal` validates property
paths against the engine's key lookups; confirm `Modules.weather.<key>` paths
pass (the `PluginConfig.Set` wrapper exists precisely for this, so they
should). If they do not, the fallback is the same mechanism the engine's own
admin config page uses — identify and reuse it. This is Task 1 of the plan;
nothing else builds until it's proven with a real write+reboot round-trip.

## 4. Per-key apply semantics

| Key | On save | Notes |
|---|---|---|
| `TickEveryGameHours`, `MaxActiveFronts`, `SpawnRateScale` | **live** | re-derive `simCfg`, reschedule next tick |
| `EmoteMode`, `EmoteEveryRounds` | **live** | reschedule next emote pass |
| `Persist` | **live** | gates `persistState` from now on |
| `SeasonsEnabled` | **live, both directions** | on→off: `seasonsOn=false` + `ReconcileSeasons` with an empty map (strips season mutators immediately); off→on: re-run `loadSeasons` (idempotent; baseline without events) |
| `BuffsEnabled` | **live one-way** | true→false: `StripBuffs()` anytime; false→true: **reboot badge** (no restore path by design) |
| `Enabled` | **reboot badge** | listener/registration topology is boot-time |
| `Seed` | **deferred badge** | "applies when state is re-seeded" |
| `IncludeSecretExits` | **deferred badge** | "applies on next graph rebuild" |
| `RebuildGraphOnBoot` | saved as-is | inherently a boot flag |

The apply table lives in module code as data (key → applier func + badge
text); the snapshot serializes it so the page renders truth.

## 5. Testing

- **Unit (in-checkout):** snapshot builder (pure given module state), config
  whitelist/coercion/apply-table dispatch, action-event validation — all
  testable without HTTP via the existing seams.
- **Handler glue:** thin; exercised via `httptest`-style direct handler calls
  where practical (handlers take `*http.Request`), else smoke.
- **Smoke (browser + scripted HTTP):** page loads under `/admin/weather`; the
  nav entry appears; status polls; a live key (e.g. `SpawnRateScale`) edits +
  applies + persists across reboot (V-1 round-trip); a reboot-badge key shows
  its badge; spawn/clear/rebuild fire and show up in the snapshot;
  `weather.write` enforcement (a session without it gets refused on POSTs).

## 6. Risks & open items

| # | Risk | Mitigation |
|---|---|---|
| AP-R1 | V-1: `SetVal` may reject `Modules.*` paths | Plan Task 1 proves the round-trip before anything else; fallback = the engine admin config page's own save path |
| AP-R2 | Snapshot copy cost per tick | Tiny (tens of zones, few fronts); built once per game hour + on demand |
| AP-R3 | Async actions confuse admins expecting sync results | Page re-polls after actions; snapshot carries `LastActionResult` |
| AP-R4 | Config writes racing the game loop's re-read | `SetVal` is engine-locked; the module only reads config on MainWorker via the change event — no torn reads |
| AP-R5 | The engine has no module admin page in-tree to copy verbatim | The registrar API is fully documented in `webconfig.go`; the engine's own `gametime.html` is the styling/JS reference |

## 7. Deferred seams

Per-key permission granularity beyond `weather.write`; a public (non-admin)
weather status web page (`Web.WebPage` exists); charts/history; config
editing for climate/track/emote DATA files (those are build-time embedded —
out of scope by construction).
