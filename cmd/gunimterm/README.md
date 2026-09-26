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

Found by a third sweep on 2026-09-26, still to check one by one:

Help, themes and shortcuts:

- While a menu is open, the full title of the item under the pointer
  shows on the bottom row.
- A short confirmation stays on the status line for 4 seconds;
  gunimterm uses a toast.
- About opens on OK, and a second update check while one runs is
  ignored.
- A development build newer than the last release says so, rather
  than "A newer gridterm is out".
- File Locations offers to make the install portable.
- Help groups the commands by menu, lists the keys the menus do not
  show, and takes the file pane's and reader's keys from gridterm's
  own lists.
- Reload Shortcuts says it worked only once the file is taken.
- Command ids renamed in `keys.Renamed` are followed.

Agent sharing:

- An unknown path to the program: a warning on copying the prompt, and
  the skill refused. The skill's blank line after its front matter;
  a relative `CLAUDE_CONFIG_DIR` taken from home.
- The share's listener closed as the window closes.

Jobs and tunnels:

- Stop in the overwrite question says "It was stopped", not a failure.
- A cancelled job that could not remove a half-written file says so.
- A running job says how long it has been going.
- Repeat pressed again while its machine reconnects does nothing more.
- Repeat finds its machines by saved-server id, and refuses one removed
  or connected elsewhere; a saved tunnel whose server was removed is
  refused too.
- A finished job keeps its sidebar row until cleared.
- Disconnecting on purpose takes its tunnels' rows away.
- Trouble closing tunnels after a drop is said.
- A tunnel that could not be saved or forgotten says so.
- Opening a saved tunnel leaves the saved list's order alone.

Checks on Windows, waiting until the Windows runs resume: the screen
reader recheck (#10) and this spike on Windows (#11).
