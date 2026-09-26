# gunimterm: a spike

One gridterm terminal in a [gunim](https://github.com/marrasen/gunim)
window. It runs your shell through gridterm's own session, VT parser and
key encoder, and draws the screen with gunim's `CellGrid`. It is here to
find out whether gunim can carry gridterm's interface, before any of it
moves over.

## Running it

gunim is a private module, so tell Go where to fetch it:

    $env:GOPRIVATE="github.com/marrasen"; $env:CGO_ENABLED=0; go run ./cmd/gunimterm

On Linux:

    GOPRIVATE=github.com/marrasen CGO_ENABLED=0 go run ./cmd/gunimterm

To work on gunim at the same time, clone it next to gridterm and run
`go work init . ../gunim`. The `go.work` file stays out of git.

- It takes gridterm's flags; `-h` lists them. `-stats`, or
  `GUNIMTERM_STATS=1`, prints each second the frames drawn and the
  screen updates merged into them.
- `-shot` drives the window through a script and writes PNGs, as in
  gridterm, such as
  `-shot 'until:$ wait:1500 type:make key:Enter until:done shot:built.png'`.
  The wait lets shell setup finish before typing.
- `GUNIMTERM_PROFILE=file` writes a CPU profile until the window closes.
- Ctrl+Shift+V pastes. The wheel scrolls back through history.

## What it showed, on Linux (2026-09-25)

Measured on the development machine: X11 over xrdp, software OpenGL
(llvmpipe), a 50 Hz display, a Swedish keyboard layout.

- `vim`, `htop`, colours, bold, the prompt and the cursor all draw right.
  Resizing the window resizes the shell (`stty size` agrees).
- Keys: Ctrl+C, Alt+B, Ctrl+A, Tab completion, the arrows in vim, F10 in
  htop, keypad digits and Enter, AltGr for @ $ € \ and a dead key for ~
  all reach the shell as they should.
- `time cat` of a million lines (6.9 MB): 2.5 to 2.7 s, against 2.3 s in
  gridterm today. The window drew 49 to 50 frames a second throughout,
  keeping up with the display, merging about 650 of 700 screen updates
  a second into them.
- In a CPU profile of that `cat`, gunim takes 8%. gridterm's VT parser
  takes 39%, and the garbage collector 37%, mostly for the line the
  parser allocates for each line that scrolls.
- Idle, the window uses no CPU: 0 ticks in 10 seconds, against 639 for
  gridterm today, which draws continuously.

## Not ported yet

What gridterm does that gunimterm does not, as of 2026-09-26. Each is to
be done, or decided against, before the switch. Checked against every
command gridterm registers, and against what was left out on the way.

Found by a second sweep on 2026-09-26, most important first. The first
six are confirmed in the code; the rest are still to check.

Connections and servers:

- The server form's details: Cancel first on a new host key, "Invalid
  password" on a retry, a key-file picker that remembers keys, Jump
  host and Forward agent off for a window, a Remove button in Edit.
- "Waiting for server" with Copy and Open for a browser sign-in.
- "Connection lost" with Reconnect for a window taken over.

Sidebar:

- Saved servers as headings, with a plus that connects, and a
  "+ Connect to server…" row at the foot.
- On each row: a state dot that pulses with traffic, a kind icon, a
  one-cell traffic graph, rows for file jobs with a progress fill, and
  a greyed row kept after a drop.
- Crosses on hover that close or clear a row.
- This computer's plus menu lists its shells.
- Headings for the machines on a window taken over; PageUp, PageDown,
  Home, End and Space; scrolling to follow the stage; "Edit This
  Window…".

Files:

- Archives open as folders (`vfs.WithArchives`).
- Go To offers Windows drives, and completes paths.
- The bar of F-keys, clickable, dimmed where they do nothing.
- Keys: Backspace edits the type-ahead first; Escape clears it, then
  the file clipboard; Insert marks; Ctrl+G opens Go To; Ctrl+D closes;
  Tab and Shift+Tab move between file panes.
- Symlinks in the link colour with "→ target"; F3 and F4 on a link to
  a folder.
- Files waiting to be pasted marked in the list; a read error as a
  row to click, and a "reading…" row.
- WSL folders in the Files menu; ".." hidden at a filesystem's top.

Secrets, serving and agents:

- A secret taken off the clipboard as the window closes.
- The secrets pane read again every second.
- An agent's secret request cut to 60 characters, invisible and
  private-use characters taken out.
- Removing several secrets at once.
- Disconnecting one served window from its row; "Don't Ask Again" on
  "Serve this window again?".

Checks on Windows, waiting until the Windows runs resume: the screen
reader recheck (#10) and this spike on Windows (#11).
