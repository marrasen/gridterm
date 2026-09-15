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

## The sidebar and the panes

- **Use the type icon instead of the dot.** A connection's row draws a
  coloured dot for its state and then the hand-drawn icon for what it
  is. One mark can do both: draw the type icon and give it the colour
  the dot would have had.
- **Fold the connection log once the connection is made.** Every
  terminal starts with the account of how it was reached. Once it has
  worked, that is scrollback nobody needs open; it should fold, with a
  way to open it again.
- **Colour the connection log.** The time in dark green on a line that
  went well and dark red on one that did not, and the words themselves a
  darker grey than the shell's output.
- **Remove the grey bar between the sidebar and the panes.**

## Errors and logs

- **Error dialogs cut their text off.** The message is trimmed to the
  dialog's width and the rest is lost. Hit twice, both times hiding
  something needed.
- **Error dialogs cannot be selected or copied.**
- **Error dialogs should be red.**
- **A log view.** Somewhere to read the whole of what a connection said,
  scroll it, and copy out of it.

## Connections

- **"Files" should connect when the machine is not connected**, the way
  "Terminal" does. It should work for an SFTP-only connection too.
- **"Forget" a server should close its connections first**, so the row
  goes rather than staying behind under a name nothing saved.
- **Remember what "Serve this window" was set to.** The dialog asks for
  the port and where it may be reached from every time, starting from
  the defaults. It should open on whatever was last used.

## The file browser

- Nothing outstanding. Ctrl+G goes to a path and offers the drives;
  typing a name jumps to it.

## Known gaps worth revisiting

- The cursor never blinks (`DECSCUSR` styles 1, 3 and 5 draw as the
  steady ones).
- `-ssh` connects before the window opens, so it asks on the console and
  has no connection pane.
- There is no help anywhere. Keys are on the file browser's bar and on
  the menus; nothing lists them all.
