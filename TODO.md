# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Questions for Marcus

Keeping a pane when its program ends raised three decisions that are
his, not mine. Each one is written down rather than guessed at.

1. ~~Nothing caps the panes a window keeps.~~ **Answered on 2026-09-16:
   no cap.** A pane that is worth keeping is worth reusing, so the
   answer is to make reusing it the easy thing rather than to throw the
   transcript away. See "Connect again in the same pane" below. Closing
   a pane does release what it held: `closePane` drops it from
   `a.panes`, from `a.ended` and from the registry, and `forgetPane`
   clears the machine, window and hand-over records, so nothing is left
   holding the grid or the emulator.

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

## Connect again in the same pane

Asked for on 2026-09-16, answering the question about capping kept
panes: the way to stop them piling up is to make the old pane the
obvious place to go back to.

1. **The pane asks.** A pane whose program has gone puts a question on
   its last row -- "Connection closed. Reconnect?" with Yes and Close
   -- and the user picks, or leaves it and goes to another tab. The
   pane is where the user is looking, so a connection that drops in
   front of them says so there rather than going quietly grey on a
   sidebar they may have hidden.
   - **Done.** `Terminal.Restart` puts a new session under an existing
     terminal, keeping the grid and the emulator, which is what keeps
     the transcript.
   - **The question itself** is the widget's to draw and the window's
     to word. It is not written into the emulator: the transcript is
     the program's.
   - **What to reconnect to.** The entry knows the host and the kind.
     A local shell starts the shell it ran; a pane on a machine
     reconnects to it and opens a shell; a command runs again. Each
     pane has to remember how it was started, which nothing records
     today. A machine that has to be dialled first goes through
     `openRoute`, so `opening` needs to say "into this pane" the way
     `at` says which split.
   - **The wording.** Every shell gets "Connection closed. Reconnect?",
     whether the transport went or the user typed exit, and whether the
     shell is on a machine or on this one. That is what ssh itself
     prints, and gridterm calls every pane a connection. Settled twice,
     on 2026-09-17: a reviewer argued a local shell was never connected
     and reconnecting it only forks a process, and Marcus kept the one
     wording anyway. Do not re-open it.
   - **A command is the one that differs**, and not for tidiness.
     Nothing is being connected: the command's channel closed and the
     SSH connection is still up. Picking the choice re-runs the
     command, which for `make deploy` or anything with side effects is
     a thing the user has to see before they press it. So the question
     names the command and the choice says it runs it again.

2. **Say how the command ended, in the question.** Marcus's own case
   for the feature: "maybe it errored, maybe this time it doesn't". The
   question could read "`make deploy` finished, exit 1. Run it again?"
   - **Remote commands already have it.** `remote.Shell.Wait` gives
     back an `*ssh.ExitError`, and `ExitStatus()` is the number.
     Nothing reads it today.
   - **Ordinary panes have it when the shell says so**, through
     `vt.Terminal.Command()`, which reads the shell's own OSC 133 and
     OSC 633 marks. `Exit()` gives the status and whether there was
     one; a shell with no integration gives nothing, and the question
     has to stay honest about that rather than saying "exit 0".

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
