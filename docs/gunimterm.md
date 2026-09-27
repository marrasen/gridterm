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

## The echo

The window sends rings out past its edges, onto the desktop, when
something happens that you may be looking away from:

- Red: a connection fails, or drops by itself; a copy or other file job
  stops with an error; a tunnel dies; a served window loses its client;
  a shell or command fails to start; a pane out of sight ends with a
  nonzero exit; a command that ran 3 seconds or more fails out of sight.
- Green: a connection is made; a file job or upload finishes; a pane out
  of sight ends with exit 0; a command that ran 3 seconds or more
  succeeds out of sight.
- Amber: the bell rings in a pane out of sight, or while another program
  has the keyboard.
- Grey, faint and repeating: a connection is being made.

Out of sight means in a pane other than the one in front, or in any pane
while another program has the keyboard. A command's finish needs a
shell that marks its commands with OSC 133 or OSC 633, as Machine >
Shell Setup teaches bash, zsh and PowerShell to.

A theme sets the colours and the strength in its `Echo` block, in the
themes file. Each colour left out comes from the theme's palette: bright
red, bright green, bright yellow, and the text colour dimmed for the
waiting ring. `Strength` scales every ring: `0` turns the echo off, `2`
draws it twice as strong.

    "Echo": {"Problem": "#ff5f5f", "Wait": "#808890", "Strength": 0.6}

The echo shows on a desktop that blends windows, and waits while the
window is maximized or full screen.

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

Found trying it on Windows on 2026-09-26:

- The "Split with" choice lacks gridterm's "Command on <machine>…"
  lines, which run a command in the new half.

Decided against:

- gridterm's short confirmations stay on the status line for 4
  seconds. gunimterm shows them as toasts, which also go by themselves
  and cost no keypress.

Checks on Windows, waiting until the Windows runs resume: the screen
reader recheck (#10) and this spike on Windows (#11).
