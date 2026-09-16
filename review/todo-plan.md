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
Done by 1707651 Fold the duplicated pieces the review named into one of each.
Done by e1de8df Keep the take-over's passphrase dialog and cancel working under one ladder.

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
Done by 065865d Draw a connection's kind icon in its state colour, and blank the dock's divider.
Done by 8f5ea0b Keep the state visible in a narrow sidebar, and let a heading have its own colour.

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
Done by 73ea052 Let the dividers between panes be dragged.
Done by 728fc32 End a divider drag only on the button that started it.

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
Done by 6105bef Scale a held screen down so all of it is visible.
Done by 6b05074 Bound the scaled layer, and give its mouse capture the toolkit's rules.

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
Done by abe6a00 Close a server's connections when it is forgotten; 323908a Say what else goes when a jump host is forgotten.
Done by 11d94e5 Let "Files" connect to a saved machine the way "Terminal" does; 42ca0e4 Say "connected, but the files could not be opened" instead of "not made".
Done by f48ce99 Open the serve dialog on what it was last set to; effdfbe Share the repeated-key check between the two JSON files, and say a newer file plainly.
Decided in review: the list is asked before anything is closed, because Remove can refuse a hop others are reached through.

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
Done by 4f9d834 Show a copy's progress from its row, with Cancel and Repeat; 5ee27b3 Say when a cancelled copy could not clear up, and keep its dialog still.
Decided in review: Repeat is offered for a copy only; a delete done again would take something away without asking, and a move cannot work twice.

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
   Done by 22f2f77 Open the window first with -ssh and connect in a pane.
   Decided in review: the `-ssh` target is an ordinary connected machine
   and `localHost` is gone, so "here" means the machine gridterm runs on
   and nothing else. A new tab or split still opens on that machine
   while it is connected -- the window keeps it as its home machine --
   and opens here once it has gone; the `-e` command runs only in the
   pane the window opened with. Only the bare keys follow the home
   machine: a line that names a machine means that machine, so
   "Terminal" on this machine's row, its palette command and the
   chooser's line for it all open a shell here. The window is never left
   empty: when the connection opens no pane at all, the first frame
   opens a shell here and says why.
   Correction: 22f2f77's message says it dropped the `Shell.Resize` line
   from the TODO. It did not. It dropped the three "Copying files" lines
   (Step 7's work), the Windows lost-output gap and the `-ssh` gap. The
   `Shell.Resize` line goes with the resize fix.
6. `dropMachine` and `letGoOfWindow` close inline on the drawing
   goroutine. `Conn.Close` waits a 250 ms drain per rider and
   `dropMachine` recurses, so forgetting a jump host can stall the
   window for about a second.
7. Windows lost output: the reaper waits for the ConPTY to drain,
   bounded, before closing the pseudoconsole when the child exited on
   its own. Tested with `cmd /c echo` on Windows.
   Done by 1e8ac89 Keep a short-lived command's output on Windows.
   Not as planned: there is no timer and no bound. `ClosePseudoConsole`
   is itself the flush signal -- it returns only once the console host
   has written the child's last output to the pipe and closed its end.
   So the reaper frees the pseudoconsole and leaves the pipes open, and
   the pending read drains and then ends on its own.
Item 1 done by a23ef28 Name a far screen's row by the window itself, not by its key.
Item 3 done by 7eb0cd1 Send a shell's size change without waiting on the wire; 85b07a3 Bound the polite end-of-file the way the session close is bounded; 45c2549 Report a resize that failed after the shell was closed.
Item 4 done by 049f151 Let the cursor blink when the program asks for it; 1584d13 Keep the cursor's drawn half in step with its phase across hides and grid swaps.
Item 6 is a known gap recorded by the forget review, not yet done.

## What is not in this plan

Nothing from `TODO.md` is left out. The "Errors and logs" dialog lines
were closed by Phase 2 and go from the TODO in Step 1's review pass.
