# Upstream PR kit 2 — module config overlay clobber + silent Set failures

Branch `fix/module-config-overlay-clobber` is already pushed to
GoMudEngine/GoMud. Open the PR here:
<https://github.com/GoMudEngine/GoMud/compare/master...fix/module-config-overlay-clobber>

## PR title

```
Fix module config overlay clobbering operator overrides; log dropped plugin config writes
```

## PR body (matches the repo's PR template: Description + Changes)

```markdown
## Description

Two related issues in the module-config write/merge path that together make
operator config silently disappear.

**1. `AddOverlayOverrides` wipes the operator's module config block.**
When a plugin loads, its data-overlay config keys are applied via
`configData.OverlayOverrides(newKeys)`, where `newKeys` is only the keys the
operator's `config-overrides.yaml` doesn't already set. Because `Modules` is
a plain `map[string]any`, the yaml unmarshal of
`{Modules:{<name>:{<newKeys only>}}}` replaces the module's inner map
wholesale — every operator-set key for that module vanishes from the live
config. The file is untouched, so this silently re-breaks on every boot.

Trigger: any world whose `config-overrides.yaml` has a partial
`Modules: <name>:` block (which `configs.SetVal` — i.e. any admin-page or
`server set` write — creates naturally) booting a module version that ships
any overlay key the block lacks. In other words: every module upgrade that
adds a config key. We hit this in the wild while releasing weather v0.2.0 —
the operator's `Enabled: true` became nil and the module booted disabled
with all settings discarded.

The function's own comment says "This ensures module defaults never clobber
operator-supplied values" — the intent is right, the implementation wasn't.
The fix applies the full merged union (operator values + new defaults), the
same proven approach `SetVal` already uses at runtime (`configs.go:365`).

**2. `PluginConfig.Set` discards `configs.SetVal`'s error.** Rejected or
failed module config writes (unregistered key, file write failure) produce
zero signal anywhere — the caller can't tell and nothing is logged. This PR
logs the error with the plugin and key names. Returning the error would be
the stronger fix, but that changes a module-facing API signature and would
break out-of-tree modules at compile time — happy to do that instead (or as
a follow-up) if you prefer.

## Changes

- `internal/configs/configs.go`: `AddOverlayOverrides` applies
  `configData.OverlayOverrides(Flatten(overrides))` (full union) instead of
  `OverlayOverrides(newKeys)`. The `Flatten` is load-bearing: at that point
  the in-memory `overrides` map is mixed-shape (nested yaml maps from the
  operator file + flat dotted keys added in the loop). The
  `len(newKeys) == 0` early return is preserved.
- `internal/plugins/pluginconfig.go`: `Set` logs `SetVal` errors via
  `mudlog.Error` (signature unchanged).
- `internal/configs/overrides_test.go`: regression test
  `TestAddOverlayOverridesPreservesOperatorOverrides` — simulates an
  operator file with a partial `Modules.weather` block plus a second
  module's block, applies a superset overlay, and asserts operator values
  survive, new keys get their defaults, and the sibling module is untouched.
  The test fails on current master (`Modules.weather.Enabled` → nil) and
  passes with the fix.

Re-confirmed against master @ 99305b2. The weather module currently ships a
boot-time self-heal working around issue 1 — once a release containing this
fix is the sensible minimum engine version, that workaround can be retired.
```
