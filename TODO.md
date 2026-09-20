# To do

Things Marcus has asked for that are not done yet.

A line goes when the work is in and reviewed. What was done is in the
commits; this file is only what is left. Decisions go under "Settled"
rather than being written up here, so they are not argued twice.

Arranged as: what is next, what waits on an answer, the open work by
subject, the gaps nobody has asked about yet, and the settled list.

## Next, in Marcus's order

From 2026-09-19. Bugs and small items come first, and then these.

1. **The file viewer.** Its own section below.
2. **Pasting an image into a terminal.** Done on Windows; the rest is
   under "Pictures, pasting and dropping".

## Deferred: the context menu

Marcus asked on 2026-09-19 to leave it until the bugs and the small
items are done, and said so again on 2026-09-20. It waits on the six
questions below. Nothing else blocks it: the modifier keys reach the
mouse now, so Shift+right-click works whichever way he answers. The
plan is under "Asked for, not yet worked out".

## Waiting on an answer from Marcus

All six are about the context menu.

- **Right click in a pane where a program owns the mouse.** vim, mc and
  htop ask for the mouse, and then the right button is theirs. The
  window already has a rule: hold Shift and the program is bypassed,
  which is how a selection is made over one. Should the menu follow
  that rule, so Shift+right-click opens it and a plain right click goes
  to the program? The other way is for the menu to win always.

- **A connection row on the sidebar has no menu to open.** The plus on
  a machine heading opens one; a row's button is a cross that closes
  it. Should a right click on a row open a new menu, and what goes on
  it: go to it, close it, share it with an agent, what the agent typed?
  Or should a row have no menu?

- **Which pane does the menu act on?** A right click on a pane that is
  not in front could focus it first, so "Close pane" cannot close a
  pane the user was not pointing at. Or the menu could act on the pane
  in front whatever was clicked. The first is what most windows do.

- **What goes on a terminal's menu?** Suggested, in this order: Copy,
  Paste, a rule, Split right, Split down, Take this pane out of its
  split, a rule, Share this pane with an agent, Close pane. That is
  nine lines, which may be too many to read at a glance.

- **The file browser.** Its operations are on the key bar: rename,
  copy, cut, paste, delete, make a directory. Should a right click
  offer those, and should it move the selection to the item under the
  pointer first?

- **A keyboard way in?** Shift+F10 and the Menu key are what other
  windows use. Worth binding, or is the palette enough?

# Open work

## Hyperlinks

Done: OSC 8, a bare address found in ordinary output, the link under
the pointer underlined, and where it goes written along a row while
ctrl is held. What is left:

- **A link is only found on one row.** An address the shell wrapped
  across two is two halves, and neither is followed. Joining them
  means knowing a row was wrapped rather than ended, which the
  emulator knows and the grid does not carry.

- **The scrollback is not searched for links, only the screen.** A
  link scrolled off is gone even though the text is still there.
  `linkSpanAt` reads the rendered grid, which is the screen.

- **Nothing opens a file path.** A build error naming
  `src/main.go:42` is the thing a user most wants to click, and it is
  not an address: it needs a rule for what a path looks like, and an
  answer to what clicking one should do.

## The file viewer

- **The scrollback viewer shows no colours.** Marcus asked for the
  terminal buffer "keeping its colors", and a reader draws a file in
  one style. Carrying the colours means the reader taking styled cells
  rather than lines, which is a change to how it draws rather than to
  what it is given. Finding a line is what the viewer is for, and that
  works.

- **A scrollbar minimap.** Marcus's own note, 2026-09-20.

- **A JSON log viewer**, inspired by the one in
  `G:\Workspace\particleview5-bugreport-viewer`. Marcus's own note,
  2026-09-20.

- **A picture's count stops while it is decoded.** The read is counted
  and the decode is not, so a very large picture sits at its full size
  for a second or two at the end. Saying "decoding" there means telling
  the reader which half it is in.

- **A drag held still past the edge of a field does not keep
  scrolling.** It picks out as far as the text that is shown and waits
  for the pointer to move again, because the field only hears about a
  move. From the selection review of 2026-09-19.

## The log pane

- **It holds the last two thousand lines and nothing older.** A reader
  that falls behind is told how many it missed rather than handed a
  gap, but they are gone. A file would hold them, and would then be a
  file holding whatever the window logged, which is worth deciding on
  before writing one.

- **Nothing can be searched.** Marcus's own note asks for searching a
  pane's scrollback through the file viewer, and the log is the first
  pane anybody will want to search. The two go together.

- **The log goes when the window does.** In memory, like the record of
  what an agent typed, and for the same reason.

- **Only lines the window logs are in it.** What a connection printed
  while it was being made goes to its own log, which "Show how this was
  reached" shows. The two are not joined up.

## Pictures, pasting and dropping

Asked for on 2026-09-17. Pasting works on Windows: paste hands the
picture over by whichever route reaches the program, and
`edit.pasteImage` on Ctrl+Alt+V writes a file wherever the pane is and
types the path.

- **Only Windows reads a picture off the clipboard.** Everywhere else
  the command says there is none. Linux and macOS each need a reader.

- **Nothing clears the pictures off a machine reached by SSH.** They go
  under `gridterm-pasted` in the home directory of whoever the
  connection logs in as, and stay there.

- **Nothing takes the local files away either.** They pile up in the
  temporary directory under `gridterm-pasted` until the system clears
  it.

- **A dropped file always goes under home.** Same place, same problem.
  Marcus may want to choose where instead: the directory a browser pane
  is showing on that machine is the obvious second answer.

- **A dropped file is copied even when the pane is on a window this one
  is connected to.** It goes through that window's files, which is
  right, but a gridterm at the far end could take it down the clipboard
  channel instead. Worth nothing until somebody wants it.

## Panes and the sidebar

- **A pane on a taken-over window cannot be reconnected.** Marcus typed
  `exit` in a pane opened through a remote gridterm window. The shell
  ended and the pane said "the program has finished" with no
  "Reconnect?", no cross on the row and no menu; "Clear finished
  connections" was the way out. `startedAs.again` is false for such a
  pane, because the program belongs to the other window, so
  `askWhatNext` asks nothing. The window that took over has to be able
  to say "open another shell there, into this pane" to the window it is
  serving from, the way `startAgainOn` says it to a machine. That is a
  new message on the wire, not a flag to flip.

- **A file browser's pane is named differently from a terminal's.**
  `Pane.Draw` writes the machine in bold on its first row with the
  directory under it, but there is no caption line, because
  `refreshCaptions` walks `a.panes` and a browser pane is not a
  terminal. A split holding a shell and a browser therefore has the
  shell's caption above it and the browser's own header inside it,
  which read as two different things. Worth settling as one or the
  other rather than adding a second header.

- **A pane drawn on a layer of its own shows no caption.** A held
  screen too big for its room is painted by the window rather than by
  the tree, and that painting draws the screen alone. The pane keeps
  its sidebar row, so nothing is lost; there is simply nothing on it.

- **A file manager is entered twice per lap of the sidebar.** Each of
  its panes has a row, under whichever machine that pane reads, so a
  browser with a local pane and a remote one is two stops far apart.
  Two of the presses only move the highlight inside a browser already
  in front, which reads as the key doing nothing. Ctrl+PageUp and
  Ctrl+PageDown, not Ctrl+Tab.

- **The sidebar's order goes stale while it is shut.** `refreshPanel`
  stops building rows when the dock is collapsed, so the walk follows
  the last order it saw with panes opened since on the end. Everything
  stays reachable, and nothing on screen contradicts it.

- **Two panes can read the same in the switcher's list.** Two fresh
  shells on this machine whose programs have set no title are both
  "PowerShell on this machine". Which one the walk is on is marked, so
  nothing is ambiguous about where it lands.

- **A window too small for the switcher closes it rather than saying
  so.** Nudge an edge one column too far and it vanishes. Saying "the
  window is too small to draw every pane at once" instead, and coming
  back when there is room, would be kinder. Worth doing with the
  animation work, which wants this dialog opened and closed smoothly.

- **Closing the switcher takes any dialog stacked over it.** Hiding a
  modal pops everything above it, and the switcher is closed from the
  frame when the window is too small. So nudging an edge too far also
  dismisses, say, a secret an agent asked for. The asker is told, so
  nothing waits for ever, but the user loses a prompt they never
  answered. Goes with the line above.

## Showing which panes are shared

Asked for on 2026-09-17. A pane handed to an agent or driven from
another window looks like every other pane.

- **A chip in the top right saying what is happening.** "Agent
  connected", "Agent running: ls -la", with a long command cropped.
  Clicking it opens the dialog that can end it. Two chips when both are
  in force, one per border. The half that names the command can use the
  record of what the agent typed, though the last line sent is not
  always the command that is running.

- **A nicer account of what an agent did, built from what the pane
  echoed.** The line a shell prints back is the command it is about to
  run, after completion and after editing. It is already on screen, so
  showing it gives away nothing a user could not read by scrolling, and
  a password at a prompt that does not echo never appears.
  `handover.markPrompt` in agents.go writes down the prompt and the
  line it was typed at, which is where such a reader starts, and it
  already says nothing on the alternate screen.

  **The open question is when to read the echo.** It lands after the
  send has returned, so something has to look afterwards: the next call
  the agent makes, or a frame while the pane is handed over.

- **A pane on a window taken over from elsewhere gets no border.** The
  border reads `pane.Watched()` and the handover list, and the pane
  drawn on the window that took over has neither: what is shared is the
  pane on the other machine. So the window driving marks nothing and
  the window being driven marks everything. Worth deciding whether the
  driving window should mark it too.

## Keyboard and shortcuts

- **The code still says "take over" where the user reads "connect
  to".** `serve.takeOver`, `takeOver`, `workOnWindow` and `taken` in
  windows.go. Nothing blocks the rename now: `keys.Renamed` follows a
  command id that has moved, so a saved shortcut naming the old one
  goes on working. It is a rename nobody has done yet.

- **Only the font size goes by the character a key produces.** The
  punctuation keys are read from what the layout says they print, so
  plus is plus wherever it sits. Everything else is read from where a
  key sits, which is right for a letter and a function key, and worth
  revisiting if a shortcut on a letter ever lands somewhere awkward.

- **The shortcuts file reaches the window's shortcuts only.** The file
  browser's own keys are fixed in `ui/files/keybar.go`. "Keys and
  commands" lists both kinds and says which is which, and that is the
  whole of what it does about it.

## Release, versions and documentation

Asked for on 2026-09-19.

- **Binaries** for Windows and Linux, and macOS if it is easy.

- **Two version numbers, not one:** what the program calls itself, and
  what the wire between two windows calls itself, so a window can tell
  a build it cannot talk to. Nothing in the protocol carries a version
  today, and `openSession` is positional, so a build that disagrees is
  refused by a parse failure rather than by a number.

- **A README worth reading with it.** It says nothing about changing a
  keyboard shortcut, which is the example Marcus gave: where the file
  is, that it holds changes rather than the whole map, that a moved
  shortcut takes two lines, and how to write a chord. The same for the
  colour themes, the server list, and what a copy carrying its own
  files is for. Some of it is already written inside the window, in
  "Keys and commands" and "Where gridterm keeps its files".

# Known gaps

Nobody has asked for these. They are written down so they are not
rediscovered.

## Tests

- **`TestClosingAPaneLeavesDetachedWorkRunning` fails now and then
  under load**, saying `the wait ended with 0x0 inside 500ms` in
  session/job_windows_test.go. Not reproduced on 2026-09-20 in forty
  runs alone and eighteen of the whole package under load, so there is
  no rate to quote. It is not the half-second budget: 0x0 is
  WAIT_OBJECT_0, so the ping had already gone, and a shorter budget
  would only hide a real kill. The failure now prints the ping's exit
  code, which is what tells a kill from an end of its own, and that is
  the next thing to read. It is a relay test over loopback SSH, and it
  has failed on its own rather than only under a whole suite.

- **The single-window tests read `testApp.shells` off the lock the
  harness appends under.** Safe today, because every append in them is
  on the test's own goroutine, and it stops being safe the moment one
  opens a second window. The reads that mattered now go through
  `testApp.shell` and `testApp.shellCount`, which take `shellsMu`. The
  remaining direct reads are in panes_test.go, agents_test.go and
  agentkeys_test.go.

- **Nothing past `openWindow` can be tested.** `sizeTheWindow` tells
  the window system how big to open and which icon to use, and cutting
  `ebiten.SetWindowIcon` out of it goes unnoticed. Everything in there
  is an ebiten call needing a real window, so pinning it would mean a
  seam per call for no gain: a window with no icon is seen at once.

- **The help dialog outgrew a 90-row test window.** Adding one command
  pushed the file browser's keys past the bottom. The dialog scrolls,
  so a user loses nothing, but a test reading the screen needs a taller
  window every time a command is added. The list is long enough now to
  want sections that fold.

## Windows and packaging

- **A Windows build for arm64 gets no icon.** `go build` links a
  resource by filename and the one checked in is
  `rsrc_windows_amd64.syso`. `.gitignore` names `gridterm-arm64.exe`,
  so somebody builds that by hand and it shows the default icon. One
  more `rsrc -arch arm64` line in the Makefile's icon target closes it.

- **The icon target needs the network the first time.** It runs `rsrc`
  through `go run …@v0.10.2`. `rsrc` only wraps the icon in a
  one-section COFF object, so `internal/mkico` could write the `.syso`
  itself: that drops the download, the temporary file and the second
  command, and lets the drift test compare byte for byte. About 120
  lines of COFF writing.

- **An orphaned `conhost.exe` can be left with no `cmd.exe` under it.**
  Seen on a live window: the shell had exited and the console host was
  still running. The job object does not cover it, because the console
  host is not in the job.

- **A shell can start something in the microseconds between
  `CreateProcess` and the job assignment**, and that escapes the job
  for good. Starting the shell suspended would close it, and go-pty
  closes the thread handle, so there is no way to resume it without
  patching go-pty.

- **A shell that sends no OSC 7 starts a new pane nowhere
  particular.** The window knows where a pane is only because the
  shell says so, and nothing says so by default on Windows: PowerShell
  needs a prompt function that writes OSC 7, and cmd.exe cannot send
  one at all. bash, zsh and fish on Linux mostly do it out of the box.
  A pane that says nothing opens the next one wherever a shell starts,
  which is what happened before. Worth a line in the README when the
  documentation is written.

- **Opening a terminal from a file browser pane does not use the
  directory it is showing.** `files.Pane.At()` knows one and no
  command opens a terminal from it. The working directory field under
  "Run a command" wants the same thing.

## Keys, files and privacy

- **A serving window carried on a USB stick cannot start.**
  `makeHostKey` links the key into place rather than renaming it, so
  two windows cannot write over each other's. FAT32 and exFAT have no
  hard links, so the link fails with `ERROR_INVALID_FUNCTION`, which is
  not `os.ErrExist`, and serving is refused every time. A stick is the
  obvious place for a copy carrying its own files. The fix is to claim
  the name with `O_CREATE|O_EXCL` and rename onto it where the
  filesystem has no links, which keeps the same guarantee by other
  means. Not built, because it changes a write path the window's
  identity hangs on.

- **A copy carrying its own files does not carry its SSH keys.** A key
  gridterm makes goes to `~/.ssh/id_ed25519_gridterm` and `known_hosts`
  stays in `~/.ssh`, so gridterm and `ssh` agree on both. A copy moved
  to another machine has saved servers naming key files that are not
  there, and every machine prompts as new. The notice says so.

- **A new key is only as private as its directory on Windows.** A file
  mode says nothing there, so `0600` on the private half is a no-op.
  `MakeKey` insists on a full path, which stops a key landing wherever
  gridterm was started, and that is the whole of what it can do.
  Refusing a path outside the user's own profile would be next.

- **The key a carried copy serves with, likewise.** `os.MkdirAll`
  ignores the mode too, so a copy under `C:\Program Files` or on a
  share hands the key to every account that can read the path. Anybody
  holding it can pretend to be this window. The notice says to keep the
  directory somewhere only you can read.

- **A starting file is written straight to its own name.** Both
  `keys.WriteStart` and `themes.WriteStart` create the real file and
  then fill it, so a write that fails part way leaves a short file the
  next attempt refuses to write over. `remote.MakeKey` shows the
  pattern: write a file of its own, flush it, link it into place.

- **The record of what an agent typed lives only as long as the
  window.** In memory, capped at two thousand sends or a quarter of a
  megabyte a pane, and gone when the pane closes. A file would hold it,
  and would then be a file holding whatever an agent typed, which is
  worth deciding on before writing one.

## Drawing and what it costs

- **A shared pane never lets the window idle.** The border and the
  sidebar stripe glow for as long as an agent or another window has the
  pane, so two layers repaint four times a second and the compositor
  never takes the skip. The connection pulse settles four seconds after
  the last byte and this does not, because a glow that stops is not a
  glow. `TestASharedPaneCostsNothingBetweenGlowSteps` pins the cost at
  two layers a step.

- **A frame that draws the glass costs 51 allocations, all inside
  ebiten.** `BenchmarkRenderingFrame`, 2026-09-19. A busy frame with no
  dialog costs 7 and a settled frame none. The glass is five draw calls
  and ebiten allocates vertex buffers and a `SubImage` wrapper for
  each. Nothing of gridterm's own is left in that path.

- **The pane switcher allocates nine times on a settled frame**, 384
  bytes, `BenchmarkSwitcherFrame`. An idle frame and one with a dialog
  are both at none. Nobody has looked at the nine.

- **The pane switcher draws every pane twice while it is open.** The
  tiles cover the window, but the tree underneath still draws each pane
  into the window's grid before each is drawn again into its tile.
  `placeScaled` avoids this with `SetElsewhere`, which the switcher
  cannot use without taking that flag off it. An idle window still
  skips its frames; a busy one pays twice.

- **The pane switcher names a pane that has closed since it opened.**
  The tiles are laid out when it opens and the names go with them, so a
  pane that closes while it is up loses its picture and keeps its name,
  and picking that tile does nothing. Laying out again would move every
  other tile under the user's eye; greying the name is better than
  both.

- **The dialog shadow only reads where something light sits behind
  it.** The row under the panel keeps 89% of its brightness at the far
  edge of the fade. Over the window's own black it is invisible,
  because a black shadow on black is nothing. Whether that is too
  subtle is Marcus's call. `frostSpread`, `frostDropCols` and
  `frostDropRows` in modals.go are the numbers.

- **The cursor keeps blinking while the window is in the background.**
  Most terminals stop the blink or draw the cursor hollow once the
  window loses focus. `updatePointer` already reads `ebiten.IsFocused`
  once a frame, so the answer is there to read.

- **A click that brings the window to the front is acted on as well.**
  A press that moves the keys to a pane stops there, through
  `ui.FocusesFirst`, and the window does not do the same for itself. A
  headless test cannot drive `ebiten.IsFocused` either.

## Dialogs, menus and the server list

- **A folder holding a comma cannot be typed in the server dialog.**
  The folders are one field and a comma parts them, so such a path can
  only be written in the server list file by hand. The dialog does not
  lose it: a save that did not touch the field writes the folders back
  as they were, and one that did is refused. A separator no path can
  hold needs a field of more than one line, which the form has no
  widget for.

- **The plus offers a line per folder rather than a submenu.**
  `ui.MenuItem` holds a command and a title and nothing else, so
  nesting is not something the menus can draw. The same gap stops an
  Open menu having headings.

- **There is no way to take a key out of the list.** Keys go in when
  one is made and when one is typed into a server, and the list is
  capped at twenty, so a path typed wrong stays until twenty more push
  it out.

## The wire between windows

- **A row for a shell a client started does not say which client.**
  Every one reads "started from another window", so a host with two
  windows connected sees rows it cannot tell apart, while the row above
  them names the client and its address. `serve.Config.Open` is
  `func(cols, rows int)` and does not carry the client.

- **Whether a window was serving is one flag for the whole user.** The
  settings file is shared, so two gridterms running at once write over
  each other's answer and the offer at the next start is whichever
  closed last. Per-window would need a name for a window.

- **A pane watched over the wire keeps the host's named colours.** The
  wire leaves the default ground and text out, so those take the
  watcher's own theme, while everything the program named is carried as
  a resolved colour. Carrying the entry a colour came from would close
  it.

- **A watcher taking a screen over sees the far end's default cursor.**
  `vt.Repaint` puts the cursor back where the program had it but
  carries neither shape nor blink, so `DECSCUSR` is lost.

- **One connection to the window answers one question at a time**, so a
  long `wait_for` or an `ask_for_secret` waiting on the user stops that
  agent asking anything else. The tools say so, and an agent that wants
  both opens a second connection with the same code. Giving requests
  ids so one connection can carry several is the real answer, and it is
  a protocol change.

# Settled, do not re-open

- **A failure belonging to a pane the user has closed goes to the log,
  not to a dialog** (2026-09-20). It failed long after the user stopped
  waiting, and the dialog took their next click. `reportForPane` shows
  it only while the pane is open. The error is not dropped: "Show what
  the window has logged" is where it goes.

- **The file viewer stays a viewer, with no caret** (2026-09-20). A
  keyboard selection goes on starting at the top left of the view,
  which is the price of the arrows still scrolling. A caret would mean
  Up and Down moved it and the view followed, which is an editor, and
  the reader is a pager.

- **The words a user reads say "connect to"** (2026-09-19), mirroring
  the host's "Serve this window…". "Work in" was wrong because
  nothing moves, "share" because a share is the set of panes handed to
  an agent, and "session" because a session is a running shell. The
  code still says "take over"; see "Keyboard and shortcuts".

- **A new pane opens on the shell that was picked last** (2026-09-19).
  Three things are asked in order: `-e` on the command line, the shell
  in the settings file, then the machine's default. `rememberShell`
  writes the file when a shell is opened by name. "Default shell" opens
  on the machine's default and forgets the pick. With nothing written
  down, `session.DefaultShell` answers `%COMSPEC%` on Windows, so a
  first launch opens cmd.exe, not PowerShell.

- **"Connection closed." already has two buttons** (2026-09-19,
  against `d79f629`). Reconnect and Close, with Close the default, as
  `TestEnterClosesThePaneRatherThanStartingItAgain` pins.

- **Every shell gets "Connection closed. Reconnect?"**, whether the
  transport went or the user typed exit, and whether the shell is on a
  machine or this one. That is what ssh prints, and gridterm calls
  every pane a connection. Settled twice on 2026-09-17: a reviewer
  argued a local shell was never connected, and Marcus kept the one
  wording.

- **A command is worded differently**, and not for tidiness. Nothing is
  being connected: the command's channel closed and the SSH connection
  is up. The question names the command and the choice says it runs it
  again, because running `make deploy` twice is a thing the user has to
  see before they press it.

- **A pane that cannot be put in a job object opens no pane, and the
  window says why** (2026-09-17). Opening it anyway brings back the
  orphaned shells the job object is there to stop, in silence.
  `log.Fatal` was wrong too: started from Explorer there is no console.

- **The keyboard shortcuts file holds changes, not the whole map**
  (2026-09-17). Moving a shortcut takes two lines, and the notice and
  the README say so.

- **A listing that fails while a path is being completed is not shown**
  (2026-09-17). A dialog per keystroke would be worse than the fault,
  so the failure goes to the log and the completion offers nothing.

- **Nothing caps the panes a window keeps** (2026-09-16). A pane worth
  keeping is worth reusing, so reusing it is the easy thing rather than
  throwing the transcript away. Closing a pane does release what it
  held.

- **What the agent sent is written down**, and is on the Servers menu
  as "What the agent typed" (2026-09-17). The user hands the pane over,
  gives the access and holds the secrets, so what the agent does there
  is theirs to read. It is what was sent, not what ran: Backspace, Tab
  completion and Up through the history all change a line first, Ctrl+U
  throws one away, a here-document reads as four commands, and in a
  full-screen program every line typed reads as a command. The dialog
  says so. A secret the user types at the agent's asking is not in it.

- **A hand-over does not expire** (2026-09-17). It lasts until the user
  takes it back or closes the pane. Revisit if forgotten hand-overs
  ever pile up.

- **A command pane can be handed over**, running or not. A running one
  takes keys on the command's stdin; an ended one can be read and not
  typed into, which is worth having for a failed build.

# Asked for, not yet worked out

Marcus's own list, in his words, kept until each has been looked at
properly and either written up above or done.

- **An Open menu of its own.** A menu listing the saved servers without
  the "Connect to" in front of each one, and the local ways in beside
  them, under a "Server" heading and a "Local" heading. `ui.MenuItem`
  holds a command and a title and nothing else, so a heading is a kind
  of item the menus cannot draw yet.

- **Context menu.** Make use of one. On the sidebar it opens the menu
  that is already there.

  A context menu is a `ui.Menu` anchored at the pointer. Everything it
  needs is there: `ui.NewMenu` takes command ids, `menu.Anchor` is a
  function returning a rectangle, and `a.showModal` puts it up. The
  plus on a machine heading does exactly this, anchored at a row.

  Where a right click can land, and what is there today:

  - A terminal pane. Nothing: the right button falls through
    (ui/term/term.go:943).
  - A machine heading on the sidebar. Its plus already opens a menu, so
    this is routing and no new lines.
  - A connection row on the sidebar. Nothing. Its button is a cross,
    not a plus, so there is no menu to open.
  - A file browser pane. Its operations live on its key bar.
  - A split divider, the menu bar, the ground between tiles. Nothing.

  The press reaches `a.root.HandleMouse` and walks the tree. Widgets
  return false for the right button today, so the window can take it
  after the tree has declined and work out what is under the pointer
  the same way the pointer shape is chosen.

  The hard part is a program that owns the mouse (ui/term/term.go:909).
  Selection already answers it: hold Shift and the program is bypassed.
  The menu should follow that rule rather than invent a second one.
