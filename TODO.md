# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

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
- A watcher taking a screen over sees the far end's default cursor.
  `vt.Repaint` puts the cursor back where the program had it but carries
  neither its shape nor its blink, so `DECSCUSR` is lost over the wire.
