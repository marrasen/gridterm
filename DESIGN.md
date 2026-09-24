# Design notes worth knowing

Why the code is the shape it is. Read this before changing how drawing,
input or the clipboard work.

## Layout

| Package | Lines | Needs a GPU? | What it is |
|---|---|---|---|
| `vt` | 2,016 | no | the VT emulator: parser, screen model, two buffers, scrollback |
| `grid` | 1,109 | no | the display grid, damage tracking, selection, wide-character invariants |
| `input` | 592 | no | key, text, mouse and paste events to VT bytes |
| `session` | 362 | no | a shell as a byte stream, and the local pty |
| `remote` | 3,693 | no | SSH: connections, shells, host keys, unlocked keys, tunnels |
| `serve` | 1,934 | no | one window served to another: the listener, the client, and what they say |
| `agent` | 683 | no | panes shared with an agent: the code, the port, and what may be asked |
| `mcp` | 638 | no | those panes over the Model Context Protocol, on standard input and output |
| `conns` | 252 | no | what the window has open, grouped by machine |
| `vfs` | 669 | no | a filesystem a file pane works on: this machine, or one over SFTP |
| `jobs` | 1,142 | no | copying, moving and deleting in the background, with progress and cancel |
| `meter` | 327 | no | bytes moved, and how long ago: the four states |
| `ui` | 5,919 | no | the widget toolkit: panes, decks, menus, dialogs, fields, lists |
| `ui/term` | 787 | no | a shell on a widget |
| `ui/files` | 1,455 | no | the file manager: any number of panes side by side |
| `glyph` | 1,301 | yes | glyph atlas, system font fallback, box drawing |
| `render` | 1,779 | yes | grid to batched triangles |
| `main` | 8,499 | yes | the window and the wiring |

The layering is deliberate: `vt` never imports the renderer, `input`
never imports ebiten (that lives in `input/ebitenin`), `ui` knows nothing
about terminals or SSH, `serve` carries bytes without knowing what rides
on them, and `session` knows nothing about any of them. Everything
fiddly is testable without a display, which is how the emulator got
written.

## The notes

**The grid is for text. Nothing else has to be cells.** A terminal is a
grid of characters, so the grid is what the emulator writes into and
what the renderer rasterises. That is where the name comes from and it
is right for text.

It is not a limit on what can be drawn. A layer is pixels. The renderer
draws quads, and a layer can carry a shader pass of its own: the frosted
panel behind a dialog is a signed distance field with a rounded corner
and a lit rim, computed per pixel, and it knows nothing about cells.
Anything that is a shape rather than a character belongs there.

Reaching for cells because the thing in front of you is already a grid
is how that gets forgotten. Two places have paid for it:

- The rules around a menu are box-drawing characters, so they are a cell
  thick, they break where a font draws those characters differently, and
  a corner can only be the shapes a font has. `glyph.Arms` and the
  stretching in the renderer exist to paper over that.
- The border round a shared pane was cells filled with colour, which
  made it a character wide and a character tall.

The rule of thumb: if you are about to ask which *character* draws
something, or how many *cells* thick it is, it is a shape and it wants
pixels.

A picture is the plainest case of it. The reader draws a picture file on
a layer with no grid at all, over the rows the pane gave it, shrunk to
fit and centred. Nothing about it is measured in cells.

**A blend can only land between its two ends.** The window works its own
furniture out from the theme: the menu bar, the sidebar and a dialog are
the theme's background shaded a little towards its foreground. That is
right for a theme whose two ends are a step apart, and it cannot express
a dark ground under light furniture, which is what a DOS program looked
like.

So a theme may write its frame down instead. `themes.Frame` names the
two colours the furniture is drawn in, a single or double rule, and the
buttons. `themes.Look` is that block with its colours read, and the
helpers in `look.go` are the one place each furniture colour is decided:
each reads the look when the theme set one and derives exactly what it
derived before when the theme did not.

Two things follow from a stated frame. Dialogs go flat -- an opaque box,
square corners, no frosted glass -- because glass behind an opaque box
is paid for and never seen. And a colour the window takes from the
numbered sixteen is now drawn on a second ground, so `app.onFrame` moves
it towards the frame's own text until it can be read there, which keeps
what it can of the hue.

**A theme may name a typeface, and the name is a wish.** `Theme.Font` is
a family name. A window takes it when it has that font, compiled in or
installed, and keeps the one it is already drawn in when it does not.
A window opens on its theme before the scan of the system's fonts has
finished, so `useWantedFont` runs again when the scan lands.

Two things are not wishes. A typeface named with `-font` or
`-font-family` is an instruction, and a theme does not overrule it. And
a font that is there and will not read is a failure rather than a miss,
so that error reaches the user instead of being swallowed as "not
found".

Two faces are compiled in: Go Mono, and the IBM VGA set the Turbo theme
asks for. `fonts/README.md` says where the second came from and what its
licence asks of anyone shipping it.

**A style the family has no face for is faked, not borrowed.** A family
may ship one face or four. `buildFaces` records what each style has to
fake in `Atlas.faked`, and the rasteriser applies it to the mask after
the glyph is drawn: bold is a smear a pixel to the right, italic is a
shear about the baseline. Before this, a one-face family drew bold and
italic as the regular glyph again, so `ESC[1m` printed nothing different.

A fallback face stands in for a *rune* the family cannot draw rather
than for a style, so what it draws is left alone.

**Paste takes whatever is on the clipboard.** Text when there is text,
and the picture when there is none. A clipboard holding both is text:
that is what copying from a browser leaves, and the words are what was
meant far more often. `edit.pasteImage` asks for the other one.

**A picture goes by the clipboard where there is one to reach, and by a
file where there is not.** A program reading a terminal cannot be handed
an image: the pipe carries text. But most of the programs that take a
pasted picture read the clipboard of the machine they run on, so the
question is whether this window can put one there.

- A pane on this machine: the picture is already on the clipboard that
  program reads, so the paste key is pressed and that is all of it.
- A pane on a gridterm this window has taken over: the picture is sent
  over a channel of its own on the connection that is already open, put
  on that machine's clipboard, and then the paste key is pressed. Only
  once it has landed, or it would paste whatever was there before.
- A pane on a machine reached by SSH: there is no clipboard over there
  to reach, so the picture is written on that machine and the path typed
  names a file it can open.

`edit.pasteImage` asks for the other thing: the picture written to a
file on whatever machine the pane is on, and the path typed. That is
what a name at a prompt wants -- `magick <paste>` -- rather than a
picture for something that reads the clipboard itself. It is also what
the ordinary paste falls back to where there is no clipboard to reach.

Reading the clipboard is per-platform. `clipboard_image_windows.go` asks
the operating system for a device independent bitmap and turns it into
an image; everywhere else reports that there is no picture, so the
command says so rather than failing in a way that reads like a fault.

There is no standard for this. OSC 52 is the standard for a clipboard
over a terminal and it carries text only; Sixel and the rest draw a
picture rather than putting one anywhere. So this is gridterm's own
channel between two gridterms.

The file an SSH pane gets goes under the home directory of whoever the
connection logs in as, because where a temporary directory is depends on
the machine and this has only a path separator to go on.

**The sidebar is built again every frame, and costs nothing to build.**
Marcus asked for it to be built only when something changes. Most of
what a row says changes on its own -- a rate, how far a job has got, how
long ago something settled, the glow on a shared pane -- so a test for
"has anything changed" would have to work out nearly everything the
rebuild works out. What was worth taking away was the garbage, not the
work: `refreshPanel` builds into slices and maps the last frame used,
`Registry.Each` walks the list without building a group and a slice of
rows per machine, and `TestBuildingTheSidebarAsksTheHeapForNothing`
holds it at nothing.

Redrawing was never the problem. The list draws through a `ui.buffer`,
so a rebuild that comes out the same dirties no row and the frame is
skipped anyway.

**Idle costs nothing; moving costs a whole frame.** Marcus settled this
on 2026-09-19. There are two savings worth making and one that is not:

- Do not draw when nothing changed. That is what the skipped frame is
  for, and it is the whole of why damage tracking exists.
- Do not draw what nobody is shown. A pane behind the switcher is drawn
  into the window's grid that nothing blits.
- Do *not* make an animation coarse to save frames. Something moving on
  screen is something the user is looking at, and it should move at the
  rate the screen refreshes.

The way to have both is to animate in bursts. The border round a shared
pane and the mark on a busy row each pulse for 250ms once a second:
every frame while a pulse is running, and perfectly still for the other
750ms, which the compositor reads as nothing to do. Smooth where it is
looked at, and three frames in four skipped anyway.

Both of those used to be stepped instead -- 200ms and 250ms a step --
and both said in their own comments that it was to cost nothing. It is
the wrong worry. Marcus runs termflix in a full-screen gridterm on an
ultrawide monitor, which animates every character on it at 24fps, and
the fans stay off. A few borders and icons are not what makes a computer
warm. If they ever look expensive, that is a thing to measure and fix
rather than a reason to animate less.

One trap, which the old stepped pulse had a comment about and the fade
had to learn again: a mark that pulses must not rest *on* the colour it
pulses from, or a busy row at rest cannot be told from a settled one.
`pulseRest` is what keeps it off the end.

**Damage tracking is load-bearing.** `ebiten.SetScreenClearedEveryFrame(false)`
means a row the renderer skips shows the *previous* frame, not a blank.
A row wrongly considered clean is a visible bug, so `grid.Set` compares
before it writes and never dirties a row for content that did not
change.

Two things dirty a row on a clock rather than on a change, and both are
meant to. The sidebar pulses the active connection's row every 200 ms,
which is `panel.go`'s `pulse`. A blinking cursor dirties the one row it
sits on each time its phase turns over, twice a second, which is
`render.Layer.stepCursorBlink`. A steady cursor dirties nothing at all,
so a window showing one is idle between keystrokes.

**Nothing on a UI thread writes to a pty.** Writing to a pty blocks once
the program stops reading its input. Both the output pump — which holds
the terminal lock — and the ebiten thread produce input, so both queue
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

Every pane on this machine runs through ConPTY, which is not a pipe. It
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
| OSC 1337 and OSC 1338, the pictures | |

What follows from it:

- **Everything gridterm reads today is passed on.** The pictures and
  the prompt marks are in the left column, which is why they work.

- **DA1 is answered by ConPTY from its own model.** It asks this
  window once as it starts and keeps the answer. So adding a
  capability to gridterm's own DA1 reply changes what conhost thinks
  and not what a program is told. Sixel is discovered through DA1, so
  that is the second thing blocking it.

- **XTVERSION reaches gridterm.** That settles the open question: the
  CSI sequences ConPTY has no opinion about are passed on.

- **There is no passthrough flag.** microsoft/terminal#1985 asked for
  one and was closed as a duplicate. The real flags are in
  `src/inc/conpty-static.h`, and they are about glyph width.

- **A passed-on sequence can arrive out of order** against the text
  around it -- microsoft/terminal#17314 and #11220. If a picture ever
  lands a line off, that is where it comes from.

## Settled, do not re-open

- **A failure belonging to a pane the user has closed goes to the log,
  not to a dialog** (2026-09-20). It failed long after the user stopped
  waiting, and the dialog took their next click. `reportForPane` shows
  it only while the pane is open. The error is not dropped: "Show what
  the window has logged" is where it goes.

- **The file viewer stays a viewer, with no caret** (2026-09-20). A
  keyboard selection goes on starting at the top left of the view,
  which is the price of the arrows still scrolling. A caret would mean
  Up and Down moved it and the view followed, which is an editor, and
  the reader is a pager.

- **The words a user reads say "connect to"** (2026-09-19), mirroring
  the host's "Serve this window…". "Work in" was wrong because
  nothing moves, "share" because a share is the set of panes handed to
  an agent, and "session" because a session is a running shell. The
  code still says "take over"; see #60.

- **A new pane opens on the shell that was picked last** (2026-09-19).
  Three things are asked in order: `-e` on the command line, the shell
  in the settings file, then the machine's default. `rememberShell`
  writes the file when a shell is opened by name. "Default shell" opens
  on the machine's default and forgets the pick. With nothing written
  down, `session.DefaultShell` answers `%COMSPEC%` on Windows, so a
  first launch opens cmd.exe, not PowerShell.

- **"Connection closed." already has two buttons** (2026-09-19,
  against `d79f629`). Reconnect and Close, with Close the default, as
  `TestEnterClosesThePaneRatherThanStartingItAgain` pins.

- **Every shell gets "Connection closed. Reconnect?"**, whether the
  transport went or the user typed exit, and whether the shell is on a
  machine or this one. That is what ssh prints, and gridterm calls
  every pane a connection. Settled twice on 2026-09-17: a reviewer
  argued a local shell was never connected, and Marcus kept the one
  wording.

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
  (2026-09-17). Moving a shortcut takes two lines, and the notice and
  the README say so.

- **A listing that fails while a path is being completed is not shown**
  (2026-09-17). A dialog per keystroke would be worse than the fault,
  so the failure goes to the log and the completion offers nothing.

- **Nothing caps the panes a window keeps** (2026-09-16). A pane worth
  keeping is worth reusing, so reusing it is the easy thing rather than
  throwing the transcript away. Closing a pane does release what it
  held.

- **What the agent sent is written down**, and is on the Servers menu
  as "What the agent typed" (2026-09-17). The user hands the pane over,
  gives the access and holds the secrets, so what the agent does there
  is theirs to read. It is what was sent, not what ran: Backspace, Tab
  completion and Up through the history all change a line first, Ctrl+U
  throws one away, a here-document reads as four commands, and in a
  full-screen program every line typed reads as a command. The dialog
  says so. A secret the user types at the agent's asking is not in it.

- **A hand-over does not expire** (2026-09-17). It lasts until the user
  takes it back or closes the pane. Revisit if forgotten hand-overs
  ever pile up.

- **A command pane can be handed over**, running or not. A running one
  takes keys on the command's stdin; an ended one can be read and not
  typed into, which is worth having for a failed build.
