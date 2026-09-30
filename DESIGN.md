# Design notes worth knowing

Why the code is the shape it is. Read this before changing how drawing,
input or the clipboard work.

## Layout

`main.go` is all of package `main`: it reads the command line and
starts the program. Everything else is a package of its own.

The window and the program:

- `app`: the program side. It owns the panes' programs, the
  connections, the files and the rest, publishes what the windows show
  as `State`, and carries out what they ask for as intents.
- `view`: the window. It draws the `State` with gunim and turns what
  the user does into intents.
- `look`: kakel's themes turned into gunim's, and the colours of the
  window's own parts.
- `winkeys`: keys as gunim hears them, turned into kakel's keys and
  back.
- `words`: numbers and names written the way kakel says them.
- `appicon`: kakel's icon, drawn at any size.

The terminal:

- `vt`: the VT emulator: parser, screen model, two buffers, scrollback,
  images.
- `grid`: the character grid, damage tracking and selection.
- `input`: key, text, mouse and paste events turned into VT bytes.
- `session`: a program as a byte stream, and the local pty.
- `screen`: the programs running in panes, each with its screen, and
  the session a watcher from another window gets.
- `ui/term`: a shell on a terminal, with the goroutines that move its
  bytes.
- `shells`: the shells a pane on this machine can run, and WSL paths.
- `shellsetup`: the lines that teach a shell to say what it is doing.
- `links`: which links and paths in a pane may be followed.
- `glyph`, `fonts`: the monospaced fonts installed, and the ones
  compiled in.
- `syntax`: colour for the lines of a file in the reader.

Machines and connections:

- `machines`: one record per machine kakel works on: its name, its ID,
  and what hangs off its connection.
- `remote`: SSH: connections, shells, host keys, unlocked keys, jump
  hosts, tunnels.
- `tunnel`: a port forwarded over a connection, and its account.
- `meter`: bytes moved, and how long ago.
- `serve`: one kakel window connected to another: the listener, the
  client, and what they say to each other.
- `vfs`: a filesystem a file pane works on: this machine, or one over
  SFTP.
- `jobs`: copying, moving and deleting in the background.
- `ui/files`: the reader, and the file panes' keys. `view` draws the
  file panes themselves.
- `pasted`: where a pasted image or a dropped file is written.

Agents:

- `agent`: panes shared with an agent: the code, the port, and what
  may be asked.
- `agentterm`: how an agent reads a terminal and types into it.
- `agenthost`: the agent programs kakel knows how to set up.
- `mcp`: shared panes over the Model Context Protocol, on standard
  input and output.
- `steps`: the steps that drive a pane, shared by agents and `-shot`.

Keeping things:

- `conf`: where kakel keeps its files.
- `settings`: the choices kakel remembers between runs.
- `keys`: the user's keyboard shortcuts file.
- `themes`: the themes built in and the ones the user wrote.
- `secrets`, `vaultkeys`: the secrets file, and the SSH keys that open
  it.
- `logs`: what the window logged, for the Window Log.
- `clip`: images on the clipboard, and text for the secrets.
- `notify`: a message outside the window.

`ui` is the old app's text-mode toolkit. What is left of it in use is
the keymap and what `ui/term` and `ui/files` are built on. `internal`
holds the build stamp, test helpers and the icon writer.

gunim is imported by `app`, `view`, `look`, `winkeys`, `settings` and
`main`. The terminal packages, `serve`, `remote`, `vfs` and `jobs` do
not import it. So everything fiddly is testable without a display,
which is how the emulator got written.

## The notes

**The grid is for text. Nothing else has to be cells.** A terminal is a
grid of characters, so the grid is what the emulator writes into and
what the window draws. That is right for text.

It is not a limit on what can be drawn. The window is drawn by gunim,
whose painter draws rounded rectangles, strokes, shadows and images in
pixels. Anything that is a shape rather than a character belongs there.

Reaching for cells because the thing in front of you is already a grid
is how that gets forgotten. The old app, gridterm, paid for it twice:

- The rules around a menu were box-drawing characters, so they were a
  cell thick, they broke where a font drew those characters differently,
  and a corner could only be the shapes a font had.
- The border round a shared pane was cells filled with colour, which
  made it a character wide and a character tall. It is now a ring drawn
  in pixels, in `view/glow.go`.

The rule of thumb: if you are about to ask which *character* draws
something, or how many *cells* thick it is, it is a shape and it wants
pixels.

An image is the plainest case of it. The reader draws an image file on
a layer with no grid at all, over the rows the pane gave it, shrunk to
fit and centred. Nothing about it is measured in cells.

**A blend can only land between its two ends.** The window works its own
furniture out from the theme: the menu bar, the Servers pane and a dialog are
the theme's background shaded a little towards its foreground. That is
right for a theme whose two ends are a step apart, and it cannot express
a dark ground under light furniture, which is what a DOS program looked
like.

So a theme may write its frame down instead. `themes.Frame` names the
two colours the furniture is drawn in, a single or double rule, the
buttons, and the Servers pane. `themes.Look` is that block with its colours
read. `look.Of` is the one place each furniture colour is decided: it
reads the look when the theme set one, and derives the colour from the
theme's two ends when it did not.

A stated frame also changes how things are drawn: a black shadow under
each button, the active colours on the button Enter presses, and a
double rule round a dialog when the frame asks for one. Where a colour
from the palette has to read on the frame, `look.Of` picks the one of
a few that stands out most (`standout`).

**The typeface is the user's, not the theme's.** A theme never changes
it, so switching themes leaves the font where the user put it. The
Font menu's pick is kept in the settings as `fontFamily` and taken at
the next start. A compiled-in face is taken at once. One on disk waits
for the scan of the system's fonts, since the window opens before the
scan finishes, and a kept face no longer installed is let go with a
line in the log. A typeface named with `-font` or `-font-family` wins
over the kept one for that run, and does not replace it. A font that
is there and will not read is a failure rather than a miss, so that
error reaches the user instead of being swallowed as "not found".

Two faces are compiled in: Go Mono, and the IBM VGA set that suits the
Phosphor theme. `fonts/README.md` says where the second came from and what its
licence asks of anyone shipping it.

**Paste takes whatever is on the clipboard.** Text when there is text,
and the image when there is none. A clipboard holding both is text:
that is what copying from a browser leaves, and the words are what was
meant far more often. `edit.pasteImage` asks for the other one.

**An image goes by the clipboard where there is one to reach, and by a
file where there is not.** A program reading a terminal cannot be handed
an image: the pipe carries text. But most of the programs that take a
pasted image read the clipboard of the machine they run on, so the
question is whether this window can put one there.

- A pane on this machine: the image is already on the clipboard that
  program reads, so the paste key is pressed and that is all of it.
- A pane on this machine whose shell is at its prompt: readline reads
  the paste key as quoted-insert, so the image is written to a file
  and the path typed instead (`shellWouldQuoteIt`).
- A pane on a kakel window this one is connected to: the image is sent
  over a channel of its own on the connection that is already open, put
  on that machine's clipboard, and then the paste key is pressed. Only
  once it has landed, or it would paste whatever was there before.
- A pane on a machine reached by SSH, or beyond a connected window:
  there is no clipboard over there to reach, so the image is written
  on that machine and the path typed names a file it can open.

`edit.pasteImage` asks for the other thing: the image written to a
file on whatever machine the pane is on, and the path typed. That is
what a name at a prompt wants -- `magick <paste>` -- rather than a
image for something that reads the clipboard itself. It is also what
the ordinary paste falls back to where there is no clipboard to reach.

Reading the clipboard is per-platform. `clip/image_windows.go` asks
the operating system for a device independent bitmap and turns it into
an image, and `clip/clip_linux.go` reads one through X11. Everywhere
else reports that there is no image, so the command says so rather
than failing in a way that reads like a fault.

There is no standard for this. OSC 52 is the standard for a clipboard
over a terminal and it carries text only; Sixel and the rest draw a
image rather than putting one anywhere. So this is kakel's own
channel between two kakel windows.

The file an SSH pane gets goes under the home directory of whoever the
connection logs in as, because where a temporary directory is depends on
the machine and this has only a path separator to go on.

**Idle costs nothing; moving costs a whole frame.** Marcus settled this
on 2026-09-19. There are two savings worth making and one that is not:

- Do not draw when nothing changed. That is what the skipped frame is
  for, and it is the whole of why damage tracking exists.
- Do not draw what nobody is shown.
- Do *not* make an animation coarse to save frames. Something moving on
  screen is something the user is looking at, and it should move at the
  rate the screen refreshes.

Two things move on their own: the ring round a shared pane and the mark
on a busy row of the Servers pane. Both glow on one three-second cycle
(`glowEvery`). While either is showing, the window is drawn again every
50 ms (`glowStep`); with neither, nothing asks for a frame.

Cost is the wrong worry here. Marcus runs termflix in a full-screen kakel on an
ultrawide monitor, which animates every character on it at 24fps, and
the fans stay off. A few borders and icons are not what makes a computer
warm. If they ever look expensive, that is a thing to measure and fix
rather than a reason to animate less.

One trap, which the old app had to learn twice: a mark that pulses must
not rest *on* the colour it pulses from, or a busy row at rest cannot be
told from a settled one.

**Damage tracking is load-bearing.** `term.sync` copies only the rows
the grid marks dirty into what the window draws, so a row it skips keeps
showing what it held before. A row wrongly considered clean is a visible
bug, so `grid.Set` compares before it writes and never dirties a row for
content that did not change.

**Nothing on a UI thread writes to a pty.** Writing to a pty blocks once
the program stops reading its input. Both the output pump — which holds
the terminal lock — and the UI thread produce input, so both queue
through a writer goroutine. Without that, a program that stops reading
wedges the whole window.

**Box characters are drawn, not looked up.** A font's box glyphs are cut
for that font's own advance width. Inside a terminal cell the strokes
stop short of the edges and adjacent cells do not meet, so every framed
TUI renders as a field of disconnected ticks.

**Host keys are never assumed.** A terminal that silently trusts an
unknown SSH host key can be man-in-the-middled and nobody finds out. An
unknown host gets a dialog showing its fingerprint, and only an explicit
yes records it. A key that does not match one already in `known_hosts`
is refused outright: there is no answer a user could give that would
make connecting safe. A `known_hosts` that cannot be read is an error
rather than an empty one, because a truncated list does not report a
host as unknown — it reports its key as changed.

## What ConPTY passes on

On Windows every pane on this machine runs through ConPTY, which is not
a pipe. It
reads what the program writes, keeps a console buffer, and writes that
out again. So it answers some sequences itself and passes on the ones it
has no opinion about.

Measured on 2026-09-20 on this machine, twice: once from PowerShell and
once with raw bytes through `cmd /c type`, which agreed.

| Passed on | Kept by ConPTY |
|---|---|
| XTVERSION (`CSI > q`) | DA1 (`CSI c`) |
| OSC 4, the palette question | OSC 11, the background question |
| OSC 7, where the shell is | APC, which is the kitty protocol |
| OSC 9, a message | DCS, which is sixel |
| OSC 133, the prompt marks | |
| OSC 1337 and OSC 1338, the images | |

What follows from it:

- **Everything kakel reads today is passed on.** The images and
  the prompt marks are in the left column, which is why they work.

- **DA1 is answered by ConPTY from its own model.** It asks this
  window once as it starts and keeps the answer. So adding a
  capability to kakel's own DA1 reply changes what conhost thinks
  and not what a program is told. Sixel is discovered through DA1, so
  that is the second thing blocking it.

- **XTVERSION reaches kakel.** That settles the open question: the
  CSI sequences ConPTY has no opinion about are passed on.

- **There is no passthrough flag.** microsoft/terminal#1985 asked for
  one and was closed as a duplicate. The real flags are in
  `src/inc/conpty-static.h`, and they are about glyph width.

- **A passed-on sequence can arrive out of order** against the text
  around it -- microsoft/terminal#17314 and #11220. If an image ever
  lands a line off, that is where it comes from.

## Settled, do not re-open

- **A failure is a notice, not a dialog** (2026-09-20). It was settled
  for a pane the user had closed: it failed long after they stopped
  waiting, and a dialog took their next click. The gunim window does it
  for every failure: a toast, which takes no click, and a line in the
  Window Log, where it can be read again once the toast has gone.

- **The file viewer stays a viewer, with no caret** (2026-09-20). A
  keyboard selection goes on starting at the top left of the view,
  which is the price of the arrows still scrolling. A caret would mean
  Up and Down moved it and the view followed, which is an editor, and
  the reader is a pager.

- **The words a user reads say "connect to"** (2026-09-19), mirroring
  the host's "Serve This Window…". "Work in" was wrong because
  nothing moves, "share" because a share is the set of panes handed to
  an agent, and "session" because a session is a running shell. The
  code still says "take over"; see #60.

- **A new terminal here opens on the shell kept for new terminals**
  (2026-09-19). Three things are asked in order: the shell the focused
  pane runs (`likeHere`), the shell kept in the settings file, then the
  machine's default. A shell is kept by picking "Start … in New
  Terminals" in the palette (`pickShell`); "New Terminal, Default
  Shell" opens the machine's default and forgets the pick. With nothing
  kept, `session.DefaultShell` answers `%COMSPEC%` on Windows, so a
  first launch opens cmd.exe, not PowerShell.

- **A pane whose program ended asks what next, with two buttons**
  (2026-09-19). Close is the default, so typing exit and Enter leaves
  nothing behind. `TestEnterClosesAPaneWhoseShellEnded` pins it.

- **A shell on a server says "Connection closed.", with Reconnect.** A
  shell on this machine says "The shell has finished.", with Start
  Again (`paneEnded` in `app/ended.go`). On 2026-09-17 Marcus kept one
  wording, "Connection closed.", for both; the gunim window has always
  worded them apart.

- **A command is worded differently**, and not for tidiness. Nothing is
  being connected: the command's channel closed and the SSH connection
  is up. The question names the command and the choice says it runs it
  again, because running `make deploy` twice is a thing the user has to
  see before they press it.

- **A pane that cannot be put in a job object opens no pane, and the
  window says why** (2026-09-17). Opening it anyway brings back the
  orphaned shells the job object is there to stop, in silence.
  `log.Fatal` was wrong too: started from Explorer there is no console.

- **The keyboard shortcuts file holds changes, not the whole map**
  (2026-09-17). Moving a shortcut takes two lines, and USAGE.md and
  the file's own `_help` lines say so.

- **A listing that fails while a path is being completed is not shown**
  (2026-09-17). A dialog per keystroke would be worse than the fault,
  so the completion offers nothing and the typing goes on.

- **Nothing caps the panes a window keeps** (2026-09-16). A pane worth
  keeping is worth reusing, so reusing it is the easy thing rather than
  throwing the transcript away. Closing a pane does release what it
  held.

- **What the agent sent is written down**, and Typing History on the
  Share menu shows it (2026-09-17). The user hands the pane over,
  gives the access and holds the secrets, so what the agent does there
  is theirs to read. It is what was sent, not what ran: Backspace, Tab
  completion and Up through the history all change a line first, Ctrl+U
  throws one away, a here-document reads as four commands, and in a
  full-screen program every line typed reads as a command. Its first
  line says so. A secret the user types at the agent's asking is not in it.

- **A hand-over does not expire** (2026-09-17). It lasts until the user
  takes it back or closes the pane. Revisit if forgotten hand-overs
  ever pile up.

- **A command pane can be handed over**, running or not. A running one
  takes keys on the command's stdin; an ended one can be read and not
  typed into, which is worth having for a failed build.
