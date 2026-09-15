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
