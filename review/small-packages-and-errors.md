# The smaller packages, and the sweep for swallowed errors

Half one covers `vfs`, `jobs`, `vt`, `session`, `input`, `conns`,
`meter` and `internal/sshtest`. Half two is a repo-wide sweep of every
discarded return value, every "log it and carry on", and every error
branch that does nothing, classified against the house rule.

## Summary

The small packages are mostly in good shape, and `vfs` has the best
error discipline in the repository. There is one real data race (new
this week), one bug that makes every tab close on Windows stall for a
quarter of a second, and a cluster of performance findings in `vt` that
between them plausibly explain the "feels slow" measurement.

The sweep is reassuring. Of 125 explicit discards in non-test code, 121
are defensible, 4 want a comment, and 1 is a violation. There is no
`recover()` anywhere. There is no empty error branch. The five
violations found are listed; two of them are deliberate fallbacks that
the rule says only Marcus can approve, and the review names them so he
can.

## Half one: the smaller packages

### 1. Data race: `SFTP.Renamed` writes `name` while a background goroutine reads it

`vfs/sftp.go:32,39`. Written from `browse.go:598` on the drawing
goroutine; read by `Name()` inside `wrap()` on every failing call, and
`ReadDir` runs on its own goroutine, as does every `jobs` operation. An
unsynchronised string field with concurrent reads and writes: a real
race under `-race`, and a torn header in practice. The `FS` doc
promises "safe to use from several goroutines"; `SFTP` is the one
implementation that is not. Introduced with the rename work this week.

Closed by 0d20f84 Stop a copy overwriting a file it could not check.

### 2. Every local session close on Windows stalls 250 ms and then kills a child that has already exited

`session/local.go:111-120`, `:168-188`. On Windows `detached` is false,
so the reaper goroutine calls `l.Close()`. If the UI closes first, the
reaper blocks inside `closeOnce.Do` -- a second `Do` blocks until the
first returns -- so the deferred `close(l.done)` cannot run, so the
first `Close`'s wait on `done` can only time out. Result: a guaranteed
`hangupGrace` wait followed by `Kill()` on a dead process, on every tab
close. Unix is unaffected. `session` has no tests on Windows (see the
test review), which is how this survived.

Closed by 290911e Stop every tab close on Windows waiting a quarter of a second.

### 3. `grid.RuneWidth` runs the full grapheme machine, twice, for every printable character

`grid/grid.go:615` calls `uniseg.StringWidth(string(r))`; it is called
at `vt/terminal.go:88` and again at `vt/screen.go:194` for the same
rune. Two rune-to-string conversions and two grapheme-cluster width
computations per character on the hottest path in the emulator, with no
ASCII fast path. The single most likely cause of the 13 MB/s ceiling.
A `< 0x7f` fast path, plus passing the computed width from
`Terminal.Print` into `Screen.Print`, should move it substantially.

Closed by 79b4cf5 Double the terminal's throughput and redraw only the rows that changed.

### 4. `applySGR` copies the whole 1 KB palette on every SGR sequence

`vt/sgr.go:22`: `Screen.Palette()` returns by value, and the type holds
`[256]color.RGBA`. Coloured `ls`, build logs and TUIs emit SGR
constantly, so this is a kilobyte copy per escape sequence, and
`defaultPen()` copies it again on every SGR 0. Return a pointer or cache
one on the terminal.

Closed by 79b4cf5 Double the terminal's throughput and redraw only the rows that changed.

### 5. The parser allocates two or three times per dispatch

`vt/terminal.go:69` feeds the vendored `go-vte` a byte at a time, and
inside it `Intermediates()` and `Params()` each allocate a fresh slice
on every dispatch, with a fresh buffer per OSC. Nothing in `vt` can
avoid this through the current API. **Speculation**, but worth
measuring: an SGR-heavy flood is probably allocation-bound.

### 6. Two full-screen cell passes per frame, one unconditional

`vt/screen.go:739-764` writes every cell through `g.Set` (each doing a
`slices.Equal` on the combining runes), then `ui/term/term.go:275-283`
copies every cell out again -- and that second loop runs every frame
whether or not anything is pending. At 240×67 that is about 32,000 cell
operations per frame per pane before anything is drawn. `Render` also
ignores which rows the emulator touched.

Closed by 79b4cf5 Double the terminal's throughput and redraw only the rows that changed.

### 7. A finished job never calls its cancel function

`jobs/jobs.go:239` creates `context.WithCancel`; `finish` and
`DropFinished` do not call it. Every job that completes normally leaves
a cancel context registered on the parent for the life of the window.
A slow leak proportional to the number of transfers.

Closed by 0d20f84 Stop a copy overwriting a file it could not check.

### 8. `beside` treats an unreadable destination as "nothing is there"

`jobs/run.go:361`: `if _, err := f.Stat(at); err == nil { refuse }`.
Any other error -- permission denied, a connection wobble -- falls
through, and the job creates or truncates at that path. A disk error
substituted with the assumption that the name is free, in the one place
where getting it wrong overwrites the user's file. **House rule
violation.**

Closed by 0d20f84 Stop a copy overwriting a file it could not check.

### 9. A failed directory-mode fixup leaves the copy half-permissioned

`jobs/run.go:241-249`. The trailing loop restores the asked-for mode on
directories created wide. If `Chmod` fails on the third of ten, it
returns and the other seven keep their widened modes, with no record of
which. The package doc promises half-written results are taken away;
this one survives.

Closed by 0d20f84 Stop a copy overwriting a file it could not check.

### 10. `vfs.Same` makes two machines the same place if they share a name

`vfs/vfs.go:133-138` compares the concrete type and `Name()`. Two SFTP
filesystems are "the same place" whenever the panel calls them the same
thing -- and `Renamed` (finding 1) can make that true at runtime. `jobs`
then turns a cross-machine Move into `From.Rename`, moving the file to a
path *on the source machine*. **Speculation** as to reachability, since
the book rejects duplicate saved names; the failure mode is silent data
movement, so identity should not be a display string.

Closed by 0d20f84 Stop a copy overwriting a file it could not check.

### 11. `vfs.Roots` takes an interface and type-asserts to `*Local`

`vfs/vfs.go:251-264`. It answers `/` for anything not a local Windows
filesystem, so a pane on an SFTP host that is itself Windows is offered
`/` as its only root. The concrete-type switch is the interface
leaking; this wants to be a method on `FS`.

Closed by 0d20f84 Stop a copy overwriting a file it could not check.

### 12. Dead and unreachable

`Screen.Modes()` (`vt/screen.go:105`) returns an unexported type and
has no callers. `jobs.move`'s same-filesystem branch (`run.go:498-500`)
repeats a test `do` already made, so it can never run; the two also
disagree about counting, which is what makes it look live.

### 13. One line allocation per scrolled line under a flood

`vt/buffer.go:166`, `:38-44`. Every line of output allocates a fresh
line of cells and fills it. A free list of lines evicted from
scrollback would make a `yes`-style flood allocation-free.

### 14. `Local.Create` reads an `Lstat` failure as "the file is new"

`vfs/local.go:121`: `_, known := os.Lstat(path)`; any error makes the
file look new and the code then chmods it to the source's mode. On a
path that exists but cannot be stat'd, the destination's own
permissions are overwritten -- the opposite of the rule two lines
above. `vfs/sftp.go:132` has the same shape.

Closed by 0d20f84 Stop a copy overwriting a file it could not check.

### 15. Harness faults

`internal/sshtest/server.go:341` slices `req.Payload[4:]` with no length
check; a malformed exec request panics the whole test binary.
`sshtest.Deaf`'s `done` channel is created, closed and never read --
dead synchronisation that reads as if it coordinated something.

### 16. Smaller things

- `conns.Registry.Groups` allocates a map, a slice and a slice per
  group on every call, from the draw loop.
- `Op.check()` runs twice per job (`jobs.go:251`, `run.go:32`); the
  second is unexplained.
- `input/paste.go:47` suppresses `\n` after `\r` but not `\n\r`, so a
  file with reversed line endings pastes two returns per line.
- `input/mouse.go:99` rejects coordinates above the legacy maximum but
  not negative ones; `byte(col+33)` on a negative column encodes a wrong
  cell rather than dropping the report.
- `meter.Meter.Totals()` loads `in` and `out` as two independent
  atomics, so a rate sample can pair counters from two instants.
  Harmless for a displayed speed.
- `meter.Bars` has no overflow guard on `uint64(max)*s`.
- `vt/screen.go:624`: ED 3 clears the current buffer's scrollback, so a
  program on the alternate screen clears a scrollback it does not have.
  Correct by accident, unstated.
- `ui/files/pane.go:251-256` keeps the previous listing when a read
  fails, showing the error beside it. Deliberate and commented, but the
  user is acting on names `vfs` just refused to vouch for.

### What is sound here

- **`vfs`'s error discipline is the best in the repo.** `ReadDir` fails
  the whole listing rather than dropping a name; `Open` refuses a
  directory before the destination is truncated; `Create` and `Mkdir`
  remove what they half-made and return both failures joined. The
  `ErrNotExist` skips are the only exception and each says why.
- **`jobs.file`** is textbook: write to a uniquely numbered `.part`,
  close, rename onto the name, and on any failure return the error
  joined with the removal's. The name at the destination is never half
  a file.
- **`jobs` refuses to decide a conflict alone**, refuses to remove the
  originals of a Move when anything was skipped, and reports a
  half-finished delete-after-copy as exactly that.
- **`vt`'s bounds discipline**: the combining-mark cap and the REP cap
  both close amplification attacks, with the measurement that motivated
  them in the comment. OSC 52 reads are deliberately unanswered, with
  the reason written down.
- **`input`** is pure, allocation-free, toolkit-free and fully testable;
  `ebitenin` is genuinely the only file that imports ebiten.
- **`meter`** reads no clock, which is why its rules are testable
  without sleeping.
- **`Repaint`**'s reasoning about deferred wrap and about sending the
  primary screen under an alternate one is unusually careful.

## Half two: the sweep for swallowed errors

The rule, verbatim: *"Never ignore an error from a database or from disk
I/O. Do not log it and carry on. Do not substitute a fallback value. Do
not degrade to a partial result. Propagate the error and stop the
operation that needed the data. The only exception is when Marcus
explicitly asks for a fallback."* This repo applies it to network and
close errors too.

**No `recover()` exists anywhere, tests included.** Three `panic` sites,
all defensible: two for programmer error registering a built-in, one
for the test harness failing to start.

### Sweep 1: explicit discards (`_ =`) -- 125 hits in non-test code

Per package: remote 38, internal/sshtest 31, serve 29, root 13,
session 5, agent 5, vt 1, ui/term 1, jobs 1, glyph 1.

By what is discarded: `Close()` 71; `Reject`/`Reply` 21;
`Write`/`Encode`/`SendRequest` 9; `os.Remove` 6; `io.Copy` 3; `Wait()`
3; `SetDeadline` 2; `Kill()` 1; other 9.

Only 23 of 125 carry a comment on the line above, though many sit
inside a block whose comment covers them (the atomic-write sequence in
`remote/book.go:504-528` is the good example).

- **Defensible: 121.** Every `Close()` is on an error path where the
  real error is already being returned, or a best-effort teardown of
  something already known to be gone. All 21 refusals are written down
  a connection being dropped anyway. The 9 writes are last words before
  hanging up. The 6 removes are temp-file cleanup after the real error.
- **Needs a comment: 4.** `jobs/run.go:419` -- the *source* file's close
  is dropped while the destination's is checked two lines later; for
  SFTP a read-handle close can carry the real error. `machines.go:81`
  and `windows.go:514` -- `_ = conn.Wait()` and `_ = win.Wait()` throw
  away the reason a connection or a window died, so the user can only
  be told *that* it went. `ui/term/term.go:574` relies on a documented
  never-fails contract and should name it.
- **Violation: 1.** `glyph/fallback.go:101` -- below.

### Sweep 2: log-and-carry-on -- about 45 sites

In `serve`, `agent` and `remote/tunnel.go` every one is at the top of a
goroutine with no caller to return to, and control flow stops: each is
followed by `return`, or by `continue` to the next *independent*
connection. That is reporting, not carrying on, and it is the right
shape.

Control flow continues with a partial result in three places:

- **Violation -- `clipboard.go:64-70`.** `clipboardRead` logs the
  failure and returns `""`. A substituted fallback: the paste then
  pastes nothing and the user sees a no-op, with the reason on a log
  they are not reading. Should return the error and let the paste fail
  loudly.
- **Violation, deliberate, needs Marcus's word -- `fonts.go:50-58`.** A
  font-directory read error is logged and the partial list is used
  anyway, presented as the list of installed fonts. The comment argues
  the case ("not a reason to take down a window with shells running in
  it"). That is a fallback the rule reserves for Marcus to ask for
  explicitly; he should say yes or no.
- **Needs a comment -- `serve/host.go:216-219`.** An unmarshal error and
  a resize error both collapse into one boolean. The reply tells the
  client it failed; nothing records why the far pane could not be
  resized.

Non-violations worth naming: `book.go:118,143` and `fonts.go:79-86`
discard command-registry collisions, not I/O, and drop only the
colliding entry, then keep it off the menu so nothing greyed-out
appears. `machines.go:696` logs a close failure at process exit because
there is no window left to tell.

### Sweep 3: error branches that do nothing, `continue`, or `return nil`

- **Violation -- `glyph/scan.go:227-229`.** `return nil, nil` after
  `sfnt.ParseCollection` fails. The file's whole design is to accumulate
  failures into `failed` and hand them back, and its doc says so; this
  path drops the error on the floor, so an unreadable font file is
  indistinguishable from one with no faces. Two lines above, the same
  function returns the error correctly.
- **Violation -- `glyph/fallback.go:101-105`.** The `WalkDir` return is
  discarded *and* the callback returns nil on every per-entry error, so
  an unreadable font tree silently yields a shorter fallback list. The
  user gets tofu boxes with no account of why. The comment concedes the
  trade ("not worth failing over") -- again a degradation the rule
  reserves for Marcus.
- **Defensible:** `glyph/scan.go:116-118` (collects into `failed`);
  `mcp/window.go:56-59` (collects and joins, with the reason written);
  `remote/hostkey.go:257-259` (a line that does not parse is *counted*
  and reported separately -- the model the rest should copy);
  `serve/host.go:47-49` (reports, then the next independent channel);
  `vfs/local.go:67-72` (skips only `ErrNotExist`); `book.go:143-145`
  (a command id collision, not I/O).

### The violations, in one list

| where | what |
|---|---|
| `jobs/run.go:361` | a `Stat` error is read as "nothing is there" and the job overwrites |
| `vfs/local.go:121`, `vfs/sftp.go:132` | an `Lstat` error is read as "the file is new" and permissions are overwritten |
| `clipboard.go:64-70` | a clipboard read failure becomes an empty paste |
| `glyph/scan.go:227-229` | a font file that cannot be parsed is reported as having no faces |
| `glyph/fallback.go:101-105` | an unreadable font tree yields a shorter fallback list |
| `fonts.go:50-58` | a font-directory read error yields a partial list, **deliberately** |

The last three are the same decision -- "a bad font file should not take
the window down" -- made in three places. It may well be the right
decision. Under the house rule it is Marcus's to make, once, and the
code should then say he made it.

Closed by 0d20f84 Stop a copy overwriting a file it could not check, for the
job and the two filesystems.
Closed by 0320a9c Give dialogs one key rule, tell the user why a connection
went, for the clipboard read.
Closed by b00f97e Make the font fallback one decision, reported once, for the
three font rows.

### What is sound repo-wide

- **The atomic-write sequences are correct and complete**: validate what
  is about to be written can be read back, write to a temp file in the
  same directory, `Sync`, check the `Close`, rename, remove the temp on
  every failure path. `serve/identity.go` uses `Link` rather than
  `Rename` so two windows cannot clobber each other's host key, with
  the race spelled out.
- **`remote/hostkey.go` refuses to answer from a partial `known_hosts`**:
  an absent file is skipped, any other read failure stops the lot, and
  the doc says why in the rule's own terms.
- **`errors.Join` is used correctly and often** -- to carry both the real
  failure and the cleanup's, never to paper over either.
- **`serve/host.go:240-265` treats "the client stopped receiving output"
  as a failed session** regardless of what the program thought.
- **Close errors are checked wherever the result still matters**, and
  `io.EOF` is filtered at the point of close rather than swallowed
  wholesale.
- **No empty error branch and no `if err != nil { return nil }` anywhere
  in non-test code.**
