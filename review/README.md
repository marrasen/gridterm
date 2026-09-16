# Review of the codebase, 15 September 2026

Five read-only reviews, run in parallel over the whole repository after
one bug came back four times in a day. Each reviewer had a brief and
could not edit anything. Where a finding could be checked cheaply, it
was checked before being written down, and the file says so.

| file | covers |
|---|---|
| [main-package.md](main-package.md) | `package main`: structure, duplication, stale state, the 81-field `app` |
| [network-layer.md](network-layer.md) | `remote`, `serve`, `agent`, `mcp`: concurrency, bounds, security, close discipline |
| [toolkit-and-rendering.md](toolkit-and-rendering.md) | `ui`, `ui/files`, `ui/term`, `render`, `grid`, `glyph`: contracts, damage tracking, duplication |
| [tests.md](tests.md) | the 47,000-line suite: what it cannot catch, and why |
| [small-packages-and-errors.md](small-packages-and-errors.md) | `vfs`, `jobs`, `vt`, `session`, `input`, `conns`, `meter`; and every swallowed error in the repo |

## How a finding is marked closed

A finding gets a "Closed by `<hash>` `<subject>`" line under it only when
every part of it is closed. A finding whose work is half done gets
"Partly closed by ..." instead, with what is still open named in the same
line. Nothing outstanding is allowed to read as finished.

## The answer to "is something rotten?"

Yes, in one place, and it is the place the bugs came from. The main
package has **eleven separate functions that each decide what kind of
host a name is** -- a machine, a gridterm window, this machine, a saved
server, one still connecting -- each with its own mix of checks. A new
rule has eleven places it could go and no place it must go. That is
what four rounds of one bug looks like from the inside. The same
package keeps three pieces of **derived state that mirror something else
and are refreshed on some events but not all** (`savedWindows`,
`serverHosts`, the `windows` map keyed by a name it never re-derives),
and each has a live bug in the main-package file.

The rest is not rotten. The network layer, the toolkit contracts, the
compositor's damage tracking and the error discipline in `vfs` and
`remote.Book` are careful, correct and explained. Their problems are
specific bugs and a body of duplication that has begun to diverge.

The test suite is the second cause. It is large and in places excellent,
but it was green through all four rounds, because its tests call the
function the author had in mind instead of driving the path a user
takes. The suite already knows the right pattern (`clickPlus`,
`chooseMenuItem`, "calling revealRow by hand tests the function, not
that anything is wired to it"). It uses it in about a dozen of sixteen
hundred tests.

## Objective numbers

- 36,000 lines of code, 46,000 of tests, 1,631 tests, none parallel.
- `package main`: 10,046 lines, flat, twenty files. `app` has **81
  fields** in ten concerns. `machines.go` is 1,048 lines with twelve
  responsibilities.
- Longest functions: `main()` 212 lines, `openRoute` 141,
  `openServerForm` 127, `authMethods` 118, `commands()` 117.
- Linter: 82 issues. Of 50 unchecked errors, 10 are in code (7 outside
  the test harness, all defensible discards on cleanup paths) and 40 in
  tests. 12 unused symbols, including the four sidebar type icons.
- Explicit discards (`_ =`) in code: 125. Defensible 121, want a comment
  4, violation 1. No `recover()` anywhere. No empty error branch.
- House-rule violations found repo-wide: **six**, listed in
  `small-packages-and-errors.md`. Three are one decision ("a bad font
  file should not take the window down") made in three places.

## Fix first

Ranked across all five files by what a user hits and how badly.

1. **A pane whose program stops reading its input cannot be closed, and
   the window freezes trying.** `remote/shell.go:181-186`: a lock held
   across a blocking SSH write that `Close` also needs. Verified. The
   `serve` package documents this exact hazard and avoids it.
   Closed by 0b03bf8 Stop the window freezing on a machine that has stopped answering.
2. **Opening a file pane, tunnel or shell on a host that is alive but
   not answering freezes the whole window.** `remote/shell.go:91`,
   `remote/files.go:42`, `remote/tunnel.go:278`: channel opens on the
   drawing goroutine with no bound.
   Closed by 0b03bf8 Stop the window freezing on a machine that has stopped answering.
3. **The split chooser cannot open a terminal on a saved window.**
   `split.go:92` calls `openOn` directly and is refused by the route
   guard. Verified: `openOn` has four callers and one is guarded. The
   fifth copy of the bug fixed four times today.
   Closed by d96de72 Decide what kind of host a name is in one place.
4. **A connection that drops on its own leaves a blank heading nothing
   can clear.** `machines.go:106-139` updates one of two sources of
   truth. Untested; `windowDied` does the opposite.
   Closed by 3fa837a Give the connected machines a type and split machines.go six ways.
5. **"^G Go to" on the file browser's bar is dim and its click does
   nothing.** `ui/files/browser.go:388-407`, `:552-579`. Verified. Added
   today; the test checked the bar listed it and the pane took the key,
   and never clicked the cell.
   Closed by 0320a9c Give dialogs one key rule, tell the user why a connection went.
6. **A data race on `SFTP.Renamed`** (`vfs/sftp.go:32,39`), written on
   the drawing goroutine and read on every filesystem goroutine. Added
   today.
   Closed by 0d20f84 Stop a copy overwriting a file it could not check.
7. **Every tab close on Windows stalls 250 ms and kills a dead child.**
   `session/local.go:111-120`. `session` has no tests on Windows, the
   primary target.
   Closed by 290911e Stop every tab close on Windows waiting a quarter of a second.
8. **`jobs/run.go:361` overwrites the user's file when `Stat` fails**
   for any reason other than not-found. The sharpest house-rule
   violation.
   Closed by 0d20f84 Stop a copy overwriting a file it could not check.
9. **The MCP client has no read bound**, so a window that stops
   answering parks a tool call for ever and the process never exits.
   Partly closed by c788cb0 Filter the far end's text everywhere, bound the agent, report the rest: the agent client sets a deadline on every
   exchange. `mcp.Serve` still bounds no read and still waits on
   `running.Wait()`, which its own comment now argues for.
10. **A hostile server can rewrite the connection log**, including "its
    host key is accepted", because its text goes into the pane's
    terminal unfiltered while the console path strips it.
    Closed by c788cb0 Filter the far end's text everywhere, bound the agent, report the rest.
11. **Three tests written today cannot fail for the reason they name**
    (`takeover_test.go:684`, `:1764`, `:1773`) -- a deleted "proves
    nothing" guard, an empty-slice pass, and a click replaced by a
    direct call. Fix these before fixing anything they claim to cover.
    Closed by 4c44b5a Make six tests able to fail for the reason they name.
12. **One font-atlas failure skips all 937 root tests and `go test`
    still prints ok.** `panes_test.go:190` should be `Fatalf`.
    Closed by 4c44b5a Make six tests able to fail for the reason they name.

## The structural work

Not bugs; the changes that stop the bugs recurring. In the order that
pays back soonest.

1. **One `about(host)` function** returning a small struct -- here,
   window, saved window, machine, connecting -- and eleven callers
   consuming it. `hostAbout` in `hostmenu.go` is already most of it.
   Closed by d96de72 Decide what kind of host a name is in one place.
2. **Move the dialling ladder out of `windows.go` into `remote`.**
   `keysFor` re-implements `authMethods` with different timeouts and a
   copied message; four exports exist only to feed it.
   Closed by 1707651 Fold the duplicated pieces the review named into one of each.
3. **Give `windows`, `serving` and `agents` their own types** with their
   own invariants. The `windows` invariant -- one connection per address,
   keyed by a name that is re-derived when the book changes -- is
   unstated today, and findings 3 and 4 of the main-package file are
   both it.
   Closed by 4bc67cc Give the taken-over windows a type with its invariant written down, and by 6350ed0 Lift four groups of fields off the app struct into their own types for the four easy lifts.
4. **Split `machines.go` and `windows.go`** along the lines given in the
   main-package file. `rename.go` in particular: the rename fan-out is
   one operation spread over three files, and putting it in one is how
   the next fan-out stops forgetting a map.
   For `machines.go`: closed by 3fa837a Give the connected machines a type and split machines.go six ways. For `windows.go`: closed by 4bc67cc Give the taken-over windows a type with its invariant written down
   and 1707651 Fold the duplicated pieces the review named into one of each.
5. **Make the test rule the default, not the exception.** Every test of
   a user action starts at the click, the menu line, the button or the
   chord. Fold the twelve waiting helpers into one. Extend `checkTree`
   to the registry.
   Partly closed by 9ec8421 Make the tests start where the user does, with one way to wait:
   the tests start at the click and there is one waiting helper.
   `checkTree` still says nothing about the registry.
6. **Fold the duplicated widget logic**: three list state machines into
   one, five truncation helpers into one, two colour mixers into one,
   the two `silentMachine`s into one.
   Closed by 1707651 Fold the duplicated pieces the review named into one of each.

## Decisions that are Marcus's, not the code's

The house rule reserves fallbacks for Marcus to approve explicitly.
These exist and have not been approved:

- **A font file that cannot be read or parsed is skipped**, and the
  window carries on with fewer fonts (`fonts.go:50-58`,
  `glyph/scan.go:227-229`, `glyph/fallback.go:101-105`). The comments
  argue the case well. Say yes once and the code should record that;
  say no and three sites change.
  Closed by b00f97e Make the font fallback one decision, reported once.
- **A file pane keeps the previous listing when a read fails**, showing
  the error beside it (`ui/files/pane.go:251-256`). Deliberate and
  commented; the user is acting on names the filesystem refused to
  vouch for.

## What is sound, in one place

Worth stating so that a refactor knows what not to touch.

- The pump discipline: one rule, stated once, followed everywhere.
- `remote.Book`: the model the rest should be held to.
- Host key handling, one implementation for both protocols, and the
  rule that trust is never offered while any known_hosts line failed to
  parse.
- The take-over protocol's authentication.
- Timer races handled with a generation counter, which `Stop`'s return
  value cannot express.
- `ui/widget.go`'s contracts, the cursor-claim rule, the compositor's
  idle path, `ui/buffer.go`, and the screen-sharing ordering argument.
- `vfs`'s error discipline and `jobs.file`'s write-then-rename.
- The in-process SSH server, pump-driven waiting, injected clocks, and
  `checkTree`.
- The comments. They say what breaks and when, not what the code does,
  and several are scar tissue from exactly these bugs. They are why five
  reviewers could be this specific.
