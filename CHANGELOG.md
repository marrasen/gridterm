# Changelog

What changed in each release, for somebody deciding whether to take it.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the numbering follows [semantic versioning](https://semver.org/spec/v2.0.0.html).

While the major version is 0 the shape is still moving: a minor bump may
change how something behaves.

## Unreleased

### Changed

**A connection's account opens in a pane, and the row of a connection
that dropped opens it.** `Connection Log` showed the account in a dialog
that had to be dismissed; it is a pane now, the way the window log is,
so it scrolls and copies like anything else on screen. The row of a
machine with nothing open on it used to answer a click with nothing at
all, and the row of one that dropped did the same -- so the moment there
was most to explain was the moment the window had least to say. Both
open the account now, and it outlives the connection it is about: the
reason a connection was lost is written into the account, where there is
room for it, rather than onto the row, where a sentence either pushed
the name off the end or did not fit and was dropped without a mark.

**The File menu's shells say only which shell they open.** Under the
`New Terminal In` heading each line read `New Terminal: Command Prompt`,
so the heading and the line said the same three words before either got
to the shell. The lines are now `Command Prompt`, `Windows PowerShell`
and the rest, which is what the same list already said on a machine's
plus menu. The commands keep their full titles for the palette, where
they are read with no heading around them.

## v0.2.1

### Fixed

**The line along the bottom row is no longer hidden behind the
sidebar.** The sidebar is drawn on a layer of its own over its columns,
and the line started at the first of them: `Shortcuts reloaded` is
shorter than the sidebar is wide, so it was invisible altogether, and so
was the sentence a menu puts there for the row under the cursor. It now
starts beside the sidebar. This is also what `Check for updates` says
when this build is already the newest release, so in v0.2.0 that answer
could not be read at all.

## v0.2.0

Every dialog and every command title reworded, and the about dialog can
now say which build it is.

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

**What a dialog needs, instead of a paragraph explaining itself.** A
field carries one hint line, drawn along the bottom while that field has
focus. A field that does not apply is disabled -- dim, taking no keys,
with the focus stepping over it -- rather than taken and quietly
dropped. A field with a list of values draws `Ctrl+↑↓` beside itself. A
notice leaves `Copy` out when there is nothing worth copying, and the
palette draws a tick for a switch the way a menu does. And there is a
status line: one line along the bottom row for four seconds, for an
action that worked and has nothing to read.

### Changed

**Every dialog is reworded, and eight behaviours changed rather than
being excused.** Titles say what happened, bodies add only what the
title cannot, buttons are one verb from a small set, errors are two or
three words, and placeholders are a format or the word `Optional`. Where
a sentence existed to excuse a behaviour, the behaviour changed instead:
the passphrase dialog asks until the key opens or you cancel, rather
than counting down from three tries on a file you can already read; the
tunnel's direction is a field rather than two buttons explained in the
body; the overwrite question has a `Cancel`, which is what dismissing it
already did; the copy dialog's button that renamed itself between
`Remember` and `Forget` is a tick box; the server dialog's jump host and
agent tick are disabled where a window cannot use them, rather than
taken and then explained away; two dialogs that only said something
worked are status lines; and the shortcuts file's rules are in the file,
where somebody editing it is looking. `Find in Scrollback` opens with
the find prompt up, so its name is true when it arrives. A server's
mid-handshake message can open the one link it carries, when it carries
exactly one and that link is http or https -- never on its own, and with
the focus staying on `Close`.

**Every command has a new title.** Title Case, verb first, two to four
words: `Close Pane`, `Reload Themes`, `Open Tunnel…`. An ellipsis means
the command asks something before it acts. Every word a title dropped is
still typed into the palette to find it, so whoever learned the old
wording still gets there. Command ids do not change, so a saved shortcut
still points at the same thing. A command that fails is headed by its
own title -- `Open Tunnel failed` -- and every error heading has one
shape: `Could not <verb> <object>`.

**The sidebar's rows say what a connection is, in the same words
everywhere.** Three were sentences that also named the window the row
sits under: `the window stopped sharing` is `stopped sharing`, `the
window closed this connection` is `closed by that window`, and `no
longer serving` is `connection lost` -- which is what a dropped
connection says whether it was carrying one pane or a whole window.
`given up on` is `cancelled`.

**The wording rules are written down.** WORDING.md is how a dialog,
button, menu row, command title and error are worded; DIALOGS.md and
COMMANDS.md record what those rules were applied to. Every button title,
field label and dialog title is a constant in one file, so no word can
drift from a second copy of itself.

### Fixed

**A wrong passphrase is asked about again, and said out loud.** Typing
the wrong passphrase for a private key used to be silent: the dialog
closed, the key was never offered, the connection went on to whatever
else it could try, and every line in the account stayed green. It is now
asked for again, with the dialog saying that the last one did not open
the key, until the key opens or you cancel. A wrong passphrase no longer
falls through to another way of signing in, and the key is named in what
the connection failed with -- even when the connection was made some
other way in the end.

**A window that stopped sharing is no longer reported as a connection
that dropped.** A window says so down the control channel before it
goes, so the other end can tell a deliberate stop from a connection that
broke. That line could be thrown away before it was read: the row then
said the connection was lost, with a reset beside it, and the window
offered to reconnect -- for something the user had done on purpose. The
window now waits for the other end to say it has the line.

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
