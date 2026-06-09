# Contributing

Thanks for helping build the GoMud Weather Module. This file captures the
working agreements that keep the project healthy; the
[design spec](docs/superpowers/specs/2026-06-08-weather-module-design.md) is the
authoritative source for *what* we're building.

## Ownership boundary: module vs. GoMud engine

We own the **module**. Volte6 / the GoMud maintainers own the **engine**
(`internal/**`) and the module **registry**. This boundary drives how we work:

| Change type | Owner | Review path |
|---|---|---|
| Module code & data (`module/weather/**`) | Us | This repo's normal review. |
| Any GoMud engine change (`internal/**`) | GoMud | Upstream PR + maintainer review. |
| Registry listing | GoMud maintainers | One-time onboarding. |
| DOGMud adapter fixes (on API drift) | Us | DOGMud repo review. |

**Guiding principle: keep the engine PR queue empty.** v1 is designed to require
**zero** engine changes — everything runs against existing mutator, event, and
data-overlay APIs. If a feature *seems* to need a core change:

1. First try to express it through existing mechanisms behind the `engine/`
   adapter (mutators, events, the data overlay, exported functions).
2. Only if there is genuinely no module-side path, propose an upstream change —
   and present it as **additive and backward-compatible**, never a hard
   dependency for the module to function.

See the spec's *GoMud core impact & module boundary* section for the full
capability-by-capability classification.

## Out-of-the-box experience is a requirement

A module that needs hand-holding to start doesn't get adopted. Every change must
preserve the OOBE bar (spec §2.3):

- Install + `Enabled: true` → working weather on a **stock** world.
- **No required data authoring** and **no world prep** — defaults ship for the
  standard biomes; the crawler discovers geography from existing exits.
- Graceful degradation, never a crash. Disabling cleanly self-heals rooms.

If your change adds a knob, it must have a sensible default that keeps OOBE true.

## Architecture rules

- **`sim/` stays pure.** No `internal/*` imports in the simulation core — an
  architecture test enforces this. All engine access goes through `engine/`.
- **All engine-specific calls live in `engine/`.** This is what makes the module
  portable across GoMud and DOGMud; a future API drift should be a localized
  patch, not a redesign.
- **Determinism.** The sim is driven by a seeded RNG; keep `sim.Step` a pure
  function of its inputs so runs stay reproducible and testable.

## Development workflow

This module only compiles inside a GoMud checkout:

1. Place `module/weather/` under a checkout's `modules/weather/`.
2. `go generate ./... && go build`.
3. Run the server; exercise via admin commands and/or the playtest harness.

Prefer test-first where practical — the crawler and `sim/` are designed to be
unit-tested without a running server (via fake `WorldView` fixtures).

## Commits & reviews

- Keep module and any (rare) engine changes in **separate** PRs to separate
  repos — never mix a `module/weather/**` change with an `internal/**` change.
- Reference the relevant spec section in non-trivial PRs.
