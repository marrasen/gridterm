# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Connections

- Nothing outstanding. "Files" connects the machine when nothing is
  connected to it, the way "Terminal" does, and opens the pane over the
  connection it makes.

## Copying files

- Nothing outstanding. A copy's row opens a dialog saying what it is on,
  how far it has got and how fast it is going, with Cancel while it runs
  and Repeat once it has finished.

## The file browser

- Nothing outstanding. Ctrl+G goes to a path and offers the drives;
  typing a name jumps to it.

## Known gaps worth revisiting

- The cursor keeps blinking while the window is in the background.
  Nothing reads `ebiten.IsFocused`, and most terminals either stop the
  blink or draw the cursor hollow once the window loses focus.
- A watcher taking a screen over sees the far end's default cursor.
  `vt.Repaint` puts the cursor back where the program had it but carries
  neither its shape nor its blink, so `DECSCUSR` is lost over the wire.
