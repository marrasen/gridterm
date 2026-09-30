# Kakel on Linux

It runs. Marcus asked for this on 2026-09-19; it was built, tested and
opened on a Linux desktop on 2026-09-21. macOS still waits: neither of
us knows anybody who runs a terminal on one.

What follows is what is true now, then what is left.

## The short of it

Kakel draws its window with gunim, which is pure Go and loads OpenGL
at run time. So the Linux build needs no cgo, no headers and no C
toolchain at all.

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

What a built kakel needs to run is a desktop's own libraries, opened
when it starts. A desktop has them.

The desktop it was built and run on is X11.

## What was fixed to get here

Five things, and only one of them was Linux code. The last one is its
own section below, because it is a Windows fix as much as a Linux one.

**The clipboard.** `clip/clip_linux.go` does the work that
`clip/image_other.go` refuses: `clip.HasText`, `clip.Image`,
`clip.SetImage`, and the text side as well.

It goes through `golang.design/x/clipboard`, which talks to X11 itself.
The old text path shelled out to `xclip` or `xsel` through
`atotto/clipboard`, and neither is part of a desktop: on the machine
this was built on, neither was installed, so copy and paste did nothing
at all. Windows and macOS still use `atotto/clipboard` for text, in
`clip/text_other.go`.

Since the move to gunim, `clip` carries less. Copying and pasting text
in a pane goes through gunim's own clipboard. `clip` is what pastes a
image, puts an image another window sent on this machine's clipboard,
and copies a secret.

An image that cannot be read is still an error, told apart from a
clipboard that holds no image, which is what the paste command needs.
`clipboard.Init` is asked once, lazily, and never at startup: a kakel
with no display still runs, and there the clipboard is simply not one of
the things it can do.

All of it was checked against a separate process, not only in a test.
Text written by another program pastes into a pane; text copied out of a
pane is read back by another program; a PNG put on the clipboard by
another program arrives in the pane as a file whose pixels are identical
to what was sent. The desktop it was checked on runs `csd-clipboard`,
Cinnamon's clipboard manager, so what kakel copies outlives kakel
there; a desktop without one would lose it when the window closes, which
is X11 rather than kakel.

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

**Five tests that described Windows rather than kakel.** The quoting
of a path on a command line follows the platform's shell and the tests
said double quotes; the default shell is COMSPEC only on Windows; a
connection to a closed port hangs on Windows and is refused at once on
Linux, which two takeover tests were built on. None of these were
faults in the window.

## The rest of the survey, as it stands

**The shells.** Nothing to do. `shells.Find` returns the login shell on
Linux, and `app.localArgv` asks `session.DefaultShell` rather than
working it out for itself.

**Fonts.** Nothing to do. `kakel -list-fonts` on the test machine
found DejaVu Sans Mono, Liberation Mono, Nimbus Mono PS, Noto Sans Mono
and the Noto CJK families.

## Pasting an image into a POSIX shell

Found by driving the window, and fixed. It was never Linux-only, and the
fix is not either.

With an image on the clipboard and nothing else, the ordinary paste
shortcut reaches `pastePicture`, and for a pane on this machine that was
`PressPaste` -- which sends the program a literal ctrl+V. That is
the right thing more often than it looks. Kakel cannot hand an image
down a pty, so what it does is nudge the program to go and read the
clipboard itself, which is how Claude Code and the rest take one as a
image rather than as a path.

It is wrong in one place: the shell's own line editor. readline reads
ctrl+V as `quoted-insert`, which takes the next character literally, so
pressing it at a bash or zsh prompt left the shell quoting the beginning
of whatever was pasted next. That paste then showed its bracketed-paste
markers as text instead of obeying them, and the shell stayed that way
with nothing on screen to say why.

**This hit Windows too.** Not through a pane on a machine at the far end
-- those were already handed a file. Through WSL: a WSL pane is local,
and it runs bash.

Two questions decide it now, and neither is about which machine kakel
is running on:

- Does this pane run a shell that reads ctrl+V that way?
  `shellsetup.RouteFor` already answers, and answers the same everywhere
  now that it reads a Windows path through `shells.CommandBase`.
- Is that shell what is reading right now, rather than a program it
  started? The shell says so itself, in the OSC 133 marks shell setup
  puts there and which are on by default. `RunningAProgram` in
  `ui/term` is that answer, with the alternate screen counting as a
  program on its own.

So ctrl+V is kept for the case it is good for -- a program is running
and the shell says so -- and the image goes as a file otherwise.
`shellWouldQuoteIt` in `app/images.go` asks both questions. A
shell that sends no marks lands on the file too: not knowing is not a
reason to send a key that breaks a shell silently, and a path is
something every program here reads already, which is what a pane on a
machine at the far end is handed anyway.

The Command Prompt and PowerShell are untouched, because ctrl+V really
is paste there.

## What has still not been looked at

- **Wayland.** The window has only been run on X11. How it behaves on
  Wayland is unknown.
- **The icon and the taskbar.** `appicon` builds, and the window title
  follows the pane in front. What a Linux desktop does with the icon has
  not been seen.
- **The primary selection.** Middle click pastes the clipboard. The
  selection clipboard, as distinct from the clipboard, is still not
  implemented.
  The library now in use reaches both -- `clipboard.FromPrimary` -- so
  this is a smaller job than it was.
- **Anything else a Linux user expects a terminal to do.** The shortcuts
  have not been looked at against what a Linux terminal usually binds.

## The tray icon

kakel's tray icon is a StatusNotifierItem, over D-Bus. Cinnamon, KDE,
XFCE with its status notifier plugin, and most other panels show it.
Stock GNOME does not: it needs the AppIndicator extension. With no tray
to show the icon in, kakel runs as it did before it had one, and
closing its last window ends it. Options › Tray Icon turns it off.

It was tried on Cinnamon 6 over X11: the icon shows, its menu opens,
and a pick reaches kakel.

## Driving it without a person

`kakel -shot` runs a script of steps and writes PNGs, which is how the
window was checked here:

```
kakel -shot "until:$ shot:before.png type:pwd key:enter until:/ shot:after.png"
```

It is the way to take an image of the window from a script.

For driving it rather than photographing it, `xdotool` works on an
unlocked screen, and is the better tool: its key presses go through X11
the way a person's do. A locked screen is the one thing that stops it:
the screensaver holds the X keyboard grab.
