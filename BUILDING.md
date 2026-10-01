# Building kakel

## From source

There is nothing to install: no C toolchain, no development headers, no
platform-specific anything. Every build is pure Go.

```
go build -o kakel .
```

Cross-compiling works the same way, in any direction:

```
GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o kakel.exe .
GOOS=linux   GOARCH=amd64 go build -o kakel .
```

On Windows, `-ldflags -H=windowsgui` links kakel as a windowed program.
Without it kakel is a console program: started from a shortcut, Windows
gives it a console window, which flashes up before kakel lets it go.
Built that way, `kakel -list-fonts` and `kakel -mcp-skill` still print
to the terminal they are run from.

`arm64` builds too, on both, though nobody has run one.

The window is drawn by [gunim](https://github.com/marrasen/gunim), which
loads OpenGL when the program starts rather than linking it, so the
build needs no graphics headers either.

`go test ./...` runs the whole suite, and needs no display: the window's
tests draw into gunim's offscreen window.

`make` has the rest: `make windows`, `make linux`, `make test`,
`make vet`, `make fmt`, `make icon`, `make release`.

CI's gate is `gofmt -l .`, `golangci-lint` and `go test ./...`
([.github/workflows/check.yml](.github/workflows/check.yml)). Run all
three before a push: `go vet` alone lets through what the linter
stops.

### Working on gunim at the same time

Clone gunim beside kakel and make a workspace, which the repository
ignores:

```
go work init . ../gunim
```

Builds then take gunim from `../gunim`. `go.mod` names the gunim commit
a build uses without the workspace; after pushing gunim, move kakel onto
it with `go get github.com/marrasen/gunim@<commit>`.

## Building a release

`make release` writes everything a release ships into `dist/`: a zip for
Windows, a tarball for Linux, and a `SHA256SUMS` beside them. It stamps
the version into the binary, so `TERM_PROGRAM_VERSION` and the MCP
handshake say which build this is. Both halves cross-compile, so this
makes a whole release on either platform.

```
make release VERSION=v0.1.0
```

CI runs the same target on a tag. See
[.github/workflows/release.yml](.github/workflows/release.yml) and
[RELEASING.md](RELEASING.md).

## Looking at the pixels

Most of this is testable without a display, but the parts that are not,
such as a corner, a colour or a line of text in the wrong place, are
exactly the parts where a test tells you nothing useful. `-shot` drives
a real window through a short script and writes PNG files:

```
kakel -shot "until:$ wait:1500 type:ls key:Enter wait:500 shot:ls.png"
```

The script is the same vocabulary an agent sends to a pane over MCP, so
a step learned in one is a step learned in both:

| step | what it does |
| --- | --- |
| `wait:<ms>` | wait that many milliseconds |
| `until:<text>` | wait until that text arrives on the focused pane |
| `key:<chord>` | press a chord, spelled the way a keymap spells it |
| `type:<text>` | type text, a character at a time |
| `shot:<file>` | write the window to a PNG file |

Each step takes at least a frame, so what a step did has been drawn
before the next one looks at it. Several `shot:` steps in one script
capture several states from one window launch. The window closes when
the script ends.

A shot holds the window itself. A popup, such as the command palette or
a menu, is a window of its own, and stays out of the image.

**Prefer `until:` to `wait:`.** A wait is a guess about how long a shell
takes to draw its prompt, and the guess is wrong on the machine that is
busy:

```
kakel -shot "until:$ wait:1500 type:./build.sh key:Enter until:FINISHED wait:200 shot:built.png"
```

`until:` waits for text that arrives after the step began, so it does
not match the echo of what the script has just typed. The exception is a
`until:` before anything has been typed -- "wait for the prompt, then
type" -- which takes the pane as it already is, because there is nothing
for the text to be an answer to. Wait a moment after it all the same:
the shell's own setup can still be running when the prompt shows. A step
that waits 30 seconds without seeing its text fails the run, and the
steps after it are not run.

Two things that cost a run each:

- **The script splits on spaces**, so a `type:` with an argument in it
  needs a script file: write `/tmp/x.sh`, `chmod +x`, then
  `type:/tmp/x.sh key:Enter`. `key:space` types a space between two
  `type:` steps.
- **A bare `until`** -- the MCP one, which waits for the shell to say a
  command has finished -- is refused. It reads the shell's own marks,
  which a screenshot script has no way to ask about. Give it text.

**`-shot` stands in for the keyboard.** Its `key:` and `type:` steps
hand events to the window's tree of nodes, which is where the platform's
keys arrive once gunim has read them. The path from the keyboard to
there, through the operating system and gunim's driver, is covered by
real keys only: `xdotool` on X11 and `SendInput` on Windows send them.
The check worth making is that Ctrl+C interrupts rather than typing the
letter c.

### The icon

The drawing is the source: `appicon` gives the icon in fractions of its
side and renders it at whatever size is asked for, so there is no image
file to edit by hand. A window sets its own icon from it, which is what
the window frame and the taskbar show while kakel runs.

The executable's own icon, which Explorer and a pinned shortcut show, is
a Windows resource built from the same drawing.
`rsrc_windows_amd64.syso` is checked in and `go build` links it by its
name alone, so building needs neither the network nor an extra tool. Run
`make icon` after changing the drawing, and a test fails if you forget.
Only `windows/amd64` gets one: the name is what the toolchain matches
on.

## The go-vte fork

`go.mod` replaces go-vte, the parser the terminal emulator reads a
program's output with, with
[marrasen/go-vte](https://github.com/marrasen/go-vte) at
`v1.0.11-gt.1`. That is upstream v1.0.11 with two changes, and the note
beside the replace line says what they are:

- The parser hands the emulator its own buffers for each control
  sequence, where it made new ones every time. A screen of true colour
  half blocks sends two sequences a cell, and parsing one frame at 250
  by 75 went from 21 ms and 112,504 allocations to 11.3 ms and none.
- SOS, PM and APC strings stop growing at a megabyte.

Both are worth offering upstream. The text for the two pull requests is
in that fork's
[UPSTREAM.md](https://github.com/marrasen/go-vte/blob/gt/UPSTREAM.md).
