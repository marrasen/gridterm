# gridterm on Linux

It runs. Marcus asked for this on 2026-09-19; it was built, tested and
opened on a Linux desktop on 2026-09-21. macOS still waits: neither of
us knows anybody who runs a terminal on one.

What follows is what is true now, then what is left.

## The short of it

The fork question is settled twice over. It was settled the first time
by trying it: the old `v2.7.5-ub.27.win.1` fork built on Linux
unchanged, and the `.win.1` tag meant what the note beside it said -- a
build tag, not a Windows feature.

It is settled properly now. gridterm sits on `v2.10.2-gt.1`, which is
upstream v2.10.2 with the key-event pipeline added in 404 lines. v2.10
rewrote ebitengine's GLFW layer from C into Go, so the Linux build needs
no cgo, no headers and no C toolchain at all.

`go build ./...` passes, `go test ./...` passes, and the window opens on
X11 and runs a shell.

## Building it

Nothing but Go 1.27.1, which `go.mod` asks for and `GOTOOLCHAIN=auto`
fetches.

```
go build ./...
go test ./...
```

No C toolchain, no X11 development headers, no display for the tests,
and no cgo: `CGO_ENABLED=0` is the whole build. A Linux release
cross-compiles from Windows and the other way round.

That was not true when this file was first written. The build needed
`libx11-dev` and the rest, because ebitengine's bundled GLFW was C.
Upstream rewrote that layer in Go for v2.10, gridterm's fork moved onto
it, and the requirement went with it -- along with the glibc floor: the
binary asks for no versioned glibc symbol at all now, where the cgo
build wanted GLIBC_2.34.

What a built gridterm needs to run is a desktop's own libraries, opened
when it starts. A desktop has them.

Wayland was not needed. The desktop it was built and run on is X11, and
ebiten used the X11 path without being asked.

## What was fixed to get here

Five things, and only one of them was Linux code. The last one is its
own section below, because it is a Windows fix as much as a Linux one.

**The clipboard.** `clipboard_linux.go` is new and does the work that
`clipboard_image_other.go` used to refuse: `clipboardHasText`,
`clipboardImage`, `setClipboardImage`, and now the text side as well.

It goes through `golang.design/x/clipboard`, which talks to X11 itself.
The old text path shelled out to `xclip` or `xsel` through
`atotto/clipboard`, and neither is part of a desktop: on the machine
this was built on, neither was installed, so copy and paste did nothing
at all. Text and pictures now go through one library, which they have to:
X11 gives the clipboard to a single owning process, so text written by a
helper process and pictures written by this one would take the clipboard
from each other on every copy. Windows and macOS are untouched and still
use `atotto/clipboard`, in `clipboard_text_other.go`.

A picture that cannot be read is still an error, told apart from a
clipboard that holds no picture, which is what the paste command needs.
`clipboard.Init` is asked once, lazily, and never at startup: a gridterm
with no display still runs, and there the clipboard is simply not one of
the things it can do.

All of it was checked against a separate process, not only in a test.
Text written by another program pastes into a pane; text copied out of a
pane is read back by another program; a PNG put on the clipboard by
another program arrives in the pane as a file whose pixels are identical
to what was sent. The desktop it was checked on runs `csd-clipboard`,
Cinnamon's clipboard manager, so what gridterm copies outlives gridterm
there; a desktop without one would lose it when the window closes, which
is X11 rather than gridterm.

**Reading a Windows path on a machine that is not Windows.**
`shells.CommandBase` is new, in `shells/argv.go`, and `shells.IsWSL` and
`shellsetup.RouteFor` use it. Both take a command line apart to find the
program in it, and both used `filepath.Base`, which splits on the
separator of the machine it is running on. A Linux build therefore read
`C:\Windows\System32\cmd.exe` as one long name and recognised no shell
in it -- so a pane running a Windows shell, over SSH to a Windows server
or through WSL, would have been typed bash prompt hooks. It now splits on
both separators everywhere and gives the same answer on every machine.

**A name beside a file.** `beside` in `jobs/run.go` refused a name
holding `/` or the filesystem's own separator. On Linux that let a
backslash through, so the rule changed with the machine. It refuses both
everywhere now.

**Five tests that described Windows rather than gridterm.** The quoting
of a path on a command line follows the platform's shell and the tests
said double quotes; the default shell is COMSPEC only on Windows; a
connection to a closed port hangs on Windows and is refused at once on
Linux, which two takeover tests were built on. None of these were
faults in the window.

## Two flaky tests, now fixed

Both predate this work and both showed up on Linux. They were races, not
platform differences, and each was found by taking a goroutine dump of a
hung run.

`TestAParkedRelayFromAnOldConnectionLeavesTheNewCountAlone` waited for
the machine's own count of file sessions before cutting the connection.
That count goes up when the request to open one arrives, while the answer
is still crossing, so the test sometimes cut the connection mid-handshake
and left the window waiting for an answer that was never coming. It now
waits for the window to start carrying bytes, which is the thing it
actually meant.

`TestClickingAWholePathOpensIt` printed a path longer than the pane is
wide and looked for a run of characters the wrap fell inside. Where it
broke depended on how many digits the machine put in a temporary
directory's name, so it passed about seven times in ten. It now looks at
the text with the row breaks taken out.

## The rest of the survey, as it stands

**The shells.** Nothing to do. `shells.Find` returns the login shell on
Linux, and `localArgv` asks `session.DefaultShell` rather than working
it out for itself.

**Fonts.** Nothing to do. `gridterm -list-fonts` on the test machine
found DejaVu Sans Mono, Liberation Mono, Nimbus Mono PS, Noto Sans Mono
and the Noto CJK families. The list in `glyph/fallback.go` is still a
guess rather than a fontconfig query, and still wants trying on a machine
with a thin font set.

## Pasting a picture into a POSIX shell

Found by driving the window, and fixed. It was never Linux-only, and the
fix is not either.

With a picture on the clipboard and nothing else, the ordinary paste
shortcut reaches `pastePicture`, and for a pane on this machine that was
`pane.PressPaste()` -- which sends the program a literal ctrl+V. That is
the right thing more often than it looks. gridterm cannot hand a picture
down a pty, so what it does is nudge the program to go and read the
clipboard itself, which is how Claude Code and the rest take one as a
picture rather than as a path.

It is wrong in one place: the shell's own line editor. readline reads
ctrl+V as `quoted-insert`, which takes the next character literally, so
pressing it at a bash or zsh prompt left the shell quoting the beginning
of whatever was pasted next. That paste then showed its bracketed-paste
markers as text instead of obeying them, and the shell stayed that way
with nothing on screen to say why.

**This hit Windows too.** Not through a pane on a machine at the far end
-- those are `hostMachine` and were already handed a file. Through WSL: a
WSL pane is local, so it is `hostHere`, and it runs bash.

Two questions decide it now, and neither is about which machine gridterm
is running on:

- Does this pane run a shell that reads ctrl+V that way?
  `shellsetup.RouteFor` already answers, and answers the same everywhere
  now that it reads a Windows path through `shells.CommandBase`.
- Is that shell what is reading right now, rather than a program it
  started? The shell says so itself, in the OSC 133 marks shell setup
  puts there and which are on by default. `term.RunningAProgram` is that
  answer, with the alternate screen counting as a program on its own.

So ctrl+V is kept for the case it is good for -- a program is running
and the shell says so -- and the picture goes as a file otherwise. A
shell that sends no marks lands on the file too: not knowing is not a
reason to send a key that breaks a shell silently, and a path is
something every program here reads already, which is what a pane on a
machine at the far end is handed anyway.

The Command Prompt and PowerShell are untouched, because ctrl+V really
is paste there.

## What has still not been looked at

- **A release.** The CI runner is one Windows box, pinned by hand. Linux
  binaries need a second runner or a container.
- **Wayland.** ebiten chose X11 here and was never put to the question.
  Whether the fork's key-event pipeline behaves the same on Wayland is
  unknown.
- **The icon and the taskbar.** `appicon` builds and the window title is
  right -- it reads `gridterm — rdp@marras-skylake: /tmp`, so OSC 7
  reaches it. What a Linux desktop does with the icon has not been seen.
- **The primary selection.** Middle-click paste, and the selection
  clipboard as distinct from the clipboard, are still not implemented.
  The library now in use reaches both -- `clipboard.FromPrimary` -- so
  this is a smaller job than it was.
- **Anything else a Linux user expects a terminal to do.** The shortcuts
  have not been looked at against what a Linux terminal usually binds.

## Driving it without a person

`gridterm -shot` runs a script of steps and writes PNGs, which is how the
window was checked here:

```
gridterm -shot "wait:150 shot:before.png type:pwd key:enter wait:120 shot:after.png"
```

It is the way to take a picture of the window from a script, because
ebiten draws only inside its own loop and there is no reading a frame
back without one.

For driving it rather than photographing it, `xdotool` works on an
unlocked screen, and is the better tool: its key presses go through X11
and so through the fork's key-event pipeline, which is the part of this
that was worth doubting. Typing, chords such as ctrl+shift+K, and
mouse selection by drag all arrive. A locked screen is the one thing
that stops it: the screensaver holds the X keyboard grab, and synthetic
events sent straight to the window with `--window` are ignored, because
GLFW drops anything with the `send_event` flag set.
