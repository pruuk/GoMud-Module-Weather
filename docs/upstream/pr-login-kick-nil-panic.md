# Upstream PR kit 1 — login-kick nil panic

Branch `fix/login-kick-nil-panic` is already pushed to GoMudEngine/GoMud.
Open the PR here:
<https://github.com/GoMudEngine/GoMud/compare/master...fix/login-kick-nil-panic>

## PR title

```
Fix nil panic when kicking a link-dead session at login
```

## PR body (matches the repo's PR template: Description + Changes)

```markdown
## Description

Logging in as an existing user and answering `y` to the "kick the existing
session?" prompt panics the server if that session has disappeared between
the prompt and the confirmation — `users.GetByUserId` returns `nil` when the
user is no longer in the active user map, and `FinalizeLoginOrCreate` called
`user.ConnectionId()` on it unconditionally.

Reproduce: kill your client (don't quit), reconnect quickly as the same
user, answer `y` to the kick prompt while the old session is being reaped as
link-dead → nil-pointer panic.

The prompt's `Condition` does check `user != nil` at prompt time
(`login.go:193-197`), but nothing re-checks at finalize time, which is the
race window.

## Changes

- `internal/inputhandlers/login.go`: only perform the kick (goodbye
  message, `SetLinkDeadUser`, `Kick`) when `GetByUserId` returns a live
  user; when it returns `nil` there is no session left to kick, so login
  proceeds normally.
- The same guard covers the `FindUserId` → `0` lookup-failure case, since
  `GetByUserId(0)` returns `nil`.

Found while developing the weather module (reported in Discord 2026-06-10);
re-confirmed against master @ 99305b2.
```
