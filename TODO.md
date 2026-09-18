# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

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

## Reading a file without a shell

Asked for on 2026-09-17. A new kind of pane, opened from the file
browser, which needs an icon of its own.

- **View a file, and tail a file.** Two commands in the browser. Both
  open a reader pane; tailing follows the file as it grows and stays at
  the bottom, the way `tail -f` does.

- **The reader works like less.** It reads a page at a time rather than
  the whole file, "/" searches, ":" goes to a line number, and a hex
  mode shows the bytes.

- **Colour and markdown.** Syntax colouring, and a rendered view for a
  markdown file.

- **Images too.** Viewing a picture shows the picture. The window
  already draws pane-sized layers of its own, so there is somewhere to
  put one.

- **Tailing on Windows may be refused.** A file another program has open
  for writing can fail to open at all. That has to be reported, not
  assumed away.

## Pasting and dropping an image

Asked for on 2026-09-17, after seeing Claude Code take a pasted image in
a terminal.

- **Nothing can read an image off the clipboard today.** gridterm uses
  atotto/clipboard, which carries text only, and `clipboardWriter` in
  clipboard.go only writes. Reading an image needs another library or
  the platform call.

- **A program reading stdin cannot be handed a picture.** Claude Code's
  answer is a file: the image is written somewhere the program can open
  it, and the path is typed in its place. For a pane on another machine
  the file has to go over the connection first.

- **Dropping a file on the window is the same question** with the path
  already on disk. `ebiten.DroppedFiles` reports it.

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

## Panes and the sidebar

- **A window whose connection dropped is not offered a way back.** The
  other window now says why it is going, so a client tells a window that
  stopped sharing from one that was thrown out from one that just went.
  A window that just went is the case worth offering "Reconnect" on, and
  nothing offers it. What stops it is that `taken` does not keep the key
  file the window was taken over with, so there is nothing to reach it
  again with: `takeOver` wants an address and a key file, and only the
  address survives. Either keep the key file on `taken`, or look it up
  in the server list by address, which only works for a window that was
  saved.

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

- **A file browser's pane gets no line naming it.** The line above a
  pane is a terminal's, and the file manager's panes are not terminals:
  `refreshCaptions` walks `a.panes`, and `ui/files.Pane` has no caption
  at all. A split holding a shell and a browser names the shell and not
  the browser, which is half of what the line is for.

- **A pane drawn on a layer of its own shows no line.** A held screen
  too big for its room is painted by the window rather than by the tree,
  and that painting draws the screen alone. The pane keeps its row for
  the screen instead, so nothing is cut; there is simply nothing to read
  on it.

## Known gaps worth revisiting

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

- **The gap beside the sidebar still runs through any dialog wide enough
  to cross it.** The window leaves half a cell after the sidebar's last
  column, and every modal grid is given the same padding so the two
  agree about where a column sits. A dialog whose box spans that column
  therefore has the gap running down the inside of it, breaking every
  line of text across it. The menu from a sidebar row now opens past the
  gap, which was the case in the report, but the File menu, the palette,
  the chooser and any wide form still cross it. Selected rows and rules
  hide it, because a background covers the padding and a line reaches
  across it; plain text does not.
  The fix is for modal grids not to carry that gap at all. What stops it
  being a one-liner is the mouse: a pixel becomes a column through one
  geometry, the window's, so a dialog whose columns sat elsewhere would
  take a click on the wrong one. The frost would have to be measured
  from the modal layer's own geometry too.

- **The keyboard shortcuts file is read once, at startup.** Colour
  schemes have a "Reload", and this does not. Applying the changes again
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

- **`TestAFinishedJobLetsGoOfItsContext` fails now and then.** It says
  `the job finished still holding its context` at jobs/stat_test.go:179.
  Seen once while the whole suite was running. Thirty runs of it alone
  and five whole suites passed on 2026-09-18, so there is no rate to
  quote.

- **`TestClosingAPaneLeavesDetachedWorkRunning` fails now and then under
  load.** It says `the wait ended with 0x0 inside 500ms` at
  session/job_windows_test.go:42. Seen once while the whole suite was
  running. Measured on 2026-09-18: thirty runs of it alone passed and
  one of five whole suites failed, so it is the half-second budget
  rather than the job object.

- **`TestAMachineWithTooManyParkedFileSessionsIsRefusedTheNext` fails
  about one run in sixty.** It says `the plus opened <nil>, want a
  menu` at serving_test.go:1095: the press on the plus is taken and
  nothing opens. It was failing nineteen runs in sixty until the reset
  a window that let go arrives as stopped being reported as a fault,
  which was putting a notice over the press. What is left is something
  else, and the modal is nil rather than a notice, so it is not the
  same cause. Not diagnosed.

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

- **Nothing helps a user set up a copy that carries its own files.** They
  make the directory and copy six files into it by hand, and a copy done
  wrong looks like a window that was freshly installed. A "Make this copy
  carry its own files" button on the notice would do the whole thing, and
  would still not have gridterm making the directory on its own, because
  the user would have asked.

- **The key a carried copy serves with is only as private as its
  directory.** Windows ignores the mode on the file, and `os.MkdirAll`
  ignores the mode on the directory, so a copy under `C:\Program Files`
  or on a share hands the key to every account that can read the path.
  Anybody holding it can pretend to be this window. The notice says to
  keep the directory somewhere only you can read, and that is the whole
  of what gridterm does about it. Same class as the key line above.

- **A pane watched over the wire keeps the host's named colours.** The
  window the pane belongs to sends its screen again when the scheme
  changes, so a watcher sees the change at once. What it sees is a mix.
  The wire leaves the default ground and text out, so those take the
  watcher's own scheme. Everything the program named is carried as a
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

- **"New terminal" and "New pane" are the same thing under two names.**
  The split chooser's first line is "New terminal" (split.go:52); the
  File menu and the palette call it "New pane". Marcus asked what the
  first one does, which is the question a name earns when it is the only
  one of its kind.

- **The glow on a shared pane is a whole cell thick.** It is drawn as
  cells, so the border round a pane somebody else is in is a character
  wide and a character tall. It should be a couple of pixels. That means
  drawing it the way the frosted rim is drawn rather than as a row of
  cells.

- **"Connection closed. Reconnect?" should have two buttons.** Make it
  "Connection closed." with "Reconnect" and "Close", and let Close be
  the one Enter takes. The question is worded in `closedQuestion` in
  restart.go and the buttons are the caller's. This does not re-open the
  settled question above about which panes get asked and in what words;
  it is about the answer, not the question.

- **Shadows under a dialog break at the rounded corners.** `drawShadow`
  in ui/frame.go paints whole cells, and the corner of a frosted panel
  is round, so the shadow shows through the curve.

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
