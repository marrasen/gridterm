# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Now

- **Lend the screen so a watcher owns the size.** A pane being watched is
  drawn on the machine it runs on as well, and its size comes from that
  window's layout, so a watcher sees a screen of the wrong size and
  anything drawing a layout breaks. The pane should take the watcher's
  size and be drawn inside whatever box the host's layout gives it. In
  progress; an earlier attempt that gave the whole window away is
  stashed as "lending the screen, work in progress".

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
