# GoMud Weather Module

A weather (and, later, seasons) system for [GoMud](https://github.com/GoMudEngine/GoMud)
worlds. Weather forms as discrete **systems** that move across a graph of the
world's geography, gather or lose strength based on the **terrain they cross**,
and express themselves through GoMud's existing room **mutators** — descriptions,
ambient emotes, light, and curated, overridable buffs.

> A storm forms over the coast, rolls inland across the plains gathering
> strength, climbs into the mountains where the terrain bleeds it dry, and
> dissipates on the far side — and players in each zone along the way feel it
> arrive, pass, and leave.

Built in the same spirit as the
[GoMud Module Playtest Harness](https://github.com/GoMudEngine/GoMud-Module-Playtest-Harness):
engine-native, compiled-in, data-driven, and testable in isolation.

---

## Status

**M1b complete — crawler runs against a live world.** On top of the pure core
(`sim/`, `crawler/`), the `engine/` adapter reads the live GoMud world, the
module builds a geography graph on the first round, caches it to disk, and
exposes an admin `weather` command (summary / `graph [zone]` / `rebuild`).
Smoke-tested on upstream GoMud's default world (15 zones, build → persist →
reload verified). Built for upstream GoMud; the only DOGMud backport delta is
the one-line `sendLine` helper. Next: M2 (weather simulation core).

The full design remains the source of truth for what we're building and why:

- **[Design spec](docs/superpowers/specs/2026-06-08-weather-module-design.md)** —
  scope, alternatives considered, architecture, the module/engine boundary,
  v2 seasons seams, extension guide, testing, risks, and milestones.
- **[M1a plan](docs/superpowers/plans/2026-06-09-geography-crawler-core.md)** —
  the implemented crawler-core plan.

Feedback from GoMud community developers is welcome on the spec.

## Design highlights

- **Traveling weather systems** over a zone-adjacency graph (zone = node).
- **Biome ⇄ weather feedback loop** — terrain shapes passing fronts, not just
  the other way around.
- **Mutator-based application** — no prose hard-coded into the engine; builders
  own the voice via overridable emote tables (and a `tag-only` mode to drive
  their own room scripts).
- **Curated, overridable default buffs** — sensible mechanics out of the box,
  fully toggle-able.
- **Reproducible** — a pure, seeded simulation core (`sim/`) with no engine
  imports; deterministic given seed + world.
- **Zero GoMud engine changes for v1** — implemented entirely against existing
  APIs (see the spec's *Module boundary & core impact* section).
- **Works out of the box** — install, flip one flag, and a stock world has
  weather with no data authoring required.

## Layout

The module source lives at the repo root, with `go.mod` declaring the path
`github.com/GoMudEngine/GoMud/modules/weather` so the pure packages compile
identically here and inside a GoMud checkout's `modules/weather/`. The pure
packages (`sim/`, `crawler/`) are unit-tested standalone with
`go test ./sim/... ./crawler/...`; the engine-backed packages compile only
inside a checkout.

```
sim/               # PURE simulation/data core — no engine imports (Graph, cache, Neighbors)
crawler/           # geography crawler (zone adjacency) — pure, engine-agnostic Build
engine/            # engine adapter — the ONLY package importing internal/* (WorldReader, cache codec)
weather.go         # plugin registration, first-round graph build/persist, the `weather` command
weather_config.go  # module config (Enabled / IncludeSecretExits / RebuildGraphOnBoot)
files/             # config overlay (data-overlays/config.yaml)
```

(Future milestones add weather/climate/emote/buff data under `files/` and the
simulation tick in `sim/`.)

See the spec's *Architecture* section for the full breakdown and the three
sub-projects (crawler → simulation core → engine integration).

## Geography crawler

The crawler (`crawler/`) turns a MUD's zones into a weighted **zone-adjacency
graph** by walking room exits; the graph type and its JSON cache live in `sim/`.
Both packages are **engine-agnostic** — zero engine imports (enforced by an
architecture test), reading the world only through a small `WorldReader`
interface — so the identical code runs on **GoMud and DOGMud**; only the thin
`WorldReader` implementation differs per engine.

Two semantics worth knowing when consuming the graph:

- **Edge weight counts *directed* exits, not connections.** Each room-exit that
  crosses a zone border adds 1, so a normal two-way border reads `weight: 2`
  (one exit each way). Treat the weight as a "border width" proxy and halve it
  if you need a connection count.
- **Room→zone resolution assumes zones don't share room ids.** Each room id is
  mapped to a single zone; if the same id were reported under two different
  zones, the mapping (and a few edges) could vary between runs. Real worlds
  don't do this — `WorldReader.RoomIDs` returns disjoint sets per zone — so it's
  a non-issue in practice, noted only for `WorldReader` adapter authors.

## Installation (planned)

Once listed in the GoMud module registry:

```sh
go run . module install weather
go generate ./... && go build
```

Then set `Modules.weather.Enabled: true` (the shipped default) and run.
Engine prerequisites and graceful-degradation behavior are documented in the
spec; on engines missing required primitives the module fails soft with a
startup warning rather than crashing.

## Development

The pure core (`sim/`, `crawler/`) is tested standalone in this repo:
`go test ./sim/... ./crawler/...` (note: NOT `./...`, which now includes the
engine-coupled packages). The `engine/` and root `weather` packages import the
GoMud engine and compile only inside an upstream GoMud checkout. To build/test
them, sync the module into a checkout and build there:

    pwsh scripts/sync-to-checkout.ps1 -Checkout <path-to-GoMud-checkout>
    # then, in the checkout:
    go generate ./... && go build && go test ./modules/weather/...

The sync excludes this repo's `go.mod` (in-checkout modules have none). See
[CONTRIBUTING.md](CONTRIBUTING.md) for the module/engine boundary and the
DOGMud backport delta.

## License

[GPLv3](LICENSE), matching the GoMud engine.
