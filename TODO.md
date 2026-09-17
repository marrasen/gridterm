# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Waiting on an answer from Marcus

- **Should the window keep a record of what an agent typed?** The
  account below needs one. The obvious version was built on 2026-09-17
  and thrown away, because writing down what an agent sends keeps a
  password it types at a `sudo` prompt. Those characters do not echo, so
  until now the window never held them, and `Terminal.WaitForSecret` in
  ui/term/term.go says plainly that the characters are the program's and
  nothing here keeps them or passes them on. A keystroke record breaks
  that, and it puts the password in a dialog with a Copy button.
  There are three ways to go. Build the reader that takes the command
  off the screen instead, which is more work and is described below.
  Keep a keystroke record and accept that it holds secrets. Or drop the
  idea.

- **Should a listing that fails while completing a path be shown?** The
  file browser's "Go to" now completes a directory name as it is typed,
  which means reading the directory above it. A directory that is not
  there yet is what half a typed path looks like, so that one is
  dropped. Every other failure -- no permission, a connection that has
  gone, a disk error -- is written to the log and not shown, because a
  dialog per keystroke would be worse than the fault. That is a bare log
  without an answer from you, which the rules say to ask about.

- **Should a pane that cannot be put in a job object open anyway?** It
  does not today: `StartLocal` returns the error, and for the first pane
  `main.go` calls `log.Fatal`. Launched from Explorer there is no console,
  so the window would not start and would say nothing about why. Two
  reviewers argued for logging it and opening the pane. Against that:
  carrying on brings back the orphaned shells that the job object is
  there to stop, in silence. The one reachable failure -- a shell that
  exited before the job could hold it -- is already handled and is not an
  error. What is left needs Windows to refuse a nested job, which needs a
  build older than gridterm's own ConPTY requirement.

## Settled, do not re-open

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
  are in force, one per border. The half that names the command waits
  on the account below: nothing tells the window what an agent ran.

- **An account of what the agent did.** Somewhere to read the commands
  an agent ran, after the fact, rather than scrolling the pane.

  - **Do not build it from what the agent sent.** That was tried on
    2026-09-17 and thrown away. It keeps secrets, which is the question
    at the top, and it is not a record of what ran. Backspace, Tab
    completion and Up through the history all change the line before
    the shell sees it. Ctrl+U throws the line away and the text then
    glues itself onto the next command. A here-document reads as four
    commands. In a full-screen program every line typed reads as a
    command.

  - **Build it from what the pane echoed.** The line a shell prints
    back is the command it is about to run, after completion and after
    editing. It is already on the screen, so a dialog showing it gives
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

- **Changing the colour scheme leaves what is on a screen behind.** A
  cell holds the colours it is drawn in, not which entry of the scheme
  they came from, so there is nothing to look the new ones up with. New
  output and anything the program clears take the new scheme; what was
  printed before keeps what it was printed in, until the program draws
  it again. Doing better means keeping the entry a colour came from on
  every cell, through `grid`, `vt`, the renderer and the wire a watcher
  reads over. Worth deciding whether that is wanted before building it.

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
  the window passes an empty directory at both call sites in
  shellpick.go, so `--cd` is never sent. Nothing in the window tracks a
  pane's working directory yet. The file browser knows one, through
  `files.Pane.At()`, and no command opens a terminal from it. The
  working directory field under "Run a command" wants the same thing.

- `TestKickingAWindowThatHasAlreadyGoneSaysNothing` in status_test.go
  fails about one run in four, with "focus never reached the Kick
  marcus@laptop out button". It predates the shell work.
  `TestAMachineWithTooManyParkedFileSessionsIsRefusedTheNext` has been
  seen to fail the same way. Both are relay tests over loopback SSH.

- The package does not pass `go test -race`.
  `TestAKeyAfterAClickOnAScaledScreenReachesTheShell` reads
  `testApp.shells` off the mutex the harness appends under. It is the
  test harness, not the window.

- The cursor keeps blinking while the window is in the background.
  Nothing reads `ebiten.IsFocused`, and most terminals either stop the
  blink or draw the cursor hollow once the window loses focus.
- A click that brings the window to the front is acted on as well. A
  press that moves the keys to a pane stops there, through
  `ui.FocusesFirst`, but the window cannot do that for itself: nothing
  reads `ebiten.IsFocused`, so the click that woke it looks like any
  other. A headless test cannot drive that flag either.
- A watcher taking a screen over sees the far end's default cursor.
  `vt.Repaint` puts the cursor back where the program had it but carries
  neither its shape nor its blink, so `DECSCUSR` is lost over the wire.

## Asked for, not yet worked out

Marcus's own list, in his words, kept until each has been looked at
properly and either written up above or done.

- **A config file for the keyboard shortcuts.** Settings beside the
  binary was done on 2026-09-17; this half of the line was not.
- **UI.** The border on the File menu glitches to the left. The
  connection menu has odd spaces in its items where the sidebar's drag
  handle goes. The Terminal and File icons are too small. The app has
  no icon of its own.
- **Context menu.** Make use of one. On the sidebar it opens the menu
  that is already there.
- **Panel switcher.** Zoom every pane out, split panes and all, and lay
  them on a grid with live previews. Walk between them with the arrow
  keys. Scaled through the OpenGL layer so it stays fast.
- **Serving.** When "Serve this window" is switched on, offer to switch
  it on again at every start. Terminals made remotely on a host do not
  show up on the host.
