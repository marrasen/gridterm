# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## What Marcus is looking forward to

In his own order, from 2026-09-19. Bugs and small items come first, and
then these.

1. **The file viewer.** Its own section below.
2. **Pasting an image into a terminal.** Done on Windows. What is left
   is in its own section below.

"Show every pane" was the first of these and is done. It held the window
at full speed for a picture that was not moving: the tiles painted a
ground and drew their frames over it, and writing a cell twice dirties
its row whatever it ends up holding. Drawn through a `ui.buffer` like
every other widget, a settled switcher skips the frame entirely.

What is left there is not worth doing without a reason to. Each tile's
grid is the pane's whole screen rather than the size it is drawn at, and
the panes behind the switcher are still drawn into a grid nothing blits
-- but that pass costs 120 microseconds on a 140x44 window with eight
panes, which is under one per cent of a frame.

## The context menu is deferred

Marcus asked on 2026-09-19 to leave it until the bugs and the small
items are done. The questions it waits on are at the bottom of this
file, under "Waiting on an answer from Marcus". Nothing blocks them any
more: the modifier keys reach the mouse now, so Shift+right-click can
work whichever way he answers.

## Picking text out, what is left

Everything the review of the selection work on 2026-09-19 turned up is
answered. What was done:

- **Shift and a page key picks text out rather than scrolling.** A
  widget can now claim a chord and get it before the window's
  accelerators, through `ui.ChordClaimer`, and the reader claims
  Shift+PageUp and Shift+PageDown. A terminal claims nothing, so those
  two still scroll its scrollback. The reader claims nothing while it
  has no text either, so an empty one still scrolls.

- **A text box can be dragged over.** `Field` picks text out with the
  pointer now, and the form and the palette carry the drag to it. A
  press puts the caret where it landed and starts a selection, a move
  carries the loose end, and a release that never moved leaves a click.
  Shift and a press carry the selection to where it landed. A drag off
  either end of a field runs to the end of the text, so a value wider
  than its box is picked out whole. A click on the first column of a
  scrolled field used to jump the caret to the start of the text and
  now lands on the first character shown.

- **Ctrl+X in the file viewer says why it did nothing**, and the reader
  declines a chord its bar never offered. It used to match any Ctrl
  chord, so Ctrl+Shift+R reread the file and Ctrl+Shift+F followed it,
  and Ctrl+Shift+H and Ctrl+Shift+D only reached the window's help and
  split because an accelerator runs first.

What is left of the drag: a drag held still past the edge of a field
does not keep scrolling. It picks out as far as the text that is shown
and waits for the pointer to move again, because the field only hears
about a move.

## Asked for on 2026-09-19, second set

From Marcus's inbox.

- **Only the font size goes by the character a key produces.** The
  punctuation keys this window binds are read from what the layout says
  they print, so plus is plus wherever it sits. Everything else is still
  read from where a key sits, which is right for a letter and a function
  key and worth revisiting if a shortcut on a letter ever lands
  somewhere awkward on a layout. Worth settling with the keyboard config
  under "Asked for, not yet worked out".

- **A release flow, version numbers, and documentation.** Binaries for
  Windows and Linux, and macOS if it is easy. Two versions are wanted,
  not one: what the program calls itself, and what the wire between two
  windows calls itself, so a window can tell a build it cannot talk to.
  Nothing in the protocol carries a version today, and `openSession` is
  positional, so a build that disagrees is refused by a parse failure
  rather than by a number.

  A release needs something to read with it. The README says nothing
  about changing a keyboard shortcut, and that is the example Marcus
  gave: where the file is, that it holds changes rather than the whole
  map, that a moved shortcut takes two lines, and how to write a chord.
  The same goes for the colour themes, the server list, and what a
  copy that carries its own files is for. Some of it is already written
  inside the window, in "Keys and commands" and "Where gridterm keeps
  its files", and the README can say the same things once.

## Waiting on an answer from Marcus

These are all about the context menu, which is planned below.

- **Right click in a pane where a program owns the mouse.** vim, mc and
  htop ask for the mouse, and then the right button is theirs. The
  window already has a rule for this: hold Shift and the program is
  bypassed, which is how a selection is made over one. Should the menu
  follow that rule, so Shift+right-click opens it and a plain right
  click goes to the program? The other way is for the menu to win
  always, and for those programs never to see the right button.

- **A connection row on the sidebar has no menu to open.** The plus on a
  machine heading opens one; a row's button is a cross that closes it.
  Should a right click on a row open a new menu, and what should be on
  it: go to it, close it, share it with an agent, write down what the
  agent typed? Or should a row have no menu?

- **Which pane does the menu act on?** A right click on a pane that is
  not in front could focus it first, so "Close pane" cannot close a pane
  the user was not pointing at. Or the menu could act on the pane in
  front whatever was clicked. The first is what most windows do.

- **What goes on a terminal's menu?** Suggested, in this order: Copy,
  Paste, a rule, Split right, Split down, Take this pane out of its
  split, a rule, Share this pane with an agent, Close pane. That is nine
  lines, which may be too many to read at a glance.

- **The file browser.** Its operations are on the key bar: rename, copy,
  cut, paste, delete, make a directory. Should a right click offer
  those, and should it move the selection to the item under the pointer
  first?

- **A keyboard way in?** Shift+F10 and the Menu key are what other
  windows use. Worth binding, or is the palette enough?

## Settled, do not re-open

- **A failure belonging to a pane the user has closed goes to the log,
  not to a dialog.** Answered on 2026-09-20. Closing a file pane on a
  machine that has stopped answering can leave a listing in flight,
  and it fails long after the user stopped waiting for it. The dialog
  landed over whatever they were doing next and took their next click.
  `reportForPane` in browse.go shows the dialog only while the pane is
  still open and logs it otherwise. The error is not dropped: "Show
  what the window has logged" is where it goes.

- **The file viewer stays a viewer, with no caret.** Answered on
  2026-09-20. A keyboard selection goes on starting at the top left of
  the view, which is a surprise in a file scrolled halfway down and is
  the price of the arrows still scrolling. Giving the reader a caret
  would mean Up and Down moved it and the view followed, which is an
  editor rather than a pager, and the reader is a pager.

- **A new pane opens on the shell that was picked last.** Answered on
  2026-09-19, from the code. Three things are asked in order: a command
  named with `-e` on the command line, then the shell written down in
  the settings file, then this machine's own default. The settings file
  is written by `rememberShell` when a shell is opened by name from the
  File menu or the palette, so the last one picked is what the next pane
  and the next run open on. "Default shell" on that menu opens on the
  machine's default and forgets the pick. With nothing written down,
  `session.DefaultShell` answers `%COMSPEC%` on Windows, which is
  cmd.exe, and `$SHELL` then `/bin/bash` then `/bin/sh` elsewhere. So a
  first launch on a fresh settings file opens cmd.exe, not PowerShell.

- **"Connection closed." already has two buttons.** Checked on
  2026-09-19 against `d79f629`, which landed on 2026-09-17. The pane
  says "Connection closed.", with the exit status after it when there is
  one, and offers Reconnect and Close. Close is the default, so Enter
  takes it, which `TestEnterClosesThePaneRatherThanStartingItAgain`
  pins.

- **A pane that cannot be put in a job object opens no pane, and the
  window says why.** Answered on 2026-09-17: open the window and show
  the failure in it. Opening the pane anyway was the wrong trade,
  because it brings back the orphaned shells the job object is there to
  stop, in silence. `log.Fatal` was the wrong trade too: started from
  Explorer there is no console for it to reach.

- **The keyboard shortcuts file holds changes, not the whole map.**
  Answered on 2026-09-17: leave it holding changes for now. Moving a
  shortcut therefore takes two lines, and the notice and the README say
  so.

- **A listing that fails while a path is being completed is not shown.**
  Answered on 2026-09-17: keep it off the screen. A dialog per keystroke
  would be worse than the fault, so the failure goes to the log and the
  completion offers nothing.

- **Nothing caps the panes a window keeps.** Answered on 2026-09-16: no
  cap. A pane worth keeping is worth reusing, so reusing it is made the
  easy thing rather than throwing the transcript away. Closing a pane
  does release what it held.

- **Every shell gets "Connection closed. Reconnect?"**, whether the
  transport went or the user typed exit, and whether the shell is on a
  machine or on this one. That is what ssh itself prints, and gridterm
  calls every pane a connection. Settled twice, on 2026-09-17: a
  reviewer argued a local shell was never connected and reconnecting it
  only forks a process, and Marcus kept the one wording anyway.

- **A command is worded differently**, and not for tidiness. Nothing is
  being connected: the command's channel closed and the SSH connection
  is still up. The question names the command and the choice says it
  runs it again, because running `make deploy` a second time is a thing
  the user has to see before they press it.

## Showing which panes are shared

Asked for on 2026-09-17, from Marcus's own notes. A pane handed to an
agent or driven from another window looks like every other pane, and the
user has to know which is which.

- **A chip in the top right saying what is happening.** "Agent
  connected", "Agent running: ls -la", with a long command cropped.
  Clicking a chip opens the dialog that can end it. Two chips when both
  are in force, one per border. The half that names the command could
  use the record of what the agent typed, which now exists, though the
  last line sent is not always the command that is running.

- **An account of what the agent did.** Somewhere to read the commands
  an agent ran, after the fact, rather than scrolling the pane.

  - **What the agent sent is written down, and is on the Servers menu
    as "What the agent typed".** Answered on 2026-09-17: keep the
    record. The user hands the pane over, gives the access and holds
    the secrets, so what the agent does there is theirs to read and is
    not hidden. It is what was sent, not what ran: Backspace, Tab
    completion and Up through the history all change a line before the
    shell sees it, Ctrl+U throws a line away and the text glues itself
    onto the next one, a here-document reads as four commands, and in a
    full-screen program every line typed reads as a command. The dialog
    says so. A secret the user types at the agent's asking is not in
    it, because the user types that themselves.

  - **A nicer account can still be built from what the pane echoed.**
    The line a shell prints back is the command it is about to run,
    after completion and after editing. It is already on the screen, so a dialog showing it gives
    away nothing the user could not read by scrolling, and a password
    at a prompt that does not echo never appears at all.
    `handover.markPrompt` in agents.go already writes down the prompt
    and the line it was typed at, which is where such a reader starts.

  - **Say nothing on the alternate screen.** `markPrompt` already stops
    there, because a full-screen program has no prompt and no commands.

  - **When to read the echo is the open question.** It lands after the
    send has returned, so something has to look afterwards: the next
    call the agent makes, or a frame while the pane is handed over.

- **A pane on a window taken over from elsewhere gets no border.** The
  border reads `pane.Watched()` and the handover list, and the pane
  drawn on the window that took over has neither: what is shared is the
  pane on the other machine. So the window doing the driving marks
  nothing, and the window being driven marks everything. Worth deciding
  whether the driving window should mark it too.

## The agent, through MCP

Raised after a debugging session in a handed-over pane.

1. **No expiry for now.** Answered on 2026-09-17. A hand-over lasts
   until the user takes it back or closes the pane. Revisit if forgotten
   hand-overs ever pile up in practice.

2. **A command pane can be handed over already**, running or not.
   `handPane` looks at no kind and no state. A running one takes keys on
   the command's stdin; an ended one can be read and not typed into,
   which is worth having for a failed build. Nothing to do here: it is
   written down because it looked like a gap and is not one.

## Pasting and dropping an image

Asked for on 2026-09-17, after seeing Claude Code take a pasted image in
a terminal.

Pasting a picture is done on Windows. Paste hands it over by whichever
route reaches the program: the paste key for a pane on this machine, the
picture sent to the clipboard of a gridterm taken over, and a file
written on a machine reached by SSH. `edit.pasteImage` on Ctrl+Alt+V
asks for a file wherever the pane is and types the path. What is left:

- **Only Windows reads a picture off the clipboard.** Everywhere else
  the command says there is none. Linux and macOS each need their own
  reader.

- **Nothing clears the pictures off a machine reached by SSH.** They go
  under `gridterm-pasted` in the home directory of whoever the
  connection logs in as, and stay there.

- **Nothing takes the files away again.** They pile up in the temporary
  directory under `gridterm-pasted` until the system clears it.

- **A dropped file always goes under home, and nothing clears it out.**
  Dropped files land in `gridterm-pasted` under the home directory of
  whoever the connection logs in as, the same place a pasted picture
  goes, and they stay there. Marcus may want to choose where instead: the
  directory a browser pane is showing on that machine would be an
  obvious second answer.

- **A dropped file is copied even when the pane is on a window this one
  is connected to.** It goes through that window's files, which is
  right, but a gridterm at the far end could take it down the clipboard
  channel instead. Worth nothing until somebody wants it.

## Switching between panes

Asked about and answered on 2026-09-17, and done on the same day.
Ctrl+Tab walks the panes in the order they last had focus: hold Ctrl,
press Tab to go back through them, Ctrl+Shift+Tab to go the other way,
and let Ctrl go to land. A list in the middle of the window says where
the walk is. Ctrl+PageUp and Ctrl+PageDown still follow the sidebar, so
there is one key for position and one for recency.

- **A file manager is entered twice per lap.** The sidebar gives each of
  its panes a row, under whichever machine that pane reads, so a browser
  with a local pane and a remote one is two stops far apart in the walk
  of the sidebar. Two of the presses only move the highlight inside a
  browser already in front, which can read as the key doing nothing.
  This is about Ctrl+PageUp and Ctrl+PageDown, not about Ctrl+Tab.

- **The order goes stale while the sidebar is shut.** `refreshPanel`
  stops building rows when the dock is collapsed, so the walk of the
  sidebar follows the last order it saw, with panes opened since on the
  end. Everything stays reachable. Nothing on screen contradicts it,
  because there is nothing on screen.

- **Two panes can read the same in the list.** It names a pane the way
  the sidebar does, and two fresh shells on this machine whose programs
  have set no title are both "PowerShell on this machine". Which one the
  walk is on is still marked, so nothing is ambiguous about where it
  will land, but the names do not tell them apart.

- **The shortcuts config file has to be able to say this.** A binding
  that holds a modifier is not a plain chord. Worth settling when the
  keyboard config under "Asked for, not yet worked out" is designed, so
  the file does not have to change shape twice.

## Asked for on 2026-09-19

From Marcus's inbox, after working in a shared window.

- **The words a user reads say "connect to" now; the code still says
  "take over".** Settled with Marcus on 2026-09-19: the client's side
  reads "Connect to another window…", mirroring the host's "Serve this
  window…". "Work in" was wrong because nothing moves: the panes stay on
  the window serving them and are drawn here at the same time. "Share"
  was wrong because a share is already the set of panes handed to an
  agent, and "session" because a session is already a running shell.

  `serve.takeOver` keeps its name, and so do `takeOver`, `workOnWindow`
  and `taken` in windows.go. A command id is what a saved shortcut
  points at, so renaming one breaks a shortcuts file that names it.
  Worth doing with the keyboard config, which is the other thing that
  has to be able to move an id.

- **A window too small for the switcher closes it rather than saying
  so.** Shrinking past the size it would open at now takes it away,
  which is better than the row of empty tiles it used to leave but is
  still abrupt: nudge an edge one column too far and the switcher
  vanishes. Saying "the window is too small to draw every pane at once"
  in its place, and coming back when there is room again, would be
  kinder. Worth doing with the animation work, which is the other thing
  that wants this dialog opened and closed smoothly.

- **Closing the switcher takes any dialog stacked over it.** Hiding a
  modal pops everything above it, and the switcher is now closed from
  the frame when the window is too small. So nudging an edge one column
  too far also dismisses, say, a secret an agent asked for that arrived
  over the tiles. The asker is told, so nothing is left waiting, but the
  user loses a prompt they never answered. Goes with the wording above.

## Panes and the sidebar

- **A pane on a taken-over window cannot be reconnected.** Marcus typed
  `exit` in a pane opened through a remote gridterm window. The shell
  ended and the pane said "-- gridterm: the program has finished.
  ctrl+shift+W closes this pane. --" with no "Reconnect?" question, no
  cross on the row and no context menu. "Clear finished connections" was
  the way out.
  - **Why nothing is asked.** `startedAs.again` is false for a pane
    drawn from a window taken over, because the program belongs to the
    other window. So `askWhatNext` asks nothing, and the line naming a
    key combination is the fallback.
  - **What it needs.** The window that took over has to be able to say
    "open another shell there, into this pane" to the window it is
    serving from, the way `startAgainOn` says it to a machine. That is a
    new message on the wire, not a flag to flip.
  - **Getting rid of it is answered** by the cross on hover. See the
    answers at the top.

- **A file browser's pane is named differently from a terminal's.** Not
  unnamed: `Pane.Draw` in ui/files/pane.go already writes the machine in
  bold on its first row, with the directory under it. What it does not
  have is the window's own caption line, because `refreshCaptions` walks
  `a.panes` and a browser pane is not a terminal.

  So the information is there and the shape is not: a split holding a
  shell and a browser has the shell's caption row above it and the
  browser's own header inside it, which read as two different things.
  Worth settling as one or the other rather than adding a second header.

- **A pane drawn on a layer of its own shows no line.** A held screen
  too big for its room is painted by the window rather than by the tree,
  and that painting draws the screen alone. The pane keeps its row for
  the screen instead, so nothing is cut; there is simply nothing to read
  on it.

## The file viewer

- **A file being read says how far it has got.** Done on 2026-09-20,
  from Marcus's inbox: a large file showed "empty" in the corner with
  nothing to say a read was out. It now counts up, "reading 1.9 of
  4.2 MB…", falling back to what has arrived when the file grew past
  the size it was listed at, to the size alone before the first bytes
  land, and to "reading…" when nobody knew either.

  How it hangs together: `ReadFileWatched` tells a watcher the running
  total from the goroutine doing the reading, `app.watchRead` hands
  that to the drawing goroutine through the pump no more often than
  every 100ms, and `Reader.ReadSoFar` takes it. The total comes from
  the listing the browser already drew, so nothing asks the machine how
  big the file is before reading it.

  Still open from the same note: a scrollbar minimap, and a JSON log
  viewer like the one in particleview5-bugreport-viewer.

- **A picture says how big it is but not how far it has got.**
  `ReadPicture` decodes in one go rather than reading in chunks, so
  there is nothing to count. A picture is capped at a size a pane can
  draw, so the wait is shorter than a log file's, and nobody has asked.

## The log pane

Asked for on 2026-09-20, after deciding a closed pane's failure should
go to the log. "Show what the window has logged" on the Help menu opens
a pane on it, or goes to the one already open. It is an ordinary
terminal, so the scrollback, the selection and the copy key are the
ones the user already knows, and nothing typed into it reaches
anything.

The lines still go to stderr as well. What is new is that they are
kept, which is the only way to read them in a window started from
Explorer, where there is no console for stderr to reach.

What is left:

- **It holds the last two thousand lines and nothing older.** A reader
  that falls behind is told how many it missed rather than being handed
  a gap, but they are gone. Writing to a file as well is the answer if
  anybody wants one, and that is a file holding whatever the window
  logged, which is worth deciding on before writing one.

- **Nothing can be searched.** Marcus's own note asks for searching a
  pane's scrollback through the file viewer, and the log is the first
  pane anybody will want to search. The two go together.

- **The log goes when the window does.** It is in memory, like the
  record of what an agent typed, and for the same reason.

- **Only lines the window logs are in it.** What a connection printed
  while it was being made goes to its own log, which "Show how this
  was reached" already shows. The two are not joined up.

## Known gaps worth revisiting

- **The help dialog outgrew a 90-row window.** Adding one command
  pushed the file browser's keys past the bottom. The dialog scrolls,
  so nothing is lost to a user, but a test reading the screen needs a
  taller window every time a command is added. The list is long enough
  now to want sections that fold.

- **The dialog shadow only reads where something light sits behind
  it.** Looked at on 2026-09-19 through `-shot`, over a screenful of
  text. It draws: the row under the panel keeps 89% of its brightness
  at the far edge of the fade, more nearer the panel. Over the window's
  own black background it is invisible, because a black shadow on black
  is nothing. Whether that is subtle enough or too subtle is Marcus's
  call. `frostSpread`, `frostDropCols` and `frostDropRows` in modals.go
  are the numbers.

- **A frame that draws the glass costs 51 allocations, all inside
  ebiten.** Measured on 2026-09-19 with `BenchmarkRenderingFrame`. A
  busy frame with no dialog costs 7, and a settled frame none at all,
  because the compositor skips it. The glass is five draw calls -- a
  clear, a copy, two blur passes, the panel, the shadow -- and ebiten
  allocates vertex buffers and a `SubImage` wrapper for each. Nothing of
  gridterm's own is left in that path.

- **The pane switcher is the last thing that allocates on a settled
  frame.** Nine allocations and 384 bytes, measured on 2026-09-19 with
  `BenchmarkSwitcherFrame`. An idle frame and one with a dialog up are
  both at none, and `TestAFrameThatChangedNothingAllocatesNothing` keeps
  them there. Nobody has looked at the switcher's nine.

- **A shared pane never lets the window idle.** The border and the
  sidebar stripe glow for as long as an agent or another window has the
  pane, so two layers repaint four times a second and the compositor
  never takes the skip. The connection pulse settles four seconds after
  the last byte and this does not, because a glow that stops is not a
  glow. `TestASharedPaneCostsNothingBetweenGlowSteps` pins the cost at
  two layers a step, so it cannot grow unnoticed.

- **A folder holding a comma cannot be typed in the server dialog.** The
  folders are one field and a comma parts them, so a path with one in it
  can only be written in the server list file by hand. The dialog does
  not lose it: a save that did not touch the field writes the folders
  back as they were, and one that did is refused. A separator no path can
  hold would need a field of more than one line, which the form has no
  widget for.

- **The plus offers a line per folder rather than a submenu.** The list
  asked for a submenu under "Files". `ui.MenuItem` holds a command and a
  title and nothing else, so nesting is not something the menus can draw
  yet. The lines sit beside "Files" instead, which keeps the way to
  home.

- **There is no way to take a key out of the list.** Keys go in when one
  is made and when one is typed into a server, and the list is capped at
  twenty, so a path typed wrong stays in the list the dialog steps
  through until twenty more push it out.

- **A new key is only as private as its directory on Windows.** A file
  mode says nothing there, so `0600` on the private half is a no-op and
  the key takes the directory's own permissions. `MakeKey` insists on a
  full path, which stops a key landing wherever gridterm was started,
  and that is the whole of what it can do. Refusing a path outside the
  user's own profile would be the next step.

- **The keyboard shortcuts file is read once, at startup.** Colour
  themes have a "Reload", and this does not. Applying the changes again
  on top of a keymap they have already changed would not give a deleted
  line's built-in chord back, so a real reload has to build the default
  keymap from scratch first. That means pulling the `MustBind` block out
  of `commands()` in app.go into a function of its own.

- **The shortcuts file reaches the window's shortcuts only.** The file
  browser's own keys are fixed in `ui/files/keybar.go`, and "Keys and
  commands" lists both kinds. The heading says which is which, and that
  is the whole of what it does about it.

- **A starting file is written straight to its own name.** Both
  `keys.WriteStart` and `themes.WriteStart` create the real file and
  then fill it, so a write that fails part way leaves a short file that
  the next attempt then refuses to write over, because it is already
  there. `remote.MakeKey` shows the pattern to follow: write a file of
  its own, flush it, and link it into place.

- **A Windows build for arm64 gets no icon of its own.** `go build`
  links a resource by its filename, and the one checked in is
  `rsrc_windows_amd64.syso`. `.gitignore` names `gridterm-arm64.exe`, so
  somebody builds that by hand and it shows the default icon. One more
  `rsrc -arch arm64` line in the Makefile's icon target closes it.

- **The icon target still needs the network the first time.** It runs
  `rsrc` through `go run …@v0.10.2`, which downloads the module. `rsrc`
  only wraps the icon in a one-section COFF object, so `internal/mkico`
  could write the `.syso` itself: that would drop the download, the
  temporary file and the whole second command, and would let the drift
  test compare byte for byte instead of looking for the images inside.
  About 120 lines of COFF writing.

- **Nothing past openWindow can be tested.** `sizeTheWindow` tells the
  window system how big to open and which icon to use, and cutting
  `ebiten.SetWindowIcon` out of it goes unnoticed. Everything in there
  is an ebiten call that needs a real window, so pinning it would mean
  a seam per call for no gain: a window with no icon is seen the moment
  it opens.

- **A flaky test makes a mutation sweep lie.** Two of the cuts above
  were reported as caught, and the only test that caught them was
  `TestKickingAWindowThatHasAlreadyGoneSaysNothing`, which was failing
  on its own. A sweep that counts a flake as a kill says the code is
  pinned when nothing pins it. That one was the window's fault and is
  fixed: it now passes sixty runs in sixty. The ones below are still
  worth fixing for the same reason as much as for the noise.

- **`TestClosingAPaneLeavesDetachedWorkRunning` fails now and then under
  load.** It says `the wait ended with 0x0 inside 500ms` at
  session/job_windows_test.go. Not reproduced on 2026-09-20 in forty
  runs of it alone and eighteen of the whole package, both under load
  from other packages, so there is still no rate to quote.

  The earlier note said the half-second budget was to blame. It is
  not: 0x0 is WAIT_OBJECT_0, which means the ping this test leaves
  running had already gone, and a shorter budget would only hide a
  real kill. The failure now prints the ping's exit code as well,
  because that is what tells a kill from an end of its own, and that
  is the next thing to read when it happens again.

- **A row for a shell a client started does not say which client.**
  Every one reads "started from another window", so a host with two
  windows connected sees rows it cannot tell apart, while the row above
  them names the client and its address. `serve.Config.Open` is
  `func(cols, rows int)` and does not carry the client, so naming one
  means changing that signature.

- **Whether a window was serving is one flag for the whole user.** The
  settings file is shared, so two gridterms running at once write over
  each other's answer, and the offer at the next start is whichever one
  closed last. Per-window would need a name for a window, which nothing
  has yet.

- **The pane switcher draws every pane twice while it is open.** The
  tiles cover the window, but the tree underneath still draws each pane
  into the window's own grid, and then each one is drawn again into its
  tile. `placeScaled` avoids this with `SetElsewhere`, which the
  switcher cannot use without taking that flag off it. An idle window
  still skips its frames; it is a busy one that pays twice.

- **The pane switcher names a pane that has closed since it opened.**
  The tiles are laid out when it opens and the names go with them, so a
  pane that closes while it is up loses its picture and keeps its name.
  Picking that tile does nothing. Laying the tiles out again would move
  every other one under the user's eye, which is worse; greying the name
  would be better than both.

- **The record of what an agent typed lives only as long as the
  window.** It is in memory, capped at two thousand sends or a quarter
  of a megabyte a pane, and it goes when the pane closes. A user who
  wants to go back over a run after closing gridterm has nothing. A
  file would hold it, and would then be a file holding whatever an
  agent typed, which is worth deciding on before writing one.

- **A serving window carried on a USB stick cannot start.** `makeHostKey`
  links the key into place rather than renaming it, so two windows
  cannot write over each other's. FAT32 and exFAT have no hard links, so
  on a stick the link fails with `ERROR_INVALID_FUNCTION`, which is not
  `os.ErrExist`, and serving is refused every time. A stick is the
  obvious place for a copy that carries its own files. The fix is to
  claim the name with `O_CREATE|O_EXCL` and then rename onto it when the
  filesystem has no links, which keeps the same guarantee by a different
  means. Not built, because it changes a write path the window's identity
  hangs on.

- **A copy carrying its own files does not carry its SSH keys.** A key
  gridterm makes goes to `~/.ssh/id_ed25519_gridterm`, and `known_hosts`
  stays in `~/.ssh`, so gridterm and `ssh` agree on both. A copy moved to
  another machine therefore has saved servers naming key files that are
  not there, and every machine prompts as new. The notice says so; there
  is nothing that moves them.

- **The key a carried copy serves with is only as private as its
  directory.** Windows ignores the mode on the file, and `os.MkdirAll`
  ignores the mode on the directory, so a copy under `C:\Program Files`
  or on a share hands the key to every account that can read the path.
  Anybody holding it can pretend to be this window. The notice says to
  keep the directory somewhere only you can read, and that is the whole
  of what gridterm does about it. Same class as the key line above.

- **A pane watched over the wire keeps the host's named colours.** The
  window the pane belongs to sends its screen again when the theme
  changes, so a watcher sees the change at once. What it sees is a mix.
  The wire leaves the default ground and text out, so those take the
  watcher's own theme. Everything the program named is carried as a
  resolved colour, so the reds and blues stay the host's. Carrying the
  entry a colour came from over the wire would close it.

- An orphaned `conhost.exe` can be left with no `cmd.exe` under it. Seen
  on a live window while the shell leak was being looked into: the shell
  had exited and the console host was still running. The job object put
  in for that leak does not cover it, because the console host is not in
  the job.

- A shell can start something in the microseconds between
  `CreateProcess` and the job assignment, and that escapes the job for
  good. Starting the shell suspended would close it, and go-pty closes
  the thread handle, so there is no way to resume it without patching
  go-pty.

- One connection to the window answers one question at a time, so a long
  `wait_for` or an `ask_for_secret` that is waiting for the user stops
  that agent asking anything else until it ends. The tools say so, and
  an agent that wants both opens a second connection with the same code.
  Giving requests ids so one connection can carry several at once is the
  real answer, and it is a protocol change.

- A WSL pane does not start in the directory the window is looking at.
  `shells.Shell.Command` translates a Windows directory into a `/mnt`
  path and passes it as `--cd`, and `shells.UnixPath` is tested, but
  the window passes an empty directory at all three call sites in
  shellpick.go, so `--cd` is never sent. Nothing in the window tracks a
  pane's working directory yet. The file browser knows one, through
  `files.Pane.At()`, and no command opens a terminal from it. The
  working directory field under "Run a command" wants the same thing.

- `TestKickingAWindowThatHasAlreadyGoneSaysNothing` in status_test.go
  was failing with "focus never reached the Kick marcus@laptop out
  button", and the window was at fault rather than the test: a window
  that let go arrives on Windows as a Winsock reset often enough to
  see, that was reported as the connection being lost, and the notice
  sat over the dialog the test was tabbing through. Fixed, and it now
  passes sixty runs in sixty. Both these tests are relay tests over
  loopback SSH, and both failed on their own rather than only under the
  load of a whole suite, so a sweep running one package was not safe
  from them.

- The single-window tests still read `testApp.shells` off the lock the
  harness appends under. Safe today, because every append in those tests
  is on the test's own goroutine. It stops being safe the moment one of
  them opens a second window, and nothing says so at the call site.
  `go test -race` was failing at five places for this reason, all of
  them in tests where a client attaching made the server's goroutine
  append. Those now read through `testApp.shell` and
  `testApp.shellCount`, which take `shellsMu`, and the suite passes
  under `-race`. The remaining direct reads are in panes_test.go,
  agents_test.go and agentkeys_test.go.

- The cursor keeps blinking while the window is in the background. Most
  terminals either stop the blink or draw the cursor hollow once the
  window loses focus. `updatePointer` already reads `ebiten.IsFocused`
  once a frame, so the answer is there to read.
- A click that brings the window to the front is acted on as well. A
  press that moves the keys to a pane stops there, through
  `ui.FocusesFirst`, and the window does not do the same for itself: it
  reads `ebiten.IsFocused` in `updatePointer` but only to decide whether
  the pointer is on the window, so the click that woke it looks like any
  other. A headless test cannot drive that flag either.
- A watcher taking a screen over sees the far end's default cursor.
  `vt.Repaint` puts the cursor back where the program had it but carries
  neither its shape nor its blink, so `DECSCUSR` is lost over the wire.

## Asked for, not yet worked out

Marcus's own list, in his words, kept until each has been looked at
properly and either written up above or done.

- **An Open menu of its own.** A menu listing the saved servers without
  the "Connect to" in front of each one, and the local ways in beside
  them, under a "Server" heading and a "Local" heading. `ui.MenuItem`
  holds a command and a title and nothing else, so a heading is a kind
  of item the menus cannot draw yet. The same gap stopped the plus
  offering a submenu, further up this file.

- **Context menu.** Make use of one. On the sidebar it opens the menu
  that is already there.

  A context menu is a `ui.Menu` anchored at the pointer. Everything it
  needs is already there: `ui.NewMenu` takes command ids, `menu.Anchor`
  is a function returning a rectangle, and `a.showModal` puts it up. The
  plus on a machine heading does exactly this, anchored at a row instead
  of a cell.

  Where a right click can land, and what is there today:

  - A terminal pane. Nothing: the right button falls through
    (ui/term/term.go:943).
  - A machine heading on the sidebar. Its plus already opens a menu, so
    this is routing and no new lines.
  - A connection row on the sidebar. Nothing. Its button is a cross that
    closes the pane or clears a finished row, not a plus, so there is no
    menu to open.
  - A file browser pane. Its operations live on its key bar rather than
    in a menu.
  - A split divider, the menu bar, the ground between tiles. Nothing.

  The press reaches `a.root.HandleMouse` and walks the tree. Widgets
  return false for the right button today, so the window can take it
  after the tree has declined and work out what is under the pointer the
  same way the pointer shape is chosen.

  The one hard part is a program that owns the mouse. vim, mc and htop
  turn mouse reporting on, and the right button is then theirs
  (ui/term/term.go:909). Selection already answers this: hold Shift and
  the program is bypassed. The menu should follow that rule rather than
  invent a second one.
