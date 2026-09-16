# Plan: the review's findings, the struct, and the error dialog

What this does, in order, and why in that order. Each phase is one or
more commits, each commit reviewed adversarially before the next phase
starts. The reviews are read-only. The implementer does not commit; the
orchestrator commits once the review's real findings are fixed.

Sources: `review/README.md` and the five area files; `TODO.md`. A
finding is named as `<file> #<n>`.

## The order, and the reasoning

1. **Tests that lie come first.** Everything after this relies on the
   suite telling the truth, and three tests written this week cannot
   fail for the reason they name.
2. **Freezes next.** Two bugs stop the window dead. They are independent
   of the refactor and a user hits them.
3. **The error dialog before the refactor.** Two of the review's
   findings are errors nobody sees; the TODO already asks for a dialog
   that shows the whole message and lets it be copied. Build it once,
   then route those two errors through it. Everything after that has a
   proper place to report to.
4. **The four easy struct lifts.** Each group is touched by two files.
   Mechanical, low risk, and they take about twenty fields off `app`.
5. **`about(host)` before the two hard lifts.** The eleven host-kind
   decisions are the root cause. Collapsing them into one function is
   what makes moving `windows` and `machines` a relocation of one rule
   rather than of eleven.
6. **`windows`, then `machines`**, each as a type with its invariant
   stated and tested.
7. **The remaining bugs**, grouped by package.
8. **Duplication**, once the structure it lives in has settled.
9. **Test hygiene**, last, because the earlier phases will have changed
   what the helpers are used for.

## Phase 0 -- tests that cannot fail for the reason they name

`tests.md` #1, #2, #3, #6, #23.

- Restore the "proves nothing" guard in `TestAttachingShowsWhatIsAlreadyOnTheScreen`:
  at least one remote row for that window exists, and the attached one
  is not among them.
- `TestARowWithNoScreenCannotBeWatched`: assert the screened siblings
  are still offered, so an empty list cannot pass.
- The same test drives `revealRow`, the click, not `attachHere`.
- `newTestApp`'s `t.Skipf("no atlas")` becomes `Fatalf`. The atlas is
  built from an embedded font and has no environmental reason to fail.
- The four `t.Skip`s in `region_test.go` and `panel_test.go` that skip
  on a deterministic fixture become failures.

Done when: each changed test fails against the code it was written for
with the guard reverted, and passes with it.

## Phase 1 -- the two freezes

`network-layer.md` #1, #2.

- `remote/shell.go`: `Write` must not hold `writeMu` across the channel
  write. Model it on `serve/client.go:425-428`, which documents the
  hazard and avoids it. A test: a session whose far end stops reading,
  a `Write` parked on it, `Close` returns.
- `remote/shell.go:91`, `remote/files.go:42-76`, `remote/tunnel.go:278`:
  each channel and subsystem open is bounded by a context with a
  timeout, the way `Conn.reach` already is. The bound is one constant,
  named, with a comment saying what it is not for (a terminal's reads
  and writes, which must never time out).

Done when: a pane on a far end that has stopped reading can be closed;
a file pane on a host that accepts and says nothing fails with a
message rather than freezing the window.

## Phase 2 -- the error dialog, and the two errors nobody sees

`TODO.md`: "Error dialogs cut their text off", "cannot be selected or
copied", "should be red". `small-packages-and-errors.md` finding on
`fonts.go:50-58` and `ui/files/pane.go:251-256`.

- A `ui.Notice` (or an extension of `Form`) that shows the whole of a
  message, wrapping and scrolling rather than trimming; whose text can
  be selected with the mouse and copied with the window's copy chord;
  and whose title bar is drawn in the error colour when it reports a
  failure. `reportError` uses it.
- The font scan's failure goes to the user through it, once, when the
  scan finishes: which directory could not be read and why. The
  fallback -- carrying on with the fonts that were found -- is the one
  Marcus has approved by asking for this; the comment says so.
- A file pane whose read failed shows the whole error through the same
  dialog when the pane is activated, and its header row says "could not
  be read" rather than a truncated fragment. The old listing stays; the
  comment records that Marcus approved keeping it.

Done when: a long error is readable in full, selectable, copyable, red;
a font directory made unreadable produces a dialog; a file pane on a
path made unreadable does too.

## Phase 3 -- the four easy lifts off `app`

`main-package.md` #9. Each is one commit.

1. `agents`, `handedBy`, `handedNext` → a type in `agents.go`. Touched
   by `agents.go` and one read in `panel.go`.
2. `server`, `servePaths`, `served`, `lastSnapshot`, `openNow` → a type
   in `serving.go`. Touched by `serving.go` and `publish.go`.
3. `closing`, `closeMu`, `closeErrs` → a two-method type. Touched by
   `app.go` and `browse.go`.
4. `actOn`, `acting`, `menus` → a type in `hostmenu.go`. Touched by
   `hostmenu.go` and `machines.go`. Its doc says plainly that it exists
   because `ui.Command.Run` takes no argument.

Each type gets a constructor; `main.go` shrinks by the maps it was
filling. No behaviour changes. The suite is the check.

## Phase 4 -- one decision about what a host is

`main-package.md` #1, #5, #7; `tests.md` #12, #13.

- `about(host) hostKind` in one file, returning here / window / saved
  window / saved machine / connected machine / connecting / unknown.
  `hostAbout` in `hostmenu.go` becomes it.
- The eleven callers consume it: `openTerminalOn`, `connectSaved`,
  `savedWindowInRoute`, `takeOverSaved`, `workOnWindow`, `route`,
  `isHere`, `filesystem`, `hostOf`, `hostRow`, `hostAbout`. Then
  `openFilesOn`, `disconnectHere`, `openCommandHere`. `takeOverSaved`,
  `workOnWindow` and the body of `connectSaved` become one function.
- The split chooser (`split.go:92`) goes through it, and a terminal on
  a window lands in the split asked for: `openOnWindow` and the
  take-over path take a `*spot`.
- Case: one rule. The maps are keyed exactly; the book folds case.
  `about` says which applies where.
- A test walks every entry point that can open a terminal on a host --
  the plus menu, the palette, the split chooser, the Servers menu,
  "connect to a server" by address -- against a saved window, and each
  takes it over.

## Phase 5 -- the `windows` type

`main-package.md` #3, #4, #6, #9.1; `toolkit-and-rendering.md` #3.

- `windows`, `paneOnWindow`, `watching`, `knownWindowsAt`,
  `reachPatience` → a type with the invariant written at the top: one
  connection per address, keyed by a name that is re-derived from the
  book whenever the book changes. `renamedWindow` becomes a method;
  first-save and forget are handled, not only rename.
- `refreshServers`'s shortcut compares what the titles depend on, which
  includes whether a window is held.
- The held size gets its read side: the host's own row for a held pane
  says the size somebody else set; `Terminal.Box()` and `Held()` have a
  caller.
- `keysFor`, `reach`, `reachWindow`, `agentKeys` leave `windows.go` for
  `remote` (Phase 8 folds them into `authMethods`). `serveFiles` goes to
  `serving.go`; `windowFiles` to `browse.go`.

## Phase 6 -- the `machines` type

`main-package.md` #2, #9.4, #10.

- `machines`, `opening`, `paneOn` → a type with the invariant: a name is
  held by at most one of connected and connecting. `renamedMachine`
  becomes a method.
- `machineDied` updates both sources of truth, and a test covers it and
  `windowDied`. The two agree on policy: a machine or window that drops
  keeps a greyed row that can be cleared.
- `machines.go` splits as the review proposes: `machines.go`,
  `dialling.go`, `route.go`, `connect.go`, `rename.go`, `here.go`.

## Phase 7 -- the remaining bugs, by package

Each item is small; group them into a commit per package.

- **vfs / jobs** (`small-packages-and-errors.md` #1, #7, #8, #9, #14,
  #10, #11): the `Renamed` race; jobs releasing their cancel; `beside`
  and `Create` treating a stat error as absence; the half-applied
  chmod; `Same` by identity not name; `Roots` as a method.
- **session** (#2): the Windows close stall. With tests that run on
  Windows.
- **vt** (#3, #4, #6): the ASCII fast path and width passed once; the
  palette by pointer; `Render` honouring touched rows. Measure before
  and after with the flooder; write the numbers in the commit.
- **remote / serve / agent / mcp** (`network-layer.md` #3, #4, #5, #6,
  #7, #8): the MCP read bound; server text through `plainly()` on the
  pane path too; an unreadable key file reported; the attach check the
  comments promise; the close errors that hide something; the four
  disk fallbacks.
- **ui** (`toolkit-and-rendering.md` #1, #2, #4, #5, #6, #7, #9): the
  key bar's `^G`; Escape ordering; `Form` and `Chooser` declining
  modifiers and unwanted keys; Ctrl+K moved to Ctrl+Shift+K; `Field`
  sized by `Layout`; the atlas deallocating pages.
- **glyph / fonts** (`small-packages-and-errors.md` sweep): the three
  font fallbacks become one approved decision, reported through the
  Phase 2 dialog.

## Phase 8 -- duplication

`network-layer.md` #9; `toolkit-and-rendering.md` "Duplication";
`tests.md` #4.

- `keysFor` folds into `remote.authMethods`; the take-over path uses
  the same ladder; one agent timeout; four exports become unexported.
- `Menu` and `Palette` become a `List` with a row painter.
- One truncation helper, one colour mixer, one strip painter, one
  chord vocabulary.
- One `silentMachine`, in `internal/sshtest`.
- `remote` stops sniffing `session@gridterm` from an error string:
  `serve` exports the sentinel and `remote` checks it.

## Phase 9 -- test hygiene

`tests.md` #9, #10, #11, #15, #16, #17, #18, #19, #20, #21, #24, #26.

- One waiting helper with one budget. One dialog-opening helper.
- Every test of a user action starts at the click, the menu line, the
  button or the chord. The saved-window table gains the plus menu and
  the palette. The "whole path" tests go through their dialogs.
- `typeIntoField` everywhere fields are typed into; `answer` takes
  labels.
- `session` tested on Windows. `newTestApp` closes agents.
- The menu tripwire runs over `hostItems` for each shape.
- Source-reading tests use `go/ast`.
- The skips Phase 0 did not reach, same two shapes: `render/compositor_gpu_test.go:24,266`
  (an atlas that cannot fail), and `ui/form_test.go:535,633,704`,
  `ui/palette_test.go:575` (a fixture that decides). Found by Phase 0's
  reviewer; left for the phases that touch those packages.
- `TODO.md` and `review/` updated: a line goes when its work is in.

Done by 9ec8421 Make the tests start where the user does, with one way to wait.
Done by 70caa8c Make the dial tripwire see nested calls, and record what is only partly closed.

## What is not in this plan

From `TODO.md`, and deliberately later: scaling a held screen; the type
icon replacing the dot; folding and colouring the connection log;
removing the grey bar; "Files" connecting like "Terminal"; "Forget"
closing first; the serve dialog remembering its settings; copy progress
and repeat; draggable dividers. They are features, and they go on top
of a structure that has stopped moving.

## How each phase runs

1. An implementer (Opus) is given this file, the phase, and the area
   file it cites. It writes the code and the tests, runs `gofmt`,
   `go vet` for Linux and Windows, and the suite, and reports what it
   changed and what it could not do. It does not commit.
2. Two or three reviewers (read-only) attack the diff with separate
   briefs. They cannot edit.
3. The real findings are fixed; findings that are wrong are answered
   in the commit message.
4. Commit, in plain English, saying what the review found.
5. Mutation testing on the phase's new code where the phase adds logic
   rather than moving it.
