# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Connections

- **"Files" should connect when the machine is not connected**, the way
  "Terminal" does. It should work for an SFTP-only connection too.
- **Remember what "Serve this window" was set to.** The dialog asks for
  the port and where it may be reached from every time, starting from
  the defaults. It should open on whatever was last used.

## Copying files

- **A copy should show its progress when its row is clicked.** The row
  says a copy is happening and nothing else. Clicking it should show how
  far it has got, how fast it is going and what it is on now.
- **A copy should be cancellable.** The jobs underneath already take a
  cancel; nothing offers it.
- **A copy that has finished should be repeatable from its row.** The
  row stays when the copy is done, so copying the same file again --
  a log with new lines in it, say -- should be one click rather than
  finding both ends again.

## The file browser

- Nothing outstanding. Ctrl+G goes to a path and offers the drives;
  typing a name jumps to it.

## Known gaps worth revisiting

- A sidebar row drawn before a window is re-keyed still names the
  window by its old key until the next frame, so a click on it in that
  frame finds nothing.
- The cursor never blinks (`DECSCUSR` styles 1, 3 and 5 draw as the
  steady ones).
- On Windows a command that writes and exits in the same instant loses
  its output. A ConPTY repaints on a clock of its own, and the reaper
  closes the pseudoconsole as soon as the child is reaped, which is
  before the repaint. `session`'s Windows tests type their commands into
  a shell that stays running rather than passing them on the command
  line. Unix has `TestOutputSurvivesAChildThatExitsImmediately` for the
  same case and passes it.
- `-ssh` connects before the window opens, so it asks on the console and
  has no connection pane.
- A file pane on a gridterm window has no bound. `windowFiles` in
  `browse.go` calls `t.win.Files()` and then `sftp.NewClientPipe`, both
  on the goroutine that draws and neither of them bounded. The same pane
  on a machine goes through `Conn.Files`, which gives the whole open one
  deadline.
- `Shell.Resize` can park. It sends a `window-change` request, which
  takes x/crypto's channel write lock, so it waits when the send buffer
  to the machine has filled. Dragging a window edge is what calls it.
