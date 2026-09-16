# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Now

- **Scale a held screen to fit the host's window.** A watched screen now
  takes the watcher's size, and the machine it runs on draws as much of
  it as fits in the room its own layout gives that pane. When the
  watcher's screen is the bigger of the two, the rest is not shown
  there. Marcus asked for it to be scaled instead, so all of it is
  visible however the two sizes differ. Needs the pane drawn to an
  offscreen image and blitted scaled, which the compositor does not do
  yet.

## Connections

- **"Files" should connect when the machine is not connected**, the way
  "Terminal" does. It should work for an SFTP-only connection too.
- **"Forget" a server should close its connections first**, so the row
  goes rather than staying behind under a name nothing saved.
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

## Panes

- **Dividers should be draggable.** A split shares the room evenly and
  there is no way to give one pane more of it. The same is true of the
  file browser's two panes. `ui.Split` works out its weight afresh on
  every layout and nothing ever writes it, so the weight has to become
  something the divider can set and the layout has to keep.

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
