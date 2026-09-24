# Changelog

What changed in each release, for somebody deciding whether to take it.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the numbering follows [semantic versioning](https://semver.org/spec/v2.0.0.html).

While the major version is 0 the shape is still moving: a minor bump may
change how something behaves.

## Unreleased

### Added

**The secrets open in a pane, and can be taken out again.** `Manage
Secrets` lists what is in the vault and the keys that open it, and is
where a secret is copied, read, changed and removed -- several at once,
without a dialog that goes away on the first pick. `Show Secrets` stays
as it was, because `Type` sends a secret to the program in the pane in
front and only a dialog drawn over that pane knows which one that is.

`Export Secrets` writes every secret to a plaintext CSV, and
`Import Secrets` reads one back. A password manager nobody can leave is
one nobody should adopt, so the way out is plain text -- that is what
every other manager reads -- in the columns a browser writes, which is
the nearest thing to a standard there is. The import knows the headers
Chrome, Bitwarden, LastPass, KeePassXC and 1Password write, because
none of them agree. Both say to remove the file afterwards: it is the
one place every secret sits in the clear.

**A passphrase can open the secrets, for when every key is gone.**
Every slot was an SSH key, so losing them all lost the secrets, and
copying the file did not help because the copy wanted the same keys.
`Add Secrets Passphrase` adds a slot opened by something known rather
than something held, and then a copy of the file is a backup that
survives losing the lot.

Nothing adds one. It is the weaker door -- an ed25519 key is a hundred
and twenty-eight bits in a file, and a passphrase is what somebody
typed -- so it is a command, the dialog says what it costs before it is
added, and it opens on `Cancel`. What a guess costs is written into the
slot, so it can be raised later without shutting anybody out of a vault
sealed under the old cost.

### Changed

**What this window is serving is a pane too.** It was a dialog, which
could only say who was connected at the moment it opened -- and what it
is about changes while it is up, as windows connect and go. The pane
follows: the address, the fingerprint to check this machine by, and one
line per window working here, with `Disconnect` and `Stop serving`
along the bottom. The row of a window working in this one opens it,
which is what that row could not do before: it stood in for a pane
because there was none to put in front.

**File work is watched in a pane, not a dialog.** Clicking a copy's row
on the sidebar opened a box over the window, which took the keys and had
to be dismissed before anything else could be done -- for work that
takes as long as it takes. It opens a pane now, and the pane has room to
say more than the box could: how far it has got, drawn as a bar that
moves in eighths of a cell; how much has moved, how fast, and how long
is left; the last seconds of it as a run; and the names it was given,
ticked off as it passes them. `Cancel`, `Repeat` and the box that keeps
a copy are along the bottom, and `Close` takes the pane away and leaves
the work running. `Repeat` watches the new run in the same pane, so a
copy done again and again does not leave a pane for every time.

### Added

**An agent can type, press and wait in one call.** `send_keys` takes a
list of steps -- `type:`, `key:`, `wait:<ms>`, `until:<text>` and a bare
`until` -- and works them in order. What it is for is what does not
happen between them: a command, the program it starts, and what is typed
into that program used to be three calls with a gap in each, and in
every gap the pane could be something other than what the agent thought.
Driving vim to write a three-line file took an agent nineteen calls, and
takes six now, with the whole editing session in one of them. A step
that does not do what it says stops the list there and the answer says
which one, how the waiting ended, and what the pane looked like, so the
steps after it never go to the wrong program. A wait for text has to
have seen that text: one that ended because the command finished
instead -- `cd somewhere && vim notes.md` with the directory wrong --
stops the list rather than typing an editor's keystrokes at a shell
prompt. `require:<text>` and `fail:<text>` are the same check asked for
directly: go on only if the pane says this, or stop if it does.

**A list of steps answers with what each of its waits saw.** A list can
run several commands -- type, Enter, until, then the next one -- and the
answer carries each one's output, headed by the step and what that
command exited with. The alternative is what agents do without it: chain
three commands on one line with semicolons, where the outputs run
together and a single exit status covers all three. That is also what
leads an agent to clear the screen before every command so that what
comes back is only its own, which throws away what the user had in front
of them.

**A wait can watch for text to arrive rather than text being there.**
`wait_for` matches the screen as it already is, which is right for it:
sending keys and waiting are two calls, and anything short has finished
before the second one lands. But the text an agent waits for is usually
a word it just typed, and a terminal echoes what is typed -- so "wait
until it says done" after `echo done` ended before the command had run.
`since_keys` waits for the text to arrive instead. Inside a list of
steps it is always on, because a list has no gap in which the text could
arrive unseen.

**A vault for passwords and notes, opened by a key you already
unlock.** `Show Secrets` lists what is in it; `Add Secret` and `Add
Note` put things in; `Change Secret` and `Remove Secret` deal with what
is there. It is a file called `secrets.json` beside everything else the
window saves, sealed with a key of its own, and that key is wrapped once
for each SSH key allowed to open it. So `Add Secrets Key` lets a second
machine's key in without re-encrypting anything, and `Remove Secrets
Key` takes one away with only its own wrapping.

The key has to be ed25519, because a slot is opened by having the key
sign a fixed challenge and only ed25519 signs the same way every time.
It is one of the keys already unlocked to reach a server, so the vault
usually opens for nothing: the passphrase typed at the first connection
is the whole of it. `Lock SSH Keys` locks the vault with them.

No secret is ever drawn on a row. `Copy` puts one on the clipboard and
says so without showing it, and takes it back off thirty seconds later
unless something else has been copied since. A window closing does the
same thing there and then, because the timer that would have done it
posts work nobody is left to run. `Type` sends it to the program in the
pane in front, so it never reaches the clipboard at all; a return goes
with it only when something is waiting for a whole answer. `Show` is the
only thing that puts a secret on screen, and it has to be asked for.

**A password gridterm makes, and the machine in front of you
suggested.** `Generate` on the add and change dialogs writes twenty
characters and leaves the dialog open so they can be read back with
`Show`. They are letters and digits with the lookalike pairs left out --
no `l` or `1` or `I`, no `O` or `0` -- because a password kept in a
vault is still read aloud now and again. No symbols: a symbol buys about
as much as one more character does, and it is the thing a server's own
rules refuse.

The `For` field starts on the machine the user is looking at and offers
the rest of the machines the window knows of. It is a suggestion in a
field that can be cleared.

**A new SSH key can have its passphrase generated and kept in the
vault.** `New SSH Key` has a `Generate passphrase` tick. With it on,
gridterm makes the passphrase, locks the key with it and puts it in the
vault, and nobody is shown it -- there is nothing to write down and
nothing to lose. Unlocking that key from then on takes the passphrase
out of the vault instead of asking. One that the key refuses falls
through to the dialog, and a key the vault knows nothing about is asked
about the way it always was.

It only reads a vault that a key already unlocked opens: asking for a
passphrase to read a passphrase would be a dialog to spare a dialog, and
the key being unlocked may be the vault's own. The tick is only offered
where there is a vault to put one in.

A key whose passphrase is in the vault is no use as a spare for it: with
the other key gone, opening the vault needs this key and unlocking this
key needs the vault. `Add Secrets Key` marks that key in the list and
says so before adding it, and adds it anyway when told to -- the
passphrase can be copied out and kept elsewhere, and then it is a spare
like any other.

**A word before the secrets are trusted to a key the SSH agent has.** A
slot is opened by that key signing, and an agent signs for whoever it
is forwarded to, so `A server you forward the agent to can open any
copy of the secrets it has.` Starting a vault on such a key, or adding
one to a vault that exists, now says that first and goes ahead when
told to: it is a cost only to somebody who has a copy of the file as
well, and whether that is worth it is the user's to weigh.

This and the warning about a key whose passphrase is in the vault are
one question, because a key can have both against it. It opens on
`Cancel`, the way every question about exposing something does.

The agent is asked by fingerprint, off the `.pub` file beside the key,
so nothing has to be unlocked to ask, and a key with no public half or
an agent that will not answer gets no warning rather than one the
window cannot stand behind. It is a snapshot: the key may be added to
the agent a minute later. The key `New SSH Key` offers by default,
`id_ed25519_gridterm`, is not one most people load into an agent, and
agent forwarding is off unless a saved server turns it on.

### Changed

**The sidebar's notes go quiet.** A note is the second thing on a row
and the first thing to crowd it: it takes its room from the name, which
is what the row is for. A note is now shown while it is changing -- for
as long as the status line holds a line -- and then comes off the row,
leaving the name the width back. The pointer on the row brings it back,
and so does the selection while the sidebar has the focus. A copy says
`3 of 7` and then `4 of 7`, so its note is up the whole time it runs,
and a connection that has settled goes back to being a name.

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

**The secrets went through the wording rules a second time.** Five
dialogs said `Take "Add Secret"` where the vocabulary table has
`choose` for a menu line. The `Show` button on the add and change forms
renamed itself to `Hide`, which rule 9 calls a bug wearing an
explanation; it is a `Show the secret` tick now, beside the field it is
about. The list `Show Secrets` opens was titled `Secrets` and the two
key lists both said `Choose a key`, so two different jobs shared a
heading; each is titled with the command that opens it. The question
before a key is revoked offered `Remove` and `OK`, where `OK` reads as
agreeing to the removal, and carried a `Copy` button over a body with
nothing in it to copy; it is `Remove` and `Cancel`. Two warnings that
ran to two sentences are one each. And the line over the add and change
forms said `Sealed in the vault`, which was the only place on screen
that called the secrets anything but the secrets; it now says `Only
your key opens the secrets.`, which is the one thing the title cannot
say. Every error the `secrets` package reports went the same way: they
said `the vault`, and they are read under a heading that has just said
`the secrets`. The ones that also said make, take or holds now say
create, create and is, which is what the vocabulary table has.

**A list's buttons look and answer like every other dialog's.** The row
along the bottom of a chooser drew its actions as names in square
brackets, so `Show Secrets` offered `[ Type ] [ Copy ] [ Show ]
[ Cancel ]` while every dialog beside it drew real buttons. They are now
the same buttons, right aligned from the corner the eye lands on, and
clicking one works. A form also takes left and right along its button
row, which a notice and a chooser already did -- so every dialog in the
window is answered the same way. In a field the arrows are still the
caret's.

**The File menu's shells say only which shell they open.** Under the
`New Terminal In` heading each line read `New Terminal: Command Prompt`,
so the heading and the line said the same three words before either got
to the shell. The lines are now `Command Prompt`, `Windows PowerShell`
and the rest, which is what the same list already said on a machine's
plus menu. The commands keep their full titles for the palette, where
they are read with no heading around them.

### Fixed

**A window closing waits for file work the user dismissed.** Dropping a
copy cancels it and takes its row away, and cancelling is not stopping:
the write it is in finishes, and one waiting on a machine that has
stopped answering waits however long that takes. The queue stopped
answering for it the moment it left the list, so a window could close
while it was still writing.


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
