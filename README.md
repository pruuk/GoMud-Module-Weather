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

**Design phase.** No code yet. The full design is the source of truth for what
we're building and why:

- **[Design spec](docs/superpowers/specs/2026-06-08-weather-module-design.md)** —
  scope, alternatives considered, architecture, the module/engine boundary,
  v2 seasons seams, extension guide, testing, risks, and milestones.

Feedback from GoMud community developers is welcome on the spec before
implementation begins.

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

## Planned layout

The module source lives at the repo root, with `go.mod` declaring the path
`github.com/GoMudEngine/GoMud/modules/weather` so the pure packages compile
identically here and inside a GoMud checkout's `modules/weather/`. The pure
packages (`sim/`, `crawler/`) are unit-tested standalone with `go test ./...`;
the engine-backed packages compile only inside a checkout.

```
sim/        # PURE simulation/data core — no engine imports (Graph, etc.)
crawler/    # geography crawler (zone adjacency) — pure, engine-agnostic Build
engine/     # (next chunk) the ONLY package importing internal/rooms,/mutators,/events
weather.go  # (next chunk) plugin registration + wiring
files/      # (next chunk) config overlay + climate/weather/mutator/buff/emote data
```

See the spec's *Architecture* section for the full breakdown and the three
sub-projects (crawler → simulation core → engine integration).

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

The pure core (`sim/`, `crawler/`) is developed and tested standalone in this
repo: `go test ./...`. The engine-backed reader, plugin registration, and admin
commands (next milestone) compile only inside a GoMud checkout — develop those
by syncing the module source into a checkout's `modules/weather/` (without this
repo's `go.mod`, which must not travel), then `go generate ./... && go build`.
See [CONTRIBUTING.md](CONTRIBUTING.md) for the module/engine boundary.

## License

[GPLv3](LICENSE), matching the GoMud engine.
