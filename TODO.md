# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Panes that outlive what ran in them

1. **A pane stays when its shell exits or its connection drops.**
   Today `paneEnded` in panes.go keeps the pane only for a command, or
   for a connection that could not be made. Every other shell loses its
   pane and keeps a grey row. The user should be able to select a pane
   whose shell has gone and read what it printed, scrollback and all,
   and close it themselves.
   - **Where it is decided.** One line: the `e.Kind != conns.Command &&
     !a.kept[t]` test in `paneEnded`. Keeping everything makes the
     `removePane(t, true)` branch unreachable from there.
   - **What it changes for the window.** `removePane` quits the window
     when the last pane goes, so `exit` in the only shell closes
     gridterm today. With the pane kept it will not, and the user
     closes the pane or the window themselves.
   - **What the row says.** A kept pane's row has to say the shell has
     gone, the way a finished command's does, and closing the row has
     to close the pane.
   - **What it fixes elsewhere.** Three of the agent gaps below. A pane
     that is never removed is never put through `forgetPane`, so its
     hand-over survives, the agent listener stays up and the session
     code goes on naming something.

## The agent, through MCP

Raised after a debugging session in a handed-over pane.

2. **Say when a command has finished and what it exited with.** The
   agent appends `; echo MARKER` to every command and waits for the
   marker, because `quiet_ms` is guesswork and the prompt is already on
   screen before the command runs. A `run_command` tool, or an
   `until_prompt` option on `wait_for`, would remove the ritual.

3. **Give the agent the output of the last command, not the screen.**
   `read_pane` hands back a rectangle, so the agent has to work out by
   eye where the current output starts. That is why it kept clearing
   the screen, and the window already knows where the boundary is.

4. **Let the agent ask for a secret.** Put the prompt up, block until
   the user has typed it into the pane, and return without ever showing
   the agent the characters.

5. **Make the setup line copyable.** The hand-over dialog tells the
   user to add the MCP server with a command line, and there is no way
   to copy that text, so it has to be typed out again by hand.

6. **`clear` takes the scrollback with it.** Not a bug: `clear` sends
   `ED 3` as well as `ED 2`, and `Screen.EraseInDisplay` in
   vt/screen.go drops the scrollback for mode 3, which is what the
   sequence means. It is also why the agent's own commands could not be
   found afterwards -- the screen and the history above it went
   together. `clear -x` leaves the history alone. If the history should
   survive `clear` anyway, that is a choice to make and write down, not
   a fault to fix.

## Known gaps worth revisiting

- A WSL pane does not start in the directory the window is looking at.
  `shells.Shell.Command` translates a Windows directory into a `/mnt`
  path and passes it as `--cd`, and `shells.UnixPath` is tested, but
  the window passes an empty directory at both call sites in
  shellpick.go, so `--cd` is never sent. Nothing in the window tracks a
  pane's working directory yet. The file browser knows one, through
  `files.Pane.At()`, and no command opens a terminal from it.

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
