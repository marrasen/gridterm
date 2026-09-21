# Building gridterm

## From source

There is nothing to install: no C toolchain, no development headers, no
platform-specific anything. Every build is pure Go.

```
go build -o gridterm .
```

Cross-compiling works the same way, in any direction:

```
GOOS=windows GOARCH=amd64 go build -o gridterm.exe .
GOOS=linux   GOARCH=amd64 go build -o gridterm .
```

`arm64` builds too, on both, though nobody has run one.

This used to need the X11 development headers on Linux, because
ebitengine's bundled GLFW was C. Upstream rewrote that layer in Go for
v2.10 and gridterm's fork moved onto it, so the headers are gone along
with cgo. See **The ebiten fork** below.

Most of the code needs no window at all. `make test` runs the packages
that do not touch a GPU -- the grid, the emulator, the key and mouse
encoders and both session types -- and `go test ./...` runs the whole
suite, which needs no display either.

`make` has the rest: `make windows`, `make linux`, `make test`,
`make vet`, `make fmt`.

## Building a release

`make release` writes everything a release ships into `dist/`: a zip for
Windows, a tarball for Linux, and a `SHA256SUMS` beside them. It stamps
the version into the binary, so `TERM_PROGRAM_VERSION` and the MCP
handshake say which build this is.

The Windows half cross-compiles from anywhere, because that build needs
no C toolchain. The Linux half is built natively, so a release built on
Windows carries the Windows binary only.

```
make release VERSION=v0.1.0
```

CI runs the same target on a tag. See
[.github/workflows/release.yml](.github/workflows/release.yml) and
[RELEASING.md](RELEASING.md).

## Looking at the pixels

Most of this is testable without a display, but the parts that are not —
a blurred panel, a rounded corner, a dialog drawn over the wrong thing —
are exactly the parts where a test tells you nothing useful. `-shot`
drives a real window through a short script and writes PNG files:

```
gridterm -shot "wait:60 key:ctrl+shift+k wait:2 shot:palette.png"
```

The steps are `wait:<frames>`, `key:<chord>`, `type:<text>` and
`shot:<file>`, each taking one frame so that what a step did has been
drawn before the next one looks at it. Several `shot:` steps in one
script capture several states from one window launch. The window closes
when the script ends.

This found a bug that every test had passed over: the frosted panel was
drawing pure black, because a `SubImage` of the render target silently
draws nothing when used as a source on the Direct3D backend. Nothing
errored. It simply looked wrong, and nothing was looking.

### The icon

The drawing is the source: `appicon` gives the icon in fractions of its
side and renders it at whatever size is asked for, so there is no image
file to edit by hand. A window sets its own icon from it, which is what
the window frame and the taskbar show while gridterm runs.

The executable's own icon, which Explorer and a pinned shortcut show, is
a Windows resource built from the same drawing.
`rsrc_windows_amd64.syso` is checked in and `go build` links it by its
name alone, so building needs neither the network nor an extra tool. Run
`make icon` after changing the drawing, and a test fails if you forget.
Only `windows/amd64` gets one: the name is what the toolchain matches
on.

## The ebiten fork

`go.mod` replaces ebitengine with
[marrasen/ebiten](https://github.com/marrasen/ebiten), at
`v2.10.2-gt.1`. That is upstream v2.10.2 plus 404 lines across six
files, and the note beside the replace line says why.

Upstream reports input by polling: `IsKeyPressed` says whether a key is
down at the moment it is asked, and the text a keystroke produced comes
back separately. That cannot tell Ctrl+C from the letter c, which rules
out writing a terminal against it.

The fork adds `AppendInputEvents`. Each event is a key transition with
its action -- press, release or OS repeat -- and its modifiers, or a
committed code point with the modifiers reported alongside it. The two
share a nonzero `Source` when the platform knows one produced the other,
so a caller can tell which keystroke a character came from rather than
guessing. It also reports the real paths of dropped files, beside the
filesystem they arrive as.

All of it is additive. `Runes` is still filled, the polled key state is
unchanged, and a repeat leaves the pressed and released times alone
rather than reporting a held key as released, so nothing reading the old
API sees a difference.

**This replaced a much larger fork.** gridterm used to sit on
marrasen/ebiten's fork of
[unstablebuild/ebiten](https://github.com/unstablebuild/ebiten) at
v2.7.5 -- 3,189 lines across 61 files, carrying unstablebuild's own
unrelated work and patching GLFW's C sources. Upstream rewrote that
layer into Go for v2.10, which made the C patches meaningless and the
whole feature expressible in one small additive change. It also took cgo
out of the Linux build: see [LINUX.md](LINUX.md).

The upstream request is still worth making. Nothing here is
gridterm-specific, and every terminal written against ebitengine needs
it.
