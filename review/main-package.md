# The main package

`package main`: 20 files, about 10,000 lines, all at the repo root. This is
the part Marcus suspected, and the suspicion is right. The mechanism is
specific and is stated in one sentence in the summary below.

Every finding was verified by reading the code and tracing callers with
`grep`. Anything not verified is marked **speculation**.

## Summary

**There are eleven separate places that decide "what kind of host is
this?", and every one of them was written by hand.** A machine to log in
to, a gridterm window to take over, this machine, a saved server, one
still connecting: eleven functions each ask some of those questions with
their own mix of `isHere`, `isWindow`, `savedWindow`, `book.Lookup` and
map probes. Four rounds of one bug is what that produces. A new rule has
eleven places it could go and no single place it must go.

The second cause is **derived state that mirrors something else and is
refreshed on some events but not all**. `savedWindows` mirrors the book;
`serverHosts` mirrors the set of commands; `windows` is keyed by a name
derived from the book at one moment and never re-derived. Each has a bug
below.

What is *not* rotten: the threading rule, the pump, the registry as the
one list, and the comments. See the last section.

## Objective numbers

- `app` struct: **81 fields** (`app.go:54-357`), in ten concerns.
- Longest functions: `main()` 212 lines, `openRoute` 141,
  `openServerForm` 127, `commands()` 117, `refreshServers` 103,
  `takeOver` 95, `refreshPanel` 95.
- `machines.go` is 1,048 lines with twelve responsibilities;
  `windows.go` is 835 with eight, two of which do not belong in the file.
- Linter (`golangci-lint`): 82 issues repo-wide; of the 50 unchecked
  errors, 10 are in code and 40 in tests. The four in this layer's
  reach are all defensible discards on cleanup paths and want an
  explicit `_ =` or a comment, not a fix. The four type icons in
  `panel.go:32-35` are defined and never drawn.

## Findings, most serious first

### 1. The split chooser opens a terminal on a window by logging in to it

`split.go:84-94`, specifically `split.go:92`. **Live bug, reachable
today.** Verified: `openOn` has four callers (`book.go:166`,
`book.go:254`, `machines.go:990`, `split.go:92`) and only the first is
behind the window check in `openTerminalOn`.

`addSplitChoices` loops every host and calls `a.openOn(name, nil, at)`
straight. `openOn` builds a route, and `route()` now refuses a window
(`machines.go:882`). So Ctrl+Shift+D, then "Terminal on <a saved
window>", fails with *"X is a gridterm window, which is taken over
rather than logged in to."* A window taken over by bare address fails
differently: *"there is no saved server called 10.0.0.5:2222"*.

This is the fifth copy of the bug fixed in `c9186f2`. The fix went into
`openTerminalOn`; this caller never went through it.

Related: `openTerminalOn` takes no `*spot`, and every take-over path
passes `nil`. **A terminal on a window can never land in the split the
user asked for.** It always becomes a tab.

Closed by d96de72 Decide what kind of host a name is in one place.

### 2. A connection that drops on its own leaves a heading nobody can act on

`machines.go:106-139` (`machineDied`) against `panel.go:261-266` and
`panel.go:359-372` (`hostRow`).

`machineDied`'s comment says the row stays, greyed, so what the
connection did can be read. It does not. The dot and the note on a
machine's heading are drawn from `a.machines[host]`, which `machineDied`
has just deleted; and `refreshPanel` skips every `Server` row on the
assumption that the heading carries it. Net: a blank heading, a greyed
row that is never drawn, and a `Close` closure (`machines.go:130-134`)
nothing can reach. The user sees a machine with no dot and no way to
clear it except "clear finished".

Two sources of truth for one fact -- `a.machines` and the registry --
and `machineDied` updates one. `dropMachine` updates both, which is why
that path looks fine.

`windowDied` (`windows.go:521`) does the opposite: it drops the row
entirely. Same event, opposite policy.

**No test covers `machineDied` or `windowDied`.** Zero hits in `*_test.go`.

Closed by 3fa837a Give the connected machines a type and split machines.go six ways.

### 3. Command titles drift from what is connected

`book.go:57-67` (the early return) against `book.go:79-86` (the title).

`refreshServers` skips rebuilding when the list of names and kinds has
not changed. The command title also depends on `a.windows[host]`, which
is not in that comparison. So a saved window reads "Take over X"; the
user takes it over; nothing in the comparison changed; the palette and
the Servers menu say "Take over X" for the rest of the session. Letting
go leaves it saying "Open a terminal on X".

Running the command still does the right thing, so this is a lying
label rather than a broken action. It is the same class of fault the
comment at `book.go:36-39` was written to fix for `savedWindows`, one
field over.

Closed by 4bc67cc Give the taken-over windows a type with its invariant written down.

### 4. Saving a window that is already taken over makes it unreachable under its new name

`windows.go:87-112` (`takeOver`), `windows.go:203-215` (`windowNamed`),
`book.go:369-379` (the Save guards).

`a.windows` is keyed by the book name *at the moment of take-over*, else
the address. The Save guard checks `a.windows[under]`, and `under` is
empty when adding. So: take over `10.0.0.5:2222` unsaved; add a server
named "office" at that address as a window. Now there are two headings
for one connection ("office" with no dot; the address with one),
`openTerminalOn("office")` fails with *"already taken over
10.0.0.5:2222"*, and `openFilesOn("office")` fails with *"take over
office first"*.

In one sentence: `windows` is keyed by a name derived from the book, and
nothing re-derives that key when the book changes. Rename is handled
(`renamedWindow`); first save and remove are not.

Closed by 4bc67cc Give the taken-over windows a type with its invariant written down.

### 5. Eleven separate "what kind of host is this?" decisions

| where | what it asks |
|---|---|
| `book.go:154-166` `openTerminalOn` | `isHere` / `isWindow` / `savedWindow` / else |
| `book.go:241-255` `connectSaved` | `book.Lookup().Window` (live, case-insensitive) |
| `machines.go:543-571` `savedWindowInRoute` | `book.Lookup` + address string match |
| `machines.go:575-585` `takeOverSaved` | `windows[h.Name] != nil` ? terminal : take over |
| `windows.go:190-201` `workOnWindow` | **identical body**, keyed by address |
| `machines.go:867-889` `route` | `h.Window` from `book.Route` |
| `machines.go:943-945` `isHere` | `machines == nil && (Local or localHost)` |
| `browse.go:109-125` `filesystem` | `== Local` / `isWindow` / `machines` |
| `browse.go:292-305` `hostOf` | `machines` / `isWindow` / else Local |
| `panel.go:352-372` `hostRow` | `savedWindow or isWindow`, then `windows`, then `machines` |
| `hostmenu.go:31-36` `hostAbout` | `isHere` / `isWindow` / `isSaved` / `savedWindow` |

`takeOverSaved` and `workOnWindow` are literal twins. `connectSaved` is a
third copy of the same three lines.

Two of them disagree about the `-ssh` target: `isHere` counts
`a.localHost` as here; `filesystem` counts only `conns.Local`. With
`-ssh box`, "Browse files on user@box" is registered and always fails
with *"nothing is connected to user@box"*, because `-ssh`'s connection
never enters `a.machines`.

Case sensitivity is split too: `book.Lookup` uses `EqualFold`; the maps
are plain. `isSaved("SERVER")` can be true while `savedWindow("SERVER")`
is false.

**The fix:** one `about(host) hostKind` returning a small struct (here,
window, saved window, machine, connecting), computed in one place and
consumed by all eleven. `hostAbout` in `hostmenu.go:88-98` is already
most of that type, is pure, and has a table test. Promote it; delete the
other ten.

Closed by d96de72 Decide what kind of host a name is in one place.

### 6. Derived state, field by field

| field | written | breaks on |
|---|---|---|
| `savedWindows` | `book.go:46` only | Sound now, and deliberately written before the shortcut. Fragile by construction: depends on every `book.Put` being followed by `refreshServers`, which today holds only through the dialogs' close hooks. |
| `serverHosts` | `book.go:67` | **Stale.** Finding 3: `a.windows` membership is missing from the comparison. |
| `windows` | `holdWindow` | **Stale.** Finding 4: keyed by a book name never re-derived. |
| `paneOn` | `machines.go:762,847` | Stale after `machineDied`: panes keep pointing at a `*machine` no longer in `a.machines`. Harmless today; a dangling map into a dead object. |
| `opening`, `kept`, `paneOnWindow`, `watching`, `handedBy`, `lastSnapshot`/`openNow`, `shown`, `rates`, `serverCommands` | various | Sound. `giveUp` releasing names eagerly is right and well argued. `handedBy` keyed by id rather than pane is well thought out. |

Closed by 4bc67cc Give the taken-over windows a type with its invariant written down.

### 7. Multiple funnels for one operation

- **Start a connection.** `openRoute` is a real single funnel and its
  window guard is in the right place. But `openOn` → `route()` can
  refuse *before* reaching it, which is finding 1. The guard is in the
  funnel; the gate in front of the funnel is what leaks.
- **Take over a window.** Four entry points, three of them duplicating
  the same branch. All converge on `takeOver`, which holds the real
  invariant (one per address). Good bones, three copies on top.
- **Close a connection.** Four ways in, all reaching `dropMachine` or
  `dropWindow`. Clean.
- **Open a pane.** `openSessionTab` is one funnel; `newTerminal` plus
  `openTab`/`splitNewTerminal` is a second, and the rollback
  `delete(a.panes, t); t.Close()` is written out three times
  (`servers.go:73-76`, `panes.go:150-152`, `split.go:105-107`).
- **Register a row.** Seven `&conns.Entry{}` literals with seven
  different field subsets. `windows.go:503` uses `&meter.Meter{}` where
  everything else uses `meter.New()` -- **speculation** whether a zero
  meter reports the same initial state.

Closed by d96de72 Decide what kind of host a name is in one place.

### 8. Threading -- the healthiest part of the package

No violation of the rule that only the drawing goroutine touches `app`.
Checked and clean: every `go func` at the root captures values first or
touches only mutex-guarded state; every callback handed to `serve`,
`agent`, `jobs`, `remote` posts to the pump; both post-and-wait helpers
(`attachTo`, `onDrawing`) escape on `a.ctx.Done()` and are only called
from server goroutines; `stopServing` from a dialog cannot deadlock
against a handler parked in `attachTo` (traced through `serve.Server.
Close` and `Client.Close`, neither waits on handlers).

Two things that read like violations and are not, and should say so:

- `windows.go:139-144`: the `reach{}` literal is built inside the
  goroutine, reading `a.keys`, `a.knownWindowsAt`, `a.reachPatience` off
  the drawing goroutine. All set once before the first frame. Safe, but
  the next person will copy it.
- `a.logError` (`app.go:533`) reads `a.onError` from terminal reader
  goroutines. Set once in tests, nil in the program. **Speculation**: a
  data race under `-race` if a test set it after a pane existed.

### 9. The `app` struct: 81 fields, ten concerns

| concern | fields |
|---|---|
| rendering and layout | 17 |
| widget tree and chrome | 12 |
| machines and connections | 14 |
| panes | 7 |
| files and jobs | 7 |
| lifecycle and test seams | 8 |
| windows taken over | 5 |
| serving this window | 5 |
| agents | 3 |
| host-menu context | 3 |

Should be their own types, with their own invariants:

1. **`windows` + `paneOnWindow` + `watching` + `knownWindowsAt` +
   `reachPatience`** → one type. Invariant: one `*taken` per address;
   the name key is always `windowNamed(addr)`. Findings 4 and the
   `renamedWindow` special case are both this invariant, unstated.
2. **`server` + `servePaths` + `served` + `lastSnapshot` + `openNow`** →
   one type. Already closed over `serving.go` and `publish.go`.
3. **`agents` + `handedBy` + `handedNext`** → one type. Self-contained
   in `agents.go` except two reads. The cleanest lift.
4. **`machines` + `opening` + `paneOn`** → one type carrying the real
   invariant: a name is held by at most one of `machines` and
   `opening`. Currently spread across six files.
5. **`closing` + `closeMu` + `closeErrs`** → already an ad-hoc type.
6. **`lastPixels` + `lastSize` + `lastPad` + `geo` + `sideGeo`** → one
   value; they only change together.

There because two files needed to share something:

- `actOn` / `acting` / `menus`: a menu's argument smuggled through the
  struct because `ui.Command.Run` takes no parameters. The reason
  `currentHost` has four fallbacks, and the ugliest coupling in the
  package.
- `shown`: pure panel state, on `app` so `panes.go` can nil it.
- `connecting`: exists only for tests, by its own comment.
- Seven test seams on the production struct: `prepare`, `onError`,
  `newSession`, `servePaths`, `knownWindowsAt`, `reachPatience`, `shot`.
- `everyHost()` is an alias for `allHosts()`; `refreshServers` calls it
  three times, each walking the registry and allocating, on every
  connection made or lost.
- `openRows` (`publish.go:112`) is dead.

Closed by 6350ed0 Lift four groups of fields off the app struct into their own types.
Closed by 4bc67cc Give the taken-over windows a type with its invariant written down.
Closed by 3fa837a Give the connected machines a type and split machines.go six ways.

### 10. Splitting the two big files

**`machines.go`** (1,048 lines, twelve responsibilities):

- `machines.go` (~200): `machine`, `hold`, `revealMachine`,
  `machineDied`, `dropMachine`, `ridingOn`, `closeMachines`.
- `dialling.go` (~180): `dialling` and its methods, the "already on its
  way" dialog. One state machine, currently sitting between two
  unrelated things.
- `route.go` (~130): `step`, `hostStep`, `plan`, `route`, `dialRoute`,
  `savedWindowInRoute`.
- `connect.go` (~250): `openRoute`, `reached`, `stillWanted`,
  `sayStillConnected`, `becomeShellPane`, `startOn`, `openOn`.
- `rename.go` (~60): `renamedMachine` + `renamedWindow` +
  `renamedFiles`. One operation spread over three files today; putting
  it in one is how the next fan-out stops forgetting a map.
- `here.go` (~90): `currentHost`, `isHere`, the new `about()`, the
  `...Here` commands.

**`windows.go`** (835 lines). Two of its parts do not belong in it:

- `keysFor` + `reach` + `reachWindow` + `agentKeys`
  (`windows.go:281-484`, ~200 lines) is key selection and handshake
  plumbing with no `*app` receiver. It belongs in `remote/` or `serve/`,
  where it can be tested without a window.
- `serveFiles` + `keptOpen` (`windows.go:549-587`) is *this* machine
  serving SFTP. It belongs in `serving.go`, its only caller.
- `windowFiles` + `closeFilesOver` (`windows.go:591-655`) is the SFTP
  client side. It belongs in `browse.go` beside `filesystem`.

What is left is ~350 lines and coherent.

Closed by 3fa837a Give the connected machines a type and split machines.go six ways.

## What is sound, and should be kept

- **The pump.** One rule, stated once (`pump.go:6-11`), followed
  everywhere checked. This is the part of the codebase that is not
  rotten.
- **`connLog`.** A session that is first its own words and then the
  shell's, so the pane cannot tell. The right abstraction; it is why
  "watch a connection being made" cost one type and not a parallel UI.
- **`spot` travelling with the request** rather than being recorded on
  `app`, with the reason written down (`panes.go:474-483`). Exactly the
  discipline the rest of the struct is missing.
- **`conns.Registry` as the one list** the panel, the snapshot and
  `entryByID` all read.
- **`remoteKey` kept narrow** (`publish.go:216-225`), with the comment
  explaining why the note, state and size must not be in it.
- **`hostItems`/`hostAbout`**: a pure function from a small fact-struct
  to a menu. The shape all eleven dispatches in finding 5 should have.
- **`live()`** (`panes.go:597`): anything holding a pane across time
  asks before it uses one, and `split.go:63` does ask.
- **`errors.Join` everywhere**, with `panes.go:280-282` saying why.
- **The comments.** They say why, in plain English, and several are scar
  tissue from exactly these bug rounds ("Before the shortcut",
  `book.go:36`; "the one place every connection really goes through",
  `machines.go:396`). They are load-bearing. Do not let a refactor drop
  them.
