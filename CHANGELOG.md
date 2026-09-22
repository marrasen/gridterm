# Changelog

What changed in each release, for somebody deciding whether to take it.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the numbering follows [semantic versioning](https://semver.org/spec/v2.0.0.html).

While the major version is 0 the shape is still moving: a minor bump may
change how something behaves.

## Unreleased

### Added

**The about dialog says which build this is, and checks for a newer
one.** It said `Version: development build` whatever it was. It now says
what the build calls itself -- the tag for a release, `dev-<commit>` for
a build from a working tree -- and it can be copied, because that is the
first thing a bug report needs. Beside `OK` is `Check for updates`,
which asks GitHub for the newest release. A newer release opens a dialog
naming both versions and the page it is on, with `Open`. A build that is
already the newest release, or later than it, gets a line along the
bottom row instead of a dialog to dismiss. A build from a working tree
is not put in that order at all, because it may hold work no release
has: both versions are named and the choice is left to whoever is
reading. The check is manual, and nothing asks GitHub until the button
is pressed.

### Fixed

**A wrong passphrase is asked about again, and said out loud.** Typing
the wrong passphrase for a private key used to be silent: the dialog
closed, the key was never offered, the connection went on to whatever
else it could try, and every line in the account stayed green. A
passphrase that does not open the key is now asked for again -- three
tries, the way ssh does it -- and the dialog says the last one did not
work and how many tries are left. When the tries run out the account
says so in red, and the key is named in what the connection failed with,
even when the connection was made some other way in the end.

## v0.1.0

The first release. Windows and Linux, on amd64.

### Added

**The terminal.** A GPU-rendered character grid: a full screen of text
is typically two `DrawTriangles` calls, and a screen that did not change
draws nothing at all. Alternate screen, scroll regions, scrollback, 256
and true colour, the text attributes, cursor shapes, device reports,
bracketed paste and mouse modes. bash, vim and less all run. Wide
characters take two columns, combining marks share a cell, and the box
and block characters are drawn at the exact cell size so a framed TUI
has unbroken lines.

**Keyboard.** Press, release and OS repeat with modifiers, correlated
with the text a keystroke produced, encoded to the bytes a program
expects -- including application cursor mode, which vim and readline
need.

**Shells here and on other machines.** A local pseudo-terminal, a ConPTY
on Windows, or SSH. One connection carries several things at once, so a
second terminal on a machine is a second channel rather than a second
login. A saved server can sit behind another one, and the second
connection rides inside a channel of the first.

**A file manager, with as many panes as you want.** Panes on this
machine and on any server at once, keyboard-driven the way a two-pane
browser has worked for thirty years, with copy, move and delete running
in the background and saying how far they have got. A name that is
already there is always asked about. Files land whole or not at all.

**A file reader without a shell.** Paging, search, go-to-line and a hex
dump, on this machine or a server. Code is coloured by what the file is
called, markdown gets its headings, and a picture file shows the
picture. A file can be tailed as it grows. A strip beside the file shows
the shape of the whole of it, with a second column for how bad a log
line got.

**JSON logs read as logs**, laid out in columns with the level coloured,
reading the field names every logger spells differently.

**Links and paths in the output.** Ctrl and a click follows an OSC 8
link, an address in the text, or a file the output named -- including on
a server, and at the line a compiler named. A path is checked against
the disk before it counts as a link.

**Pictures in a pane.** OSC 1337, on a layer of its own, scrolling with
the text.

**Files dragged into a pane**, landing in the directory the shell is in
rather than being typed, copied to the far machine first when the pane
is on one.

**Tunnels**: local, remote and SOCKS5, with a pane for each showing what
it is carrying and a way to watch what goes through it. Anything that
would let the rest of the network in asks first.

**One gridterm working inside another.** A window serves itself on a
port you opt into; another window takes it over, draws its panes, reads
its files over the same connection, and can join a program already
running there so both people see it. Key authentication only.

**Panes shared with an agent**, over the Model Context Protocol
(`gridterm -mcp`). One code lets an agent read and type into exactly the
panes you shared and nothing else. Nothing listens until you share a
pane, and taking the last one back makes the code useless at once.

**The WSL filesystems**, browsable like any other directory.

**A sidebar** rather than a row of tabs: every terminal, file pane,
tunnel and transfer under the machine it is on, with saved servers
listed whether or not anything is connected.

**Shell integration** that teaches bash, zsh, PowerShell and the Command
Prompt to say where they are and how each command went.

**Secrets asked for in the window** -- key passphrases, passwords,
one-time codes -- held in memory only. Unknown host keys are shown with
their fingerprint and recorded only on an explicit yes; a key that
changed is a hard failure. The SSH agent is carried to a machine only
when you turn it on for that machine.

**Linux support.** The window, the clipboard and the fonts all work on
X11. On a Wayland desktop it runs through XWayland. See
[LINUX.md](LINUX.md).

**Nothing to install to build it.** Every build is pure Go:
`CGO_ENABLED=0`, no C toolchain, no development headers, and each
platform cross-compiles from the other. The Linux binary asks for no
versioned glibc symbol at all, so it runs on far older distributions
than a cgo build would. arm64 builds for both platforms, though nobody
has run one. gridterm carries a fork of ebitengine for the key-event
pipeline; it is 404 lines on top of upstream v2.10.2, and **The ebiten
fork** in [BUILDING.md](BUILDING.md) says why.

**WSL panes are given paths their distribution can open.** A file
dropped on one, and a picture pasted into one, are named the way that
distribution names them -- `/mnt/c/...` rather than a Windows path the
program in the pane cannot open.

### Known gaps

No release for macOS. [GAPS.md](GAPS.md) says what else is not there
yet.

The cursor in a pane on another machine is held on for a fifth of a
second after a program hides it, so that the hide and show a repaint
makes never reaches the screen. It fixed a flicker that neither of the
two people who looked at it could reproduce directly, and it rests on
tests of the mechanism rather than on having watched the symptom go. If
a cursor lingers where it should not, that is the thing to suspect.
