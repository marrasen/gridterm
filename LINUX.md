# Getting gridterm working on Linux

Marcus asked for this on 2026-09-19. macOS waits: neither of us knows
anybody who runs a terminal on one.

This is a survey rather than a plan. Every claim in it was checked
against the code on 2026-09-19, and each one says how.

## The short of it

Less is missing than it looks. Every package that is not the window
already builds for Linux:

```
CGO_ENABLED=0 GOOS=linux go build ./session/... ./serve/... ./vfs/... \
    ./remote/... ./conf/... ./shells/... ./themes/... ./grid/... \
    ./vt/... ./jobs/... ./ui/... ./agent/... ./appicon/...
```

That passes today. The pty layer, the window-to-window protocol, the
filesystems, the terminal emulator, the widgets: all of it is already
portable, and the five Windows files in `session/`, `vfs/` and `remote/`
all have `_unix.go` counterparts beside them.

What is left is four things, and only the first is real work.

## 1. Build it on Linux

`GOOS=linux go build ./...` fails on this machine, and the failure is
not gridterm's:

```
# runtime/cgo
linux_syscall.c:11:10: fatal error: grp.h: No such file or directory
```

That is a Windows box with no Linux C toolchain. ebiten needs cgo for
X11 and Wayland, so gridterm cannot be cross-compiled from here. It has
to be built on Linux, or in a Linux container.

What a build host needs:

- Go 1.27.1, which `go.mod` asks for. `GOTOOLCHAIN=auto` fetches it.
- A C compiler, and the X11 and Wayland development headers ebiten
  lists. On Debian that is roughly `libgl1-mesa-dev libx11-dev
  libxrandr-dev libxcursor-dev libxinerama-dev libxi-dev libxxf86vm-dev`
  and the Wayland equivalents.

**The fork is the open question.** `go.mod` replaces ebiten with
`github.com/marrasen/ebiten/v2 v2.7.5-ub.27.win.1`. The note beside it
says why: gridterm needs unstablebuild's key-event pipeline, because
upstream's polled `IsKeyPressed` cannot tell Ctrl+C from the letter c,
and `marrasen/ebiten` is that same commit with one Windows-only call put
behind a build tag.

The tag reads `.win.1`, which looks Windows-only, but the change it
carries is a build tag rather than a Windows feature — so it ought to
build on Linux unchanged. Nothing has tried. **First job: clone the
repo on a Linux machine and run `go build ./...`.** Everything below is
downstream of that answer.

If it does not build, the fix is to point the replace at
unstablebuild's own tag for Linux. A `go.mod` replace cannot be made
conditional on the platform, so that would mean one fork carrying both
fixes, which is what `unstablebuild/ebiten#windows-build` is for.

## 2. The clipboard

`clipboard_image_other.go` is the stub for everywhere that is not
Windows. It reports no picture and refuses to put one on the clipboard,
so nothing breaks, and pasting a picture does nothing.

Three things to write, in `clipboard_image_linux.go`:

- `clipboardHasText`
- `clipboardImage`
- `setClipboardImage`

On Linux the clipboard belongs to a running process rather than to the
system, and there are two of them: X11 and Wayland. The text side
already copes by shelling out — `atotto/clipboard` runs `xclip` or
`xsel`, which is why `clipboard.go` says the clipboard goes through a
goroutine of its own. The picture side can do the same:
`xclip -selection clipboard -t image/png -o` reads one, and
`wl-paste --type image/png` is the Wayland equivalent.

A library would be tidier than shelling out. `golang.design/x/clipboard`
reads and writes PNG, and pulls in cgo on Linux, which the build already
needs for ebiten.

Whichever way: **a picture that cannot be read must stay an error.** The
Windows reader tells "there is no picture" apart from "the clipboard
would not open", and the difference is what the paste command says to
the user.

## 3. The shells

`shells/shells.go` already answers for Linux: `find` returns the login
shell and nothing else when the machine is not Windows. WSL is asked
about only on Windows.

Worth checking once there is a window to try it in: `session/local.go`
picks `COMSPEC` when it is handed nothing, and `localArgv` in `panes.go`
falls back to `cmd.exe`. Both are behind a `runtime.GOOS == "windows"`
check. Read them again on the machine rather than trusting this
paragraph.

## 4. Fonts

`glyph/fallback.go` already has a Linux branch, and it looks for
DejaVu Sans Mono, Noto Sans Mono, Unifont, Noto CJK and FreeMono, under
the usual directories. Nothing to write. It wants trying on a machine
with a thin font set, because the list is a guess rather than a
fontconfig query, and the file says so.

The bundled faces need nothing: Go Mono and the IBM VGA set are
compiled in.

## What has not been looked at

- **A release.** The CI runner is one Windows box, pinned by hand, and
  the release flow is deferred anyway. Linux binaries need a second
  runner or a container.
- **Wayland versus X11.** ebiten picks one. Which it picks, and whether
  the key-event pipeline the fork adds behaves the same on both, is
  unknown.
- **The window title, the icon and the taskbar.** `appicon` builds for
  Linux, but what a Linux desktop does with it has not been seen.
- **Anything to do with how a Linux user expects a terminal to behave.**
  The shortcuts, the middle-click paste, the selection clipboard as
  distinct from the clipboard. None of that is decided.

## The order to do it in

1. Build it on Linux. That is one command on a machine, and it settles
   the fork question that everything else waits on.
2. Run the tests there: `go test ./...`. The suite is large and most of
   it does not touch the window, so this says a lot for very little.
3. Open the window and use it. What breaks will be clearer than any
   list written from here.
4. Then the clipboard, which is the one piece of real work this survey
   is sure about.
