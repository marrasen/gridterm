# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Questions for Marcus

Keeping a pane when its program ends raised three decisions that are
his, not mine. Each one is written down rather than guessed at.

1. **Nothing caps the panes a window keeps.** A pane holds its whole
   screen and up to 5000 lines of scrollback, which on a wide window is
   tens of megabytes. A day of opening and exiting shells leaves them
   all. The choices: leave it, trim the scrollback when a pane ends, or
   close the oldest ended pane past some number. Trimming is the one
   that argues with what was asked for.

2. **The cross means nothing on a pane's row.** It drops the row on a
   machine, a window, a tunnel and a job. A pane's row has none,
   deliberately: `clearRow` says the cross must not throw a transcript
   away. But a pane row's `Close` already closes the pane and the row
   together, which is what a user wants, and a cross backed by that
   would be consistent. Today getting rid of one dead pane is a click
   and a chord, or three clicks with the mouse alone.

3. **The agent's listener never stops while a dead pane keeps its
   hand-over.** That is the mechanism that fixed the complaint about a
   rebooted host spending the session code, so it cannot simply be
   undone. But hand fifty panes over, exit all fifty shells, and the
   window holds fifty hand-overs and an open loopback port with nothing
   left to type into.

## The agent, through MCP

Raised after a debugging session in a handed-over pane.

1. **Say when a command has finished and what it exited with.** The
   agent appends `; echo MARKER` to every command and waits for the
   marker, because `quiet_ms` is guesswork and the prompt is already on
   screen before the command runs.
   - **Half of this is in.** `vt.Terminal.Command()` reads the shell's
     own marks, OSC 133 and VS Code's OSC 633, and says whether a
     command is running, what the last one exited with, and whether the
     shell gave a status at all. Nothing uses it yet.
   - **What is left.** Carry it through `ui/term`'s `Reading` under the
     same lock as the screen, so a screen and an exit status come from
     one moment, then out through `agent.Look` to the tools. `Done` is
     the signal to key on: it only moves forward, so a tool that reads
     it before sending keys can tell a real finish from a `Running`
     that is stuck.
   - **A shell with no integration still needs an answer.** Mark the
     pane when `send_keys` runs and let `wait_for` end on the prompt
     coming back. The tools have to say which of the two they gave.

2. **Give the agent the output of the last command, not the screen.**
   `read_pane` hands back a rectangle, so the agent has to work out by
   eye where the current output starts. That is why it kept clearing
   the screen, and the window already knows where the boundary is.

3. **Let the agent ask for a secret.** Put the prompt up, block until
   the user has typed it into the pane, and return without ever showing
   the agent the characters.

4. **Make the setup line copyable.** The hand-over dialog tells the
   user to add the MCP server with a command line, and there is no way
   to copy that text, so it has to be typed out again by hand.

5. **`clear` takes the scrollback with it.** Not a bug: `clear` sends
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
