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

- `GUNIMTERM_STATS=1` prints each second the frames drawn and the screen
  updates merged into them.
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

The window:

- The command-line flags: `-font-size`, `-e`, `-scrollback`, `-ssh`,
  `-font`, `-font-family`, `-list-fonts`, `-mcp-skill`, `-stats` and
  `-shot`.

Panes:

- A program's OSC 9 message and OSC 9;4 progress, on the pane's
  sidebar row. A message also goes to the log, and to a desktop
  pop-up, at most one every 2 seconds.
- Desktop pop-ups (`gridterm/notify`) for notices.
- The slow glow around a pane shared with an agent or watched from
  another window.
- A held screen bigger than its pane, drawn scaled to fit, with its
  sidebar row saying it is held.
- A session's errors, written to the window log (`OnError`).
- The line in a pane's transcript saying a server's address changed.

Settings:

- Unticking Keep on a saved command forgets it.
- Old saved commands, tunnels and copies given their server's ID
  (`FillServerIDs`).

Checks on Windows, waiting until the Windows runs resume: the screen
reader recheck (#10) and this spike on Windows (#11).
