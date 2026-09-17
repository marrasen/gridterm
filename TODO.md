# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Answered on 2026-09-17

1. **A cross on a pane's row, shown on hover.** A pane row gets a cross
   like every other row, and it does what the row's `Close` already
   does: closes the pane and the row together. It appears only while the
   pointer is over the row, so a transcript is never one stray click
   away. This replaces the rule in `clearRow` that a pane row carries no
   cross. The remote case under "Panes and the sidebar" is the same gap
   from the other side.

2. **A hand-over lasts as long as the pane, not as long as the
   program.** Closing the pane is what drops it and stops the listener.
   Nothing changes for a host that reboots: the pane is still there, so
   the code still cannot be spent again. Two things follow, written up
   under "The agent, through MCP":
   - Reconnecting in the same pane keeps the same hand-over code.
   - The agent can start that reconnect itself, when the user has ticked
     the box for it. The boxes are done.

3. **A local command gets a command row, like a remote one.** Named by
   what it runs, with the same "Run it again?" question when it ends.
   `startAgainHere` already opens a ConPTY on an argv, so that is the
   piece to reuse.

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

## The agent, through MCP

Raised after a debugging session in a handed-over pane.

1. **No expiry for now.** Answered on 2026-09-17. A hand-over lasts
   until the user takes it back or closes the pane. Revisit if forgotten
   hand-overs ever pile up in practice.

2. **Drop the hand-over when the pane is closed.** Answered on
   2026-09-17. A pane whose program has ended keeps its hand-over, so a
   rebooted host still cannot spend the code twice. Closing the pane is
   what releases it, and the listener stops once the last one has gone.

3. **A command pane can be handed over already**, running or not.
   `handPane` looks at no kind and no state. A running one takes keys on
   the command's stdin; an ended one can be read and not typed into,
   which is worth having for a failed build. Nothing to do here: it is
   written down because it looked like a gap and is not one.

## SSH keys

Asked for on 2026-09-17. Nothing in gridterm makes a key or keeps track
of one today. `remote.Config.Identities` lists key files, the server
dialog has no field for one, and an empty list means the usual `~/.ssh`
names.

1. **Make a key.** A menu item opens a dialog that writes a new key pair
   to disk. It then says how to install the public half in the common
   places: OpenSSH on Linux, sshd on Windows, and gridterm's own served
   window, which takes a list of keys.

2. **Keep an index of keys.** The user adds keys to the index, and picks
   one from it when making or editing a connection. That fills
   `Identities` for that connection instead of leaving it to the
   defaults.

## Run a command

Asked for on 2026-09-17. The dialog is `openCommandHere` in here.go and
has one field.

1. **An optional working directory.** Empty means wherever the shell
   lands.

2. **Run it on this machine too.** As a command row, like a remote
   one. Answered on 2026-09-17; see the answers at the top.

3. **Save a command, and pick a saved one.** The dialog offers the
   commands already saved, so one that is run often is not retyped.

## The file browser's "Go to"

- **Autocomplete would be nice.** Complete a path as it is typed, from
  the filesystem the pane is on.

## Switching between panes

Asked about and answered on 2026-09-17. Marcus could not work out the
order Ctrl+Tab moves in, and expected recently used.

- **Where Ctrl+Tab stands.** It walks `ui.Leaves` of the whole tree,
  filtered to panes. That order is nothing on screen, and it is the one
  Marcus could not work out. Ctrl+PageUp and Ctrl+PageDown now follow
  the sidebar, so this is the only key left walking an invisible order.
  Recency is meant to replace it, below.

- **No key steps a whole tab any more.** Ctrl+PageUp and Ctrl+PageDown
  used to move between stage children, taking a split as one unit.
  Following the sidebar means stopping on every pane, so flipping to
  the tab behind now takes a press per pane. The strip that would have
  made tabs visible is switched off, so nothing was lost that the user
  could see. Worth a key of its own if it turns out to be missed.

- **A file manager is entered twice per lap.** The sidebar gives each of
  its panes a row, under whichever machine that pane reads, so a browser
  with a local pane and a remote one is two stops far apart in the walk.
  Two of the presses only move the highlight inside a browser already in
  front, which can read as the key doing nothing.

- **The order goes stale while the sidebar is shut.** `refreshPanel`
  stops building rows when the dock is collapsed, so the walk follows
  the last order it saw, with panes opened since on the end. Everything
  stays reachable. Nothing on screen contradicts it, because there is
  nothing on screen.

- **Ctrl+Tab becomes recently used, with the modifier held.** Hold
  Ctrl, press Tab to walk back through panes in the order they last had
  focus, release Ctrl to land. Ctrl+Shift+Tab walks the other way. This
  is what Alt+Tab, VS Code and Firefox's recently-used setting do, and
  it is the same idea as screen's `Ctrl+A Ctrl+A` and tmux's
  `prefix l` with more than two steps.

- **The list must not re-order while Ctrl is held.** Freeze it on the
  first press, walk the frozen list, and move the pane landed on to the
  front only on release. A list that re-orders as you walk swaps the top
  two entries on the first press and then bounces between the same pair.

- **Every pane in the window, whatever tab it is in.** The list is the
  leaves, ordered by when each last had focus, so a file pane is in it
  as well as a terminal. Landing on a pane in another tab brings that
  tab forward, which `focus` already does. Ctrl+PageUp and Ctrl+PageDown
  stay positional over tabs, so there is one key for position and one
  for recency.

- **An overlay while Ctrl is held.** A small list in recency order with
  the pane that would be landed on marked, gone on release. Without it a
  walk of more than one or two steps is counting in the dark.

- **Releases do not reach a command today.** `ChordOf` in ui/keymap.go
  returns the zero chord for anything that is not a press or a repeat,
  so nothing can be bound to letting go of a key. That is the one piece
  of plumbing this needs.

- **Do not rely on seeing the release.** Alt+Tab away from gridterm
  mid-walk and the release lands in another window. Commit on the first
  frame where Ctrl is not held rather than waiting for an event that may
  never come. Polling the modifier also covers the window losing focus,
  which nothing here can see: `ebiten.IsFocused` is unread, as the known
  gaps below say.

- **A pane that closes while the overlay is up** comes off the frozen
  list, and the mark moves to the one after it.

- **The shortcuts config file has to be able to say this.** A binding
  that holds a modifier is not a plain chord. Worth settling when the
  keyboard config under "Asked for, not yet worked out" is designed, so
  the file does not have to change shape twice.

## The tab strip is a remnant: delete it and rename what is left

Asked about and answered on 2026-09-17. Marcus guessed this was left
over from before the sidebar, and it is. Commit c52e0a1, "The sidebar
chooses what is showing, not a row of tabs", says in its own message
"So the strip is gone" -- but it was switched off with a flag rather
than deleted. `newTabs` sets `HideStrip = true` for every strip the
window makes, and nothing outside the `ui` package's own tests ever
sets it false.

- **Delete the strip.** About 127 of the 374 lines of ui/tabs.go:
  `stripRows`, the `Titled` interface, the four colour fields, `Label`,
  `HideStrip`, `stripHeld` and `stripButton`, the `buf`, and the methods
  `strip`, `labels`, `labelOf`, `drawStrip`, `paintStrip` and
  `CancelGesture`, plus the strip branch of `HandleMouse`. `Draw`,
  `body` and `ChildArea` all shrink. About 13 of the 37 tests in
  ui/tabs_test.go go with it. `Titled` is used by `labelOf` and by
  nothing else. No behaviour changes.

- **Delete the nesting branch in `placeTab`.** panes.go:249 wraps the
  focused pane in a strip of its own when nothing above it is one. It
  cannot run: the stage sits above every pane, so `stripAbove` always
  finds it, and a new pane opened while the keys are inside a split is
  added to the stage beside that split. Probed rather than assumed --
  the tree after a split and another new pane is
  `Stage[leaf, Split[leaf, leaf], leaf]`, flat. Nothing is ever stacked
  behind anything, and the "+" already opens a pane at the top level,
  which is what Marcus wants it to do.

- **Rename `Tabs` to `Deck`.** A deck of panes, one face up. The word
  "tab" names a thing that has not been on screen since 2026-09-14, and
  it is what made an explanation of the switching keys unreadable: it
  described a widget the user has never seen. The `tab.next` and
  `tab.previous` command ids go with it, and `stripAbove` becomes the
  deck above a pane.

- **"New tab" becomes "New pane".** It matches "Close pane" and "Split
  pane", which already say pane, and there is no tab left in the product
  to name.

## The New split menu

Asked for on 2026-09-17. `addSplitChoices` in split.go offers three
kinds of line: "New terminal", "Move <pane>" for every other pane open,
and "Terminal on <machine>" for every machine the window knows.

- **Offer what the "+" on a machine's row offers.** That menu has
  Terminal, Files and "Command…", and on this machine a line per shell
  found -- CMD, PowerShell, each WSL distribution. The split chooser has
  only Terminal. "Command…" and the shells belong in it.

- **Leave Files out.** A file pane belongs to the file manager and is
  split inside it, not into a terminal split. This is the one place the
  two menus deliberately differ.

- **Finding a pane to move is hard.** The lines that move an open pane
  into the split sit in one flat list mixed in with the machines, one
  line each, named by `paneName` with `paneWhere` beside it. Group them
  by machine the way the sidebar does, or let the chooser be typed into
  to narrow the list.

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

- **CMD and PowerShell rows say the path to the binary.** `labelFor`
  joins the argv, so the row reads the full path under System32. Say
  "Command Prompt" and "PowerShell" instead. `shells.Shell.Title`
  already holds those words; nothing carries them to the row.

- **An optional title bar in a pane.** The user turns it on, and the
  first row of the terminal shows the server name and the pane's title.
  Marcus wants it for split views, where nothing on screen says which
  pane is which.

## Known gaps worth revisiting

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

- There is no way back to the default shell once one has been picked.
  The menus offer a line per shell that was found, and none of them
  means "whatever `COMSPEC` names". Undoing a pick takes editing the
  settings file by hand. A "Default shell" line on both menus would do
  it, but `settings.check` turns an empty id away, so clearing the pick
  needs a way to say "nothing is picked" that is not an empty string.

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

- **Themes.** Change the colour scheme, and maybe build one, perhaps by
  editing a JSON file.
- **Settings beside the binary.** Keep the settings where the
  executable is, so the user can have copies. Add a config file for the
  keyboard shortcuts.
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
