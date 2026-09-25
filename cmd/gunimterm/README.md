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

Left for later: scrollback past the wheel, selection, links, pictures,
splits, menus and dialogs.
