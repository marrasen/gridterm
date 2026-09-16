# Plan: the TODO list

What this does, in order, and why in that order. Each step is one or
more commits. Each commit is reviewed adversarially before the next
step starts, by reviewers that cannot edit. Marcus is away while this
runs, so every step is built on the plainest reading of the TODO line,
and the reading is written down here so he can reverse it.

Sources: `TODO.md`; `review/plan.md` Phases 8 and 9, which the review
plan left for after the structure had stopped moving; the code facts
in each step were checked against the tree on 16 September 2026.

## The order, and the reasoning

1. **The two structural phases first.** `review/plan.md` says the TODO
   features "go on top of a structure that has stopped moving". Phase 8
   (duplication) and Phase 9 (test hygiene) are that. Every target the
   plan named still exists.
2. **Cheap, shared roots next.** The connection log keeps no copy of
   its lines; one two-line change opens the log colour, the fold, the
   log view and the help view.
3. **The sidebar and the panes**, small look changes that touch one
   file each.
4. **Scaling a held screen.** Marcus's "Now" item. It is the hardest
   change and it comes after the warm-ups, once the compositor's
   contracts have been read for the look changes.
5. **Connections**, three independent behaviours.
6. **Copying files**, one change that carries three lines of the TODO.
7. **Known gaps**, each small and independent.

Nothing is left for a question. Every line of the TODO has a plainest
reading, and each is stated in its step under "Reading". The one to
look at first when Marcus is back is Step 2's fold.

## How each step runs

An Opus implementer writes the code and the tests, runs `gofmt`,
`go vet`, the suite and the race runs, and does not commit. The
working step is committed. Two or three Opus reviewers, read-only,
attack the diff with separate briefs. Real findings are fixed;
findings that are wrong are answered in the commit message. Commit.
Mutation testing where the step added logic. Every test of a user
action starts at the click, the menu line, the button or the chord.
A `TODO.md` line goes in the same commit as the work that closes it.

## Step 0 -- Phase 8, duplication

`review/plan.md` Phase 8; `network-layer.md` #9;
`toolkit-and-rendering.md` "Duplication"; `tests.md` #4.

- `keysFor` (`remote/window.go`) folds into `authMethods`
  (`remote/auth.go`): one ladder, one agent timeout. `Reach` keeps its
  shape.
- `Menu`, `Palette` and `List` share one list state machine (`at`,
  `top`, clamping) with a row painter each.
- One truncation helper (five today: `sidebar.go trimTo`,
  `ui/form.go trimTo`, `ui/files trimTitle`, `trimTail`, `trimLeft`),
  one colour mixer (three: `panel.go mix`, `ui/list.go blend`,
  `render blend`), one strip painter, one chord vocabulary
  (`ui/keymap.go` and `ui/files/keybar.go`).
- One `silentMachine`, in `internal/sshtest`.
- `serve` exports its channel-type sentinel and `remote/shell.go`
  checks it instead of matching "session@gridterm" in an error
  string. `remote` already imports `serve`.

Done when: no behaviour changes; the suite is the check; the helpers
named above each exist once.

## Step 1 -- Phase 9, test hygiene

`review/plan.md` Phase 9; `tests.md` #9-#11, #15-#21, #24, #26.

- One waiting helper with one budget in the root tests (fifteen
  today). One dialog-opening helper. `awaitModal[T]` replaces the
  form and notice waiters.
- `TestASavedWindowIsTakenOverHoweverItIsAskedFor` goes: its four
  subtests call app methods, and `TestEveryWayInTakesOverASavedWindow`
  starts at the click. Other tests that call the function the author
  had in mind are moved to the click where a helper exists.
- `typeIntoField` everywhere fields are typed into; `answer` takes
  labels.
- `newTestApp` closes agents. The menu tripwire runs over `hostItems`
  for each shape. Source-reading tests use `go/ast`.
- The skips Phase 0 did not reach: `render/compositor_gpu_test.go`,
  `ui/form_test.go`, `ui/palette_test.go`.
- `review/` files: a finding closed by Phases 1-7 gets a line saying
  which commit closed it.

Done by 9ec8421 Make the tests start where the user does, with one way to wait.
Done by 70caa8c Make the dial tripwire see nested calls, and record what is only partly closed.

## Step 2 -- the connection log: kept, coloured, folded, viewable

`TODO.md` "Fold the connection log", "Colour the connection log",
"A log view". Also the help gap under "Known gaps".

Facts: `connLog.write` (`connlog.go`) is the one funnel every line
goes through; `Read` drains the bytes and keeps no copy; `Became`
writes nothing on success; the pane is a `vt` terminal and `vt` has
no fold. `ui.Notice` already wraps, scrolls, selects and copies.

- `connLog` keeps its lines (`[]string`, under its mutex) and offers
  `Lines()`.
- Colour: `Say` writes the time in dark green on a line that went well
  and dark red on one that did not, the words in a darker grey. `Failed`
  and `GaveUp` mark their lines bad. `serve.Plain` already stops the
  far end forging SGR.
- **Reading of "fold":** `vt` cannot hide a span, so at `Became` the
  pane's account is replaced by one line -- "connected to X in N s; the
  log is on the row's menu" -- and the full account opens from the
  row's plus menu ("How it was reached") and a palette command, in a
  `Notice`. That is the log view too. If Marcus meant a collapsible
  span inside the pane, that needs a `vt` concept that does not exist
  and is a separate decision.
- **Help:** a "Help" line on the menu bar and F1 open a `Notice`
  listing every command with its chord, from `ui.Commands.All()` and
  `Keymap.ChordFor`. Same shape as the log view, no new widget.

Done when: a connection's lines are coloured; after it connects the
pane shows one line; the plus menu opens the whole account, copyable;
F1 lists the keys.
Done by b52d607 Keep, colour and fold the connection log, and add a help view.
Done by d1c9a6f Filter the window's own account lines, wake the pane at the fold, align the help.
Decided in review: help is on Ctrl+Shift+H, not F1, which belongs to the programs in the shell.

## Step 3 -- the sidebar: the type icon, the grey bar

`TODO.md` "Use the type icon instead of the dot", "Remove the grey bar".

- `ui.ListRow` gains `IconFG`; the panel sets it to the colour the dot
  had (`a.mark`) and stops setting `Mark` on connection rows. The four
  dead icon runes in `panel.go` go. **Reading:** headings keep their
  dot -- there is no type icon for a machine -- and the text keeps its
  column, so nothing else shifts.
- `a.dock.DividerFG` becomes transparent, so the dock draws a blank
  column instead of `│`. The column stays: it is the drag handle, and
  Step 4 makes the other dividers draggable too. **Reading:** the
  column's background stays the window's.

## Step 4 -- draggable dividers

`TODO.md` "Dividers should be draggable".

Facts: `ui.Split.Weight` already persists; nothing writes it, and
`Split.HandleMouse` declines a press on the divider, so `Root` never
captures the drag. `ui.Dock` is the working model (`dragging`,
`dragTo`, `CancelGesture`). The file browser is not a `Split`; its
`paneCell` divides evenly.

- `Split` mirrors `Dock`: a press on the divider is taken, moves set
  `Weight`, release ends it, `CancelGesture` clears it.
- `files.Browser` gains per-boundary weights in `paneCell` and the
  same gesture.
- Tests drive `Root.HandleMouse` with press, move, release.

## Step 5 -- scale a held screen

`TODO.md` "Now".

Facts: `Terminal.Draw` copies cells into the view it is given and
clips a held pane bigger than its box to the top-left corner. The
compositor draws layers to offscreen images (`render.Layer.tex`) and
blits them with a translate only; `region.go` is the precedent for a
widget on its own grid, layer and geometry. `placement` records what a
layer's pixels depend on, so a scale must be part of it or the screen
keeps stale pixels.

- A held pane whose `Size()` exceeds its `Box()` is drawn onto a grid
  of its own the way `region` does, on a `render.Layer` with a new
  `Scale`, applied before the translate and recorded in `placement`.
  The tree stops drawing that pane (the `PanelElsewhere` shape).
- **Reading:** uniform scale, aspect kept, letterboxed in the box;
  linear filtering; only while the held size is the bigger one; mouse
  routing maps through the inverse scale so the pane stays clickable.
- Damage tracking is load-bearing: the layer repaints rows as today,
  and a scale change forces the screen clear.

Done when: a 120x40 held screen is fully visible in an 80x24 box and
the host's row still says the size.

## Step 6 -- connections

Three commits.

1. **"Forget" closes its connections first.** `confirmRemoveServer`
   asks `about(name)`; when the name is held, the dialog says the
   connection will be closed, and Remove closes it (`dropWindow` or the
   entry's own `Close`), gives up on a dial on its way, then removes
   the name. A close that blocks runs off the drawing goroutine as
   today and does not hold the forget up.
2. **"Files" connects like "Terminal".** `openRoute`'s `command`
   becomes a "then" value whose default opens a shell and whose other
   case opens a file pane; `openFilesOn` on an unconnected saved
   machine builds the route the way `openTerminalOn` does. "Already
   connecting" works through `dialling.waiting` unchanged.
   **Reading of "SFTP-only":** the connection is made and no shell is
   opened on it; the connecting pane closes on success (its account is
   in the log view) and stays on failure.
3. **The serve dialog remembers its settings.** A versioned
   `settings.json` beside `servers.json`, written the way the book is
   written (temp, sync, rename), holding the port and the reach.
   Loaded at start; a missing file is not an error; a corrupt one
   refuses to save, like the book. **Reading:** the port is saved as
   typed (0 stays 0); serving itself stays off at start; nothing else
   joins the file yet.

## Step 7 -- copying files: progress, cancel, repeat

`TODO.md` "Copying files", three lines, one change.

Facts: the job row's `Reveal` is nil, so a click does nothing.
`jobs.Progress` already has files, bytes, current name and start
time; `meter.Rate` already has the speed; `Job.Cancel` exists;
`Job.Op()` remembers source and destination.

- `startJob` sets `Reveal` to open a dialog with the progress and the
  rate, refreshed each frame from `refreshJobs` while it is open, with
  Cancel while running and Repeat when done.
- **Reading of repeat:** the source and destination are re-resolved by
  host name through `a.filesystem`, so a repeat after the machine
  reconnected still works.

## Step 8 -- known gaps

One commit each, smallest first.

1. A sidebar row drawn before a re-key names the window by its old
   key: rows carry the `*taken` or re-derive the key on click.
2. `windowFiles` gets one deadline like `Conn.Files`.
3. `Shell.Resize` runs its `window-change` under the same
   `writing`/`quiet` discipline as `Write`, off the drawing goroutine
   when the channel is busy, coalescing to the last size.
4. Cursor blink: `grid.Cursor` gains `Blink`; DECSCUSR 1, 3 and 5 set
   it; the renderer hides the cursor on a half-second clock using the
   `pulse` precedent so only the cursor row is dirtied.
5. `-ssh` opens the window first and connects in a pane through
   `openRoute`, so it asks in a dialog and has a connection log.
6. Windows lost output: the reaper waits for the ConPTY to drain,
   bounded, before closing the pseudoconsole when the child exited on
   its own. Tested with `cmd /c echo` on Windows.

## What is not in this plan

Nothing from `TODO.md` is left out. The "Errors and logs" dialog lines
were closed by Phase 2 and go from the TODO in Step 1's review pass.
