# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Questions for Marcus

Open calls that are his, not mine. Each one is written down rather than
guessed at.

1. **"Open another pane there" opens a shell, whatever the pane it was
   opened from was running.** Hand over a pane running `docker exec sh`,
   or `kubectl logs -f`, or a command that has already finished, tick
   the box, and the agent gets a plain login shell on that machine. It
   never had one.
   - **Why it is written this way.** "Another pane to the same server"
     is what the "+" on a machine's row opens, which is a shell. Opening
     another `docker exec` would be a different feature.
   - **The question.** Should the box be refused on a pane that was
     opened with a command, so it only ever widens a shell to a second
     shell? A reviewer raised it. It opens a shell for now.

2. **Should the short rules go in the hand-over prompt as well?** The
   rules are five lines now, so carrying them in both places costs
   little. The reason to: the MCP server's `instructions` are the only
   place an agent is told them, and a client is free to ignore
   `instructions`. Claude Code shows them; gridterm cannot check what
   Codex, Cursor or another host does, and the skill is no fallback
   there because only Claude Code has a place gridterm knows to write
   one into. The prompt says nothing about them for now, as you asked
   on 17 September.

## Answered on 2026-09-18

1. **The rules say the agent is trusted, and stop there.** `mcp.Rules`
   was a list of prohibitions. It now says the pane is a live machine
   somebody has trusted it with, and asks it not to spend that trust.
   The password stays spelled out, because typing one is taking a
   credential the user never handed over.

2. **The button is "Instructions".** Five buttons of the length of
   "Install instructions" do not fit an eighty column window, and a form
   drops the ones that will not fit without saying so.

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

1. **Let the agent ask for a secret.** Put the prompt up, block until
   the user has typed it into the pane, and return without ever showing
   the agent the characters.

2. **No expiry for now.** Answered on 2026-09-17. A hand-over lasts
   until the user takes it back or closes the pane. Revisit if forgotten
   hand-overs ever pile up in practice.

3. **Drop the hand-over when the pane is closed.** Answered on
   2026-09-17. A pane whose program has ended keeps its hand-over, so a
   rebooted host still cannot spend the code twice. Closing the pane is
   what releases it, and the listener stops once the last one has gone.

4. **A command pane can be handed over already**, running or not.
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

Reported on 2026-09-17, with a request to look into it properly. It is
one bug with two halves, and then two things Marcus wants.

- **What happens.** Type a path that does not exist, say `X:\`, and
  press Go. The dialog closes. An error dialog then appears saying the
  directory could not be read. The listing stays on `D:\`. Click the
  `Marras` folder in that `D:\` listing and the error is about
  `X:\Marras`.

- **Why the dialog closes first.** The read is asynchronous. `Go` calls
  `files.Pane.Open` and returns, which closes the form, and the failure
  arrives frames later through `p.OnError`, which posts a dialog of its
  own. Nothing tells the button to wait for the read.

- **Why the next click is wrong.** `openAt` in ui/files/pane.go sets
  `p.at = path` before the read starts, and `show` keeps the old entries
  when the read fails. So the pane is showing `D:\` while `At()` says
  `X:\`, and opening a row joins the name onto the wrong one. Keeping
  the old names is the fallback Marcus approved and that part is fine.
  Leaving `at` moved is not. Put `at` back when a read fails, or do not
  move it until one succeeds.

- **Keep the dialog open on an error.** Marcus wants to fix a typo and
  try again in the same dialog. The button has to wait for the read and
  show the reason in the form, which is what `f.errText` is for.

- **Autocomplete would be nice.** Complete a path as it is typed, from
  the filesystem the pane is on.

## Switching between panes

Asked about and answered on 2026-09-17. Marcus could not work out the
order Ctrl+Tab moves in, and expected recently used.

- **What the two pairs do today.** Both walk an order nothing on screen
  shows. Ctrl+PageUp and Ctrl+PageDown step one along the stage, which
  holds every pane in creation order, because `a.stage.Add` appends.
  The sidebar groups by machine instead: local, then the saved machines
  in book order, then the rest sorted. The two orders agree only if the
  panes were opened in sidebar order and none was ever closed, so the
  keys look like they jump between machines at random. Ctrl+Tab and
  Ctrl+Shift+Tab call `focusPane`, which walks `ui.Leaves` of the whole
  tree and does not filter, so the connections sidebar is in the cycle
  as though it were a pane. Landing on it then makes Ctrl+PageUp and
  Ctrl+PageDown do nothing at all, because the sidebar has no stage
  above it.

- **Ctrl+PageUp and Ctrl+PageDown follow the sidebar.** Answered on
  2026-09-17. The keys step through panes in the order the sidebar
  lists them, down the rows and across the machine headings, so the list
  on screen is the only order there is. The stage's creation order stops
  being something the user can feel.

- **Ctrl+Tab stops landing on the sidebar.** `focusPane` filters its
  list through `a.isPane`, which already exists and already excludes the
  sidebar and the panel. One line, and it removes the dead-key symptom
  above with it.

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
  described a widget the user has never seen. `focusTab` and the
  `tab.next` and `tab.previous` command ids go with it, and `stripAbove`
  becomes the deck above a pane.

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
