# The test suite

47,000 test lines against 37,000 of code. 1,631 tests. None uses
`t.Parallel`.

## Summary

The suite is green and the user's path was broken four times in one
day. The reviewer found the reasons, and they are the ones the owner
suspected: tests that call the function the author had in mind instead
of driving the path a user takes; assertions that cannot fail for the
reason the test's name gives; a guard that said "this proves nothing"
deleted when it became inconvenient; positional indexing that a new
field silently shifts.

The good news is that the suite already knows the rule. `clickPlus`,
`chooseMenuItem`, `pressButton`, `typeIntoField` and
`TestPanelEnterRevealsThroughTheList` -- with its comment "calling
revealRow by hand tests the function, not that anything is wired to it"
-- are exactly right. They are used in about a dozen places out of
sixteen hundred.

Tags: [bypass] calls below the user's action; [passes-with-bug] cannot
fail for the stated reason; [brittle]; [flaky]; [source-reading];
[coverage]; [helpers].

## Findings, most serious first

### 1. A "this proves nothing" guard was deleted, and the assertion it protected is now unfalsifiable [passes-with-bug]

`takeover_test.go:684-689`, in `TestAttachingShowsWhatIsAlreadyOnTheScreen`.

Commit `0a4c7e6` removed `if mine == 0 { t.Error("the window over there
lost every row, so this proves nothing") }` and the counter, leaving a
bare loop that fails only on a match. It passes when the client lists
*no* remote rows at all -- which is precisely the failure mode of the
commit that removed the guard ("show only the screens"). Restore a
count: assert at least one remote row for that window exists and that
the attached one is not among them.

Closed by 4c44b5a Make six tests able to fail for the reason they name.

### 2. The same shape, same commit, in the test that guards a real regression [passes-with-bug]

`takeover_test.go:1764-1768`, `TestARowWithNoScreenCannotBeWatched`.

"It is not offered" iterates the remote rows and fails only on a match;
an empty slice passes. It needs a non-empty assertion plus proof that
the screened siblings are still offered, or it cannot tell "filtered
correctly" from "listed nothing".

Closed by 4c44b5a Make six tests able to fail for the reason they name.

### 3. The test for "clicking a screenless row opened a pane" no longer clicks [bypass]

`takeover_test.go:1773`, `:1784`.

The test calls `client.attachHere(serving, nil)`. Before `0a4c7e6` it
called `client.revealRow(serving)`, which is what the list's
`OnActivate` runs -- the user's click -- and which has its own
`remoteKey` branch. The bug being pinned was "clicking the row opened a
pane"; the test now runs one layer below the click. `revealRow` is
exercised only twice elsewhere, neither time with a `remoteKey`.

Closed by 4c44b5a Make six tests able to fail for the reason they name.

### 4. `silentMachine` in `serve` still has the send-on-closed-channel panic fixed in the root copy [flaky]

`serve/takeover_test.go:1381-1404` against the fixed twin at
`takeover_test.go:1351`.

Cleanup closes the listener and then the channel while the accept
goroutine may be blocked sending on it. A connection accepted in that
window panics the whole binary; a ninth connection blocks the goroutine
for ever. The root copy was converted to a mutex and a slice with a
comment explaining exactly this. The duplicate was not.

Closed by 1707651 Fold the duplicated pieces the review named into one of each.

### 5. `silentMachine(t)` is called from inside `go func()` [flaky]

`serve/takeover_test.go:1423`, `:1453`.

It calls `t.Helper`, `t.Fatalf` and `t.Cleanup` from a non-test
goroutine. `Fatalf` there only ends that goroutine, so the test hangs
on its 30-second select and reports the wrong thing; registering a
cleanup races test completion. Hoist the call above the goroutine.

Closed by 1707651 Fold the duplicated pieces the review named into one of each.

### 6. One font-atlas failure turns all 937 root tests green by skipping [passes-with-bug]

`panes_test.go:190` (`newTestApp`) and `:1289`: `t.Skipf("no atlas")`.

Every root test builds a `testApp`. A broken `glyph.NewAtlas` -- the
embedded font, a DPI change, a new metrics assertion -- silently turns
the entire suite to SKIP, and `go test ./...` still prints `ok`. The
atlas is built from an embedded font and has no environmental reason to
fail. This should be `Fatalf`.

Closed by 4c44b5a Make six tests able to fail for the reason they name.

### 7. The key-bar layout is asserted against the function that implements it [passes-with-bug]

`ui/files/files_test.go:826-850`.

`keyAt` is a loop over `keyCell`, and the test checks `keyAt(col)` lies
inside `keyCell(i)` -- true by construction for any arithmetic. Replace
with the drawn row: find the column where the chord's label appears and
require a click there to run that key. That test would have caught
finding 1 of the toolkit review, where the bar advertises a key its
click table does not know.

### 8. Clicks on the key bar are aimed with the production layout maths [brittle] [passes-with-bug]

`ui/files/files_test.go:771`, `:839`, `:966-967`, `:1809`.

The click column is computed with `keyCell`, so if `Draw` and `keyCell`
disagree the click still lands where the code thinks the key is.
`:806` clicks a hard-coded column and asserts nothing happened "because
nothing is wired to Rename"; add a key or reorder the bar and that
column becomes a wired key and the test quietly changes meaning.

### 9. "The whole path a user takes", with the dialog skipped [bypass]

`takeover_test.go:81-97`: the comment says "from its own dialog"; the
body calls `client.takeOver(addr, keyFile)`. The dialog's address
parsing, default-port join and key-file field are not on the tested
path. `servers_test.go:91-99` has the same mismatch. Use `openTakeOver`
plus `typeIntoField` plus `pressButton`, as `TestOpenServerRejectsABadTarget`
already does for the failure case.

Closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 10. Four ways to reach a saved window are tested; none is a way a user asks [bypass]

`book_test.go:1157-1190`.

The table drives `connectAs`, `connect`, `openTerminalOn` and
`connectSaved`. The bug it commemorates was in the *menu* wiring, and
the only click-through version is a separate test. Add the plus menu
and the palette command as fifth and sixth cases -- the two entry
points that have actually been wrong.

Closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 11. Most plus-menu lines are asserted as offered and never chosen [coverage] [bypass]

`chooseMenuItem` is used at three sites; `clickPlus` at six.
`conn.files`, `conn.command`, `conn.tunnel`, `conn.socks`,
`server.editThis` and `server.forget` are never *run* from a menu.
`forget_test.go:18` is titled "can be edited and forgotten from its own
plus menu" and checks only `offers(menu, "server.forget")`.

Closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 12. `openFilesOn` has the two-switch shape that caused the last four bugs, and nothing drives it from the UI [bypass] [coverage]

`browse.go:34-42`. `openFilesOn` tests `a.savedWindows[host]` directly
while `openTerminalOn` goes through `savedWindow()` and handles every
kind in one switch. Every file-manager test calls `openFilesOn`
directly, so the `conn.files` line on a window's plus menu is never
exercised end to end. **Speculation**: the failure is one branch from
the one fixed in `c9186f2`.

For the two-switch shape: closed by d96de72 Decide what kind of host a name is in one place.
For the coverage: closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 13. `TestTheHereCommandsDelegate` promises the here-commands and checks one [source-reading]

`hostmenu_test.go:378-403` asserts only about `openTerminalHere`.
`disconnectHere` and `openCommandHere` still contain their own
`isHere`/`isWindow` switches -- the shape the test exists to forbid. It
also hard-codes the filename, so moving the function fails with "is
gone" rather than a useful message.

Closed by d96de72 Decide what kind of host a name is in one place.

### 14. Source-reading tests: defensible, but only as a second line [source-reading]

`book_test.go:1235`, `hostmenu_test.go:378`. Verdict: **not a smell.**
They encode an invariant ("one place dials") that no runtime assertion
can express, because the point is the caller the author did not think
of. But they are substring greps: `callersOf` misses a wrapper, an
alias or a method value, and skips only whole-line comments. Replace
the string matching with `go/ast` and keep the same assertions.

Partly closed by 70caa8c Make the dial tripwire see nested calls, and record what is only partly closed:
`callersOf` reads the syntax tree, so a method value and a name behind a
chained call both count, and one in a comment or a string does not. The
verdict itself stands: a source-reading test is a second line, not the
first.

### 15. A whole package is untested on the platform it is developed on [coverage]

`session/local_test.go:1` is `//go:build !windows`. On Windows -- the
primary target -- `session` has zero tests; `go test ./...` prints no
`ok` line for it and nobody notices. `detach_windows.go` and
`hangup_windows.go` have no test on any platform. The small-packages
review found a bug in exactly that code.

### 16. `waitUntil` gives two seconds where every other wait gives thirty [flaky]

`panes_test.go:554`, 26 call sites, all waiting on a pipe-session
goroutine. Under `-race` on a loaded machine this is the most likely
source of intermittent failures in the root package. Fold it into
`waitFor`'s budget.

Closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 17. Handed-over panes leak a listener past the end of the test [flaky]

`newTestApp`'s cleanup never calls `closeAgents`. Any test that hands a
pane over and does not take it back leaves a real TCP listener and its
accept goroutine running for the rest of the binary (`agents_test.go`
has about ten). This, not the mutated code, is the likelier cause of
that test's intermittent failure.

Closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 18. Positional field indexing in app dialogs [brittle]

`takeover_test.go:1537-1538`, `servers_test.go:89`, `browse_test.go:362`,
`:371`, `ask_test.go:132`, `:310`, `:313`. This is the failure the owner
already hit; `typeIntoField` was written to fix it, with the comment "by
label rather than by position", and these six sites do not use it.

Closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 19. `answer(…, values...)` is positional typing wearing a helper's name [helpers] [brittle]

`ask_test.go:59-72` types values into whatever has focus, tabbing
between them, never checking a label. Adding a field to the
keyboard-interactive dialog silently swaps the answers. Make it take
label and value pairs and delegate to `typeIntoField`.

Closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 20. Twelve waiting helpers, three budgets, two ways to open a dialog [helpers]

Root package alone: `waitFor`, `waitForBoth`, `waitUntil`,
`waitUntilPumped`, `waitForPanes`, `waitForDialog`,
`waitForDialogPrefix`, `waitForConnecting`, `waitForFailure`,
`openDialog`, `fromAgent`, `within`. `waitFor` exists three more times
with different signatures in other packages. `waitUntilPumped` and
`waitFor` are the same function with the arguments swapped. Six
open-coded `net.SplitHostPort` calls beside `hostOf`/`portOf`. Four
key-making helpers.

Closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 21. Package variables mutated by tests with no parallel protection [brittle]

`remote/hang_test.go:62-64`, `:286-288` assign `helloTimeout`. Safe only
because nothing calls `t.Parallel`; the first one added to `remote`
makes two tests wrong in a way that looks like a flake. Same exposure
for `agentListing` and `agentGrace`.

### 22. An assertion sixteen times looser than the thing it names [passes-with-bug]

`remote/hang_test.go:86`: with the timeout set to 300 ms, the test
fails only past five seconds. Assert against a small multiple of the
timeout.

### 23. `t.Skip` where the fixture, not the environment, decides [passes-with-bug]

`region_test.go:427`, `:476`, `:487`; `panel_test.go:718`. The
condition is deterministic given the embedded font, so these either
always run or always skip; a metrics change turns them into silent
no-ops, and `:487` skips on exactly the regression it should catch.

Closed by 4c44b5a Make six tests able to fail for the reason they name.

### 24. The menu tripwire does not cover the plus menu [coverage]

`menus_test.go:30` walks the menu bar only. `hostItems` builds five
menus by shape, and a command id there that is never registered draws
greyed out with no test noticing.

Closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 25. An unfalsifiable guard in the wrong-code test [passes-with-bug]

`agents_test.go:419-422`: `swap` is `"z"` unless the code ends in it,
so `wrong != code` always holds and the guard can never fire. Dead
scaffolding that reads as protection. A deterministic table of
malformed codes would remove the question.

### 26. Positional row indexing on the sidebar [brittle]

`panel_test.go:363`, `:594` take `Rows()[1]` as "the first pane";
`browse_test.go:448` asserts `Panes()[3]`. Adding a heading or a pinned
row moves all of them. `serverRow` in `machines_gaps_test.go:31` is the
pattern to use.

Closed by 9ec8421 Make the tests start where the user does, with one way to wait.

### 27. Coverage shape

- **No test starts at the UI for:** browse files on a machine, run a
  command on a machine, open or close a tunnel or proxy from the plus,
  forget or edit a server from the plus, "Serve this window" from its
  dialog, hand a pane to an agent from the menu bar.
- **`ui` at 9,900 test lines for 5,900 of code is depth, not
  repetition.** `ui/ui_test.go` is almost entirely the root's
  mouse-capture and modal-stack state machine, each test a distinct
  transition. The menu-bar contract and menu layout tests assert drawn
  cells and idle-frame dirtiness -- the strongest material in the repo.
- **Thin relative to complexity:** `vt` has about fifty tests, one
  escape sequence each, and none covers a sequence split across two
  writes, which is the classic emulator bug. `input/ebitenin` has no
  tests. `internal/sshtest` is 437 lines of load-bearing fake with no
  tests of its own; a bug there makes every SSH test lie.

## What is sound

- **The in-process SSH server**, with the host key pinned, the agent
  off and a written-out identity: tests never touch the developer's
  agent, `~/.ssh` or `known_hosts`. `Conns()`, `Forwards()` and `Live()`
  let a test assert *how many logins* happened, which is what catches
  "a second terminal opened a second connection".
- **Pump-driven waiting.** `waitFor` runs the real frame loop while it
  waits, so nothing is tested in a state the program never reaches, and
  the failure names what was awaited. The right primitive; it needs to
  be the only one.
- **Injected clocks.** The sidebar's active → settled → closed fading is
  tested by arithmetic, not by sleeping. Four `time.Sleep`s outside
  polling loops in 47,000 lines.
- **Test-owned state everywhere:** a temp server list, a temp
  known-windows file, a clipboard that writes into the test app, each
  with a comment naming the user's real file it protects.
- **`checkTree`** as a universal invariant -- panes in the tree equal
  panes in the app, exactly one focused leaf, none while a modal is up.
  The single best thing in the suite. Extend it to the registry (every
  open pane has exactly one row) and several findings above become
  unnecessary.
- **`clickPlus`, `chooseMenuItem`, `TestPanelEnterRevealsThroughTheList`,
  `TestEveryMenuLineNamesACommand`.** The author already knows the rule.
  It is applied in a dozen places; it should be the default.
