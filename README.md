# gridterm

A GPU-rendered terminal emulator in Go, built for Windows first.

It runs a shell on a local pseudo-terminal — a PTY on Unix, a ConPTY on
Windows — or on another machine over SSH, feeds the output through a VT
emulator, and draws the resulting character grid as batched triangles.

24,043 lines of Go, 29,048 lines of tests, 1,117 tests.

![a shell running in gridterm](docs/shell.png)

## What works

- **A real terminal.** bash, vim and less all run: alternate screen,
  scroll regions, scrollback, 256 and true colour, bold, dim, italic,
  underline, strikethrough and reverse video, window title, cursor
  shapes, a cursor that blinks when the program asks for one, device
  reports, bracketed paste and mouse modes.
- **Local shells and SSH.** One `session.Session` interface with two
  implementations. Nothing above it — the emulator, the grid, the
  renderer — can tell the difference.
- **Connections, not just shells.** One SSH connection carries several
  things at once, so a second terminal on a machine is a second channel
  rather than a second login. A remote command gets a connection of its
  own, named by what it runs. Connect from inside the window with
  `Ctrl+Shift+N`.
- **One gridterm window working in another.** A window can serve itself
  on a port you opt into, and another window on another machine can take
  it over: its sidebar appears under that window's name, and a pane
  opened there is drawn here. Key authentication only, from a list of
  keys you write; there is no password and no way past an unknown host
  key but saying yes to its fingerprint, and a host key that changed is a
  hard failure. The window being served keeps drawing and says who is
  working in it. Closing the connection gives it its screen back.
- **Working in a shell that is already running over there.** The served
  window says what it has open, and choosing one of those rows opens a
  pane here on the program that is already running there, starting with
  the screen as it stands. Both people see it and either can type. It
  keeps running over there when this window lets go. A pane already
  watching something comes forward rather than opening a second one, and
  the row says when somebody elsewhere is reading it.
- **The files of the window taken over.** The same connection carries
  them, as SFTP on a channel of its own, so a browser pane on that
  machine costs no second login. A window that would rather not offer
  its files refuses the channel by name.
- **Panes shared with an agent.** Set the panes up -- through whatever
  machines, as whatever user, with whatever credentials -- put them in a
  share, and give a program you are talking to the one code for it. The
  code is the whole of what lets it in, and with it the agent can read
  those panes, type into them, and wait for them to settle. It reaches
  no other pane, no connection of yours and no file except through the
  panes you shared. Add a pane while it works and it is there the next
  time the agent asks what it has; take one out and it is gone at once.
  So "set this up across three machines" is one code and one prompt. It
  is a narrow way in rather than a fence around what follows: what it
  types goes into live shells running as whoever you set those panes up
  as, and they do whatever those shells do — in your panes, in front of
  you, and you can take them back. Nothing listens until you share a
  pane, the port is on the loopback address, and taking the last pane
  back makes the code useless at once.
  `gridterm -mcp` is the Model Context Protocol server the agent runs;
  it holds no credentials and reaches nothing until you give it a code.
- **One machine reached through another.** A saved server can say it is
  behind another one. The second connection is carried inside a channel
  of the first, so no local port is opened for it and nothing else on
  the machine can use it. Closing the one in the middle closes what
  rides on it.
- **A file manager with as many panes as you want.** One manager for the
  window, and a pane added to it from the plus on any machine in the
  sidebar: this machine, a server, or five of each with gridterm in the
  middle. Each pane says which machine it is on above the directory it
  is showing. Tab moves to the next pane and Shift+Tab back, Enter
  descends, Backspace goes up and Space marks, the way a two-pane browser
  has worked for thirty years. Moving files is a clipboard rather than a
  direction: F5 copies and F6 cuts, and F7 pastes into whichever pane you
  have gone to. A copy can be pasted into one pane after another; a cut
  lands once. What is waiting to be pasted is marked in the pane it came
  from, and comes from the directory it was taken in whatever that pane
  is showing by then. A bar along the bottom says which key does what,
  the way Midnight Commander does, and clicking a key on it runs that
  key. A directory is never read on the goroutine that draws, so a slow
  machine cannot stop the window, and a read that fails leaves the
  listing that worked on screen with the reason beside it.
- **A reader for a file, without a shell.** F3 opens a file from the
  browser and F4 tails one, on this machine or on a server. It works the
  way `less` does: a page at a time, "/" to search, "n" and "N" for the
  next match and the one before, ":" to go to a line, and Ctrl+H for a
  hex dump. A file being tailed is asked about three times a second and
  stays at its end as it grows; scroll back and it leaves you where you
  put yourself. Code is coloured by what the file is called, and a
  markdown file gets its headings, bullets and quotes. A picture file
  shows the picture, on a layer of its own over the pane: the grid is for
  text. Nothing is read on the goroutine that draws, and a file that will
  not read says why rather than showing an empty pane.
- **A strip beside the file**, where a code editor puts its minimap and
  doing the same job: the shape of the whole file at once, the pane's
  place in it as a box, and a click to go there. A log gets a second
  column for how bad it got, so one error in a thousand quiet lines is
  found by looking rather than by scrolling. `Ctrl+M` turns it off.
- **A log of JSON lines read as a log.** A file whose lines are JSON
  objects is laid out in columns -- the time, the level, the message,
  and the rest of the fields after it -- with the level coloured for
  what it means. It turns itself on for a file that looks like one, and
  `Ctrl+J` puts the JSON back. Every logger spells the fields
  differently, so `time`, `ts`, `@timestamp`, `level`, `severity`,
  `msg` and `message` are all read.
- **Links and file paths in the output.** Ctrl and a click follows a
  link a program declared with OSC 8, an address written out in the
  text, or a file the output named. Holding ctrl marks what is under
  the pointer and writes where it goes along the bottom row, because a
  program can put any address under any words. A file opens in the
  viewer and a directory in the browser, at the line a compiler named
  when it named one. A path is checked against the disk before it
  counts as a link, so a run of characters naming nothing is just
  text. It works on a server too: the machine at the far end is asked
  over the connection the window already has, and what it says is kept,
  so a path lights up a moment after the pointer reaches it. A relative
  name needs the shell to say where it is, which gridterm sets up
  itself; see **Shell integration** below.
- **The files inside WSL.** Every distribution installed is a line on
  the plus for this machine, and the browser reads it like any other
  directory: Windows serves them on a share, so nothing of gridterm's
  own is needed. A file dropped on a WSL pane lands in the directory
  that shell is in, on the same share.
- **A picture a program put in its output.** OSC 1337, the sequence
  iTerm2 made and the terminals after it copied. The pane holds the
  picture on the line it landed on and it scrolls with the text, on a
  layer of its own. It travels to a window watching the pane: a screen
  is sent as the escape sequences that draw it, so the pictures go the
  same way. Only an inline picture is taken -- the same sequence asks a
  terminal to save a file, which a pane should not be able to make this
  window do.
- **Files dragged into a pane.** They land in the directory the shell
  said it was in, and nothing is typed: the file is already where the
  program is looking. The window says when it has arrived. A pane on a
  server has the file copied there first, with a row saying how far it
  has got. A shell that has not said where it is leaves nowhere to put
  the file, and then the path is typed instead.
- **File work in the background.** Copying, moving and deleting, on one
  machine or between two, with how far along it is and a way to stop it.
  A name that is already there is asked about — replace, skip, rename, or
  stop — and never decided alone. A file is written beside its name and
  moved onto it at the end, so what is at that name is either the file
  that was there or the whole of the new one, never half of either. Every
  failure stops the job and says why: half a directory that says it
  worked is worse than one that stopped.
- **Tunnels you can find again and look inside.** Clicking a tunnel's
  row opens a pane for it: what it has been doing, a way to watch what
  goes through it, and the button that closes it. Watching is off
  until asked for, because a tunnel carries whatever it carries. A
  tunnel can be kept the way a command can, and each one kept is a
  line on the palette.
- **Tunnels.** A port here that stands for a service over there, a port
  over there that stands for one here, or a SOCKS5 proxy that reaches
  whatever it is asked for as the far machine sees it. A tunnel with no
  address of its own listens on that machine only, and one that would
  let the rest of the network through asks before it opens — as does
  every remote forward, because where the far machine really binds it is
  the far machine's decision. The panel shows what each is carrying: how
  many streams, how fast, and how many failed.
- **A sidebar instead of a row of tabs.** It is open when the window
  opens, and it is how everything is reached: every terminal, file pane,
  tunnel and transfer, under the machine it is on with this one at the
  top. Every saved server is on it whether or not anything is connected,
  and every machine carries a plus that drops a menu of what can be
  opened there. A row's hand-drawn kind icon is coloured for what it is
  doing — green for open, brightening and dimming while bytes are going
  past, grey once it has finished — and a machine's own heading carries a
  dot in the same colours, as does a row in a sidebar dragged too narrow
  to draw an icon. The bar follows whatever pane is in front, so the
  sidebar is the list of what is open and says which one you are looking
  at. "Connect to server…" is pinned under the list. Nothing polls and
  nothing ticks: the row is worked out afresh each frame from when the
  last byte went by, so an idle sidebar redraws nothing at all.
  `Ctrl+Shift+B` hides it and shows it again.
- **Servers are saved.** A machine you add gets a line on the Servers
  menu and an entry in the palette, kept in a JSON file under the OS
  configuration directory. It holds no secret and never will. A list
  that cannot be read is reported and is never written over, because a
  file nobody could parse is still somebody's list of servers.
- **Secrets are asked for in the window.** A key passphrase, an account
  password and a one-time code all get a dialog. An unlocked key is kept
  in memory for as long as the window is open and never written
  anywhere, so the second connection to a machine asks nothing.
- **Unknown host keys are shown, not assumed.** A host that is not in
  `known_hosts` gets a dialog with its fingerprint, and only an explicit
  yes records it. A key that does not match one already recorded is
  refused with no button to press.
- **Batched rendering.** A full screen of text is one `DrawTriangles`
  call for the backgrounds plus one per atlas page for the glyphs,
  typically two in total however much text is on screen.
- **Damage tracking.** Writing a cell that already holds the same
  content does not dirty its row, so an idle screen draws nothing at all.
  Two things dirty a row on a clock instead of on a change: the sidebar
  pulses the active connection's row, and a blinking cursor dirties the
  row it sits on twice a second. A steady cursor dirties nothing.
- **Wide characters and combining marks.** CJK and emoji take two
  columns; a base character and its marks share one cell.
- **Box drawing that joins up.** The box and block characters are drawn
  in code at the exact cell size, so framed TUIs have unbroken lines.
- **A real key pipeline.** Press, release and OS repeat with modifiers,
  correlated with the text they produced, encoded to the bytes a program
  expects — including application cursor mode, which vim and readline
  need.
- **Mouse, selection and clipboard.** Programs that ask for the mouse
  get it; hold Shift to select text anyway. Drag to select, Alt+drag for
  a rectangle.

![selecting text with the mouse](docs/selection.png)

![vim running on the alternate screen](docs/vim.png)

## Try it

```
git clone https://github.com/marrasen/gridterm
cd gridterm
go run .                       # your login shell
go run . -ssh user@host        # a shell on another machine
go run . -e 'vim /etc/hosts'   # one command
go run . -font-size 18
go run . -font /path/to/Regular.ttf,/path/to/Bold.ttf
```

With `-ssh` the window opens first and connects in a pane, so it asks
about an unknown host key in a dialog and keeps the account of how the
machine was reached. A new pane or split opens on that machine too, and
its row on the sidebar offers the rest: files, a command, a tunnel and
the account.

Text is drawn in the four Go Mono faces compiled into the binary:
regular, bold, italic and bold italic. `-font` takes font files instead,
comma separated, in the order regular, bold, italic, bold italic. Only
the regular font is required — a style you leave out borrows one you
gave. There is no way to pick a font by family name yet; give paths.

gridterm comes with:

| Key | |
|---|---|
| `Shift+PageUp` / `Shift+PageDown` | scroll the scrollback |
| mouse wheel | scroll, or arrow keys on the alternate screen |
| drag | select; `Alt+drag` selects a rectangle |
| `Shift+drag` | select even while a program owns the mouse |
| `Ctrl+Shift+C` / `Ctrl+Shift+V` | copy and paste |
| middle click | paste |
| `Ctrl+=` / `Ctrl+-` / `Ctrl+0` | font size |
| `Ctrl+Shift+B` | show or hide the sidebar |
| `Ctrl+Shift+L` | go to the sidebar |
| `Ctrl+Shift+N` | connect to a server |
| `Ctrl+Shift+A` | show every pane at once |

"Keys and commands" on the Help menu lists every command, the key that
runs it, and the name the shortcuts file calls it by.

To change a shortcut, take "Write a starting keyboard shortcuts file" on
the same menu. It writes `keys.json` holding every shortcut you have
now, and gridterm reads the file the next time it starts.

The file says what to change, not what the whole window does:

- Add a line to put a command on another chord. To move it, set the old
  chord to `"nothing"` as well, or the command runs on both.
- Delete a line and that chord goes back to what gridterm comes with.
- Shortcuts added to a later gridterm arrive on their own. A built-in
  chord that a later gridterm moves does not, because your file still
  names the old one.

Every chord in the file runs before the pane sees it, so a chord a
program in the pane needs stops reaching it. A chord you would type,
such as a plain letter, is refused for that reason: hold `Ctrl`, `Alt`
or `Super`, or use a function key.

The file browser's own keys are not in the file.

"Show every pane" on the Go menu, or `Ctrl+Shift+A`, draws every pane at
once on a grid, each one live and shrunk to fit. The arrows walk them,
`Enter` goes to the one marked and `Escape` leaves you where you were. A
click goes straight there. The pictures are shrunk by the GPU rather
than cell by cell, and a window with nothing happening in it still skips
the frames it would have skipped anyway.

Sharing a pane with an agent is on the Servers menu and on the plus on
this machine's row. The first pane starts the share; after that the same
line reads "Add this pane to the share". Each pane gets a dialog of its
own with four tick boxes on it, saying what the agent may do there
beyond reading the pane and typing into it: restart a closed connection,
open another pane on the same machine, read only, and read above a
clear. Every box starts off, what you tick is remembered, and turning
one over takes effect on the agent's next call. An agent that hits a
password prompt can ask you to type it into the pane: a line appears
saying what it wants, what you type goes to the program, and the agent
is told you typed something and never what.

gridterm writes down what an agent types, and "What the agent typed" on
the Servers menu shows it for the pane you are on. You hand the pane
over, you give the access and you hold the secrets, so what the agent
does in there is yours to read. It is what the agent sent, not what the
shell ran: a line it edited before pressing Enter is there as it was
typed. A secret you type at the agent's asking is not in it, because
you typed that yourself. The record lives as long as the pane: it is
capped, it says how many of the oldest lines it has dropped, and it goes
when the pane closes.

"Show the share…" on the same menu, or the "Sharing with an agent" chip
on the menu bar, opens the share itself: the one code, a row per pane
that takes it out and puts it back, and what to give the agent. It asks
which agent it is for -- Claude Code, Codex, Cursor, or another host
that takes a JSON MCP config -- and remembers the answer for next time.
"Copy the prompt" puts a prompt on the clipboard and does nothing else:
paste the whole of it to the agent, and it carries the code and says how
that host adds this window's `gridterm -mcp` server. What the tools do
and what the rules are come from the server's own instructions once the
agent connects, so the prompt does not repeat them. "Instructions" opens
those setup lines on their own, with a button and the copy chord that
take the command line -- or the JSON, for a host set up by a file -- off
the dialog. "Write the skill" saves a `SKILL.md` where that host reads
skills from, and says where it went; `gridterm -mcp-skill` prints the
same file. "Stop sharing" ends the share and the code stops working, and
so does taking the last pane out.

Serving this window and connecting to another are on the menu rather
than on a key: "Serve this window…" asks for the port and says the
fingerprint to check, and "Connect to another window…" asks for the
address and the key to offer. Connecting opens nothing over there: what
that window has open lands on the sidebar under its name, and the plus
on that heading opens a pane on it. Nothing listens until you ask it to,
and the keys allowed in are the ones you list in an `authorized_keys`
file in gridterm's own directory, not the one in `~/.ssh`.

gridterm keeps its files where the operating system puts a program's.
Make a directory called `gridterm-files` beside `gridterm.exe` and it
keeps them there instead, so one machine can hold several copies with
files of their own. "Where gridterm keeps its files" on the Help menu
names every file and says how to move them.

In the file manager: `Tab` and `Shift+Tab` move between panes, `Enter`
descends, `Backspace` goes up, `Space` marks, `F2` renames, `F5` copies,
`F6` cuts, `F7` pastes, `F8` deletes, `F9` makes a directory and `F10`
closes the pane. The bar along the bottom says the same thing, and
clicking a key on it runs that key.

In a dialog, a field that usually holds one of a few answers offers them:
`Ctrl+Down` and `Ctrl+Up` step through what is saved.

On Windows there is nothing else to install — no C toolchain, no cgo:

```
go build -o gridterm.exe .
```

Cross-compiling to Windows from anywhere else works the same way:

```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o gridterm.exe .
```

Building for Linux needs the X11 development headers ebitengine's
bundled GLFW compiles against:

```
sudo apt-get install -y libxcursor-dev libxinerama-dev libxi-dev \
    libxxf86vm-dev libxrandr-dev libgl1-mesa-dev
```

Most of the code needs neither. `make test` runs everything that does
not touch a GPU, which is the grid, the emulator, the key and mouse
encoders and both session types.

## Shell integration

A shell is a separate program, and gridterm only sees the bytes it
prints. So it cannot know which directory the shell is in, or where one
command's output ends and the next begins, unless the shell says so. The
shell says so by printing escape sequences nobody sees: OSC 7 or OSC 9;9
for the directory, OSC 133 around each command.

gridterm sets this up itself. As a shell starts it types one line in,
the way you would type it, and then clears the pane. There is nothing to
install and no profile to edit.

- **On this machine it is on**, and `Shell setup on this machine, on or
  off` in the command palette turns it off. It is invisible: gridterm
  builds the line for whichever shell the pane runs.
- **On a server it is off**, and the **Shell setup** field in the server
  dialog turns it on. It is off because the line goes into whatever
  login shell that account has. bash and zsh understand it; fish, a
  device CLI or a menu would answer with an error.
- **Another gridterm is never set up from here.** The window over there
  starts the shell and applies its own answer.

A program can say things of its own through the same channel. A
message (OSC 9) goes on the pane's row in the sidebar, into the
window's log, and up as a Windows notification, so one that arrives
while you are looking elsewhere is still seen. How far along it is
(OSC 9;4) goes on the row.

Not a dialog: a dialog takes the keyboard, and anything that can write
to a pane could send one of these one after another. For the same
reason there is one pop-up every two seconds at most, and the ones
left out are still on the row and in the log. The notification is a
balloon in the notification area, which Windows 10 and 11 turn into a
toast and a line in the action centre. The icon appears the first time
a program asks for one and goes when the window closes.

A program may also ask what colour something is drawn in: the text
(OSC 10), the background (OSC 11) or one of the 256 palette entries
(OSC 4). All are answered, which is how a program works out whether it
is on a dark theme and picks a colour that will show against it.
Setting a colour is not: the colours are the window's theme, and a
pane left unlike every other one would have nothing to put it back.

What each shell is told:

| Shell | Directory | Command marks |
|---|---|---|
| PowerShell, pwsh | OSC 7, wrapping the prompt already there | yes, with PSReadLine |
| bash, zsh, WSL, a server's login shell | OSC 7 | yes |
| Command Prompt | OSC 9;9 | no |

The Command Prompt builds its prompt out of what `cmd.exe` substitutes,
and none of those pieces makes a URL, so it sends the plain path that
Windows Terminal uses. It has no hook for a command starting or ending.

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

## Design notes worth knowing

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

## The ebiten fork

`go.mod` replaces ebitengine with
[marrasen/ebiten](https://github.com/marrasen/ebiten), a fork of
[unstablebuild/ebiten](https://github.com/unstablebuild/ebiten). `go
build` fetches it like any other dependency; there is nothing to check
out by hand.

Upstream ebitengine gives you polled `IsKeyPressed` plus
`AppendInputChars`, which cannot tell Ctrl+C from the letter c, nor an
OS key repeat from a fresh press. That rules out writing a terminal
against it. unstablebuild's fork adds `AppendInputEvents`, where each
observation is either a key transition or a committed code point, tied
together by an `InputSource` id — and it implements this properly on
Windows, in the Win32 message loop.

That fork does not itself compile for `GOOS=windows`: `initializeGLFW`
calls `glfw.InitHint`, which only the cgo glfw binding defines, so the
pure-Go Windows port fails to build. This fork is the same commit with
that one macOS-only call put behind a build tag — the branch is
`windows-build`, offered upstream. Once it lands there, the replace can
point back at unstablebuild's own tag.

ebitengine and both forks are Apache-2.0. Nothing here is derived from
the Rune IDE, which is GPL-3.0-or-later and keeps its renderer and
emulator under `internal/` where they cannot be imported.

## Known gaps

- **Fonts are chosen by file path, not by name.** `-font` takes paths.
  Matching a family name means reading the name table out of every font
  file on the system, grouping the four styles despite inconsistent
  subfamily strings, and rejecting proportional fonts; none of that is
  written yet.
- **A fallback glyph is always upright.** The system fonts consulted for
  runes the main font lacks are shared by every style, so CJK, braille
  and heavy box drawing stay regular even in bold or italic text.
- **Variable fonts render at their default instance.**
  `x/image/font/sfnt` does not apply variation axes, so asking such a
  font for its bold weight gets the default one.
- **Blink** is parsed and ignored.
- **Colour emoji** do not render. `x/image/font/sfnt` cannot read the
  bitmap tables that colour emoji fonts use.
- **Emoji ZWJ sequences and flags** show only their first glyph; the
  rest of the cluster is dropped rather than stacked in one cell.
- **OSC 52 clipboard writes** are parsed but not yet applied. Reads are
  deliberately never answered — replying would let any program that can
  write to the terminal exfiltrate the clipboard.
- **A click faster than one frame is missed.** ebiten reports the mouse
  as polled state, so a press and release inside the same 16 ms are
  never seen as either. No human manages it; a test harness does.
- **File panes share the width evenly, and the split cannot be dragged.**
  Five panes in an eighty-column window are sixteen columns each. Closing
  one gives its width back to the rest, but there is no way to make one
  pane wider than another.
- **A watched pane is not resized to suit the watcher.** The screen is
  drawn on the machine it is running on as well, and shrinking somebody
  else's shell to fit a pane they are not looking at would reach further
  than watching was asked to. So the size travels instead and the row
  says what it is; a screen wider than the pane showing it wraps.
- **Two gridterm windows have to be the same build.** What one window
  says to another uses SSH's own encoding, which is positional: there is
  no room for a field one end knows and the other does not. A window of
  another build is refused by name rather than half understood.
- **The files of a machine the served window reached** are not offered.
  A window serves the files of the machine it is running on. Something
  it reached over SSH of its own is another hop, and nothing proxies it
  yet.
- **An agent is handed a screen, not a session.** It reads what is on
  the pane and types into it, the way a person looking over your
  shoulder would. A shell with shell integration on tells it when a
  command finished, what it exited with, and where that command's output
  began, so it can read the output on its own. A shell without it leaves
  it watching for the prompt to come back, which is a guess. Clearing the
  screen stops the agent reading what was above it; the screen it took
  away goes into the history, so you can still scroll up to all of it.
- **The port an agent reaches is the machine's, not the session's.** A
  loopback port on Windows is reachable by every session on the machine,
  not only by the one that opened it. Nothing gets past it without the
  code, which is not guessable and which you give out yourself, and
  anything that does not say what it is at once is hung up on. But it is
  a port, and it is open while a share has a pane in it.
- **Sixel and the Kitty graphics protocol** are not implemented. OSC
  1337 is the one this reads.
- **Only four megabytes of picture travel with a screen.** A pane may
  hold sixty-four pictures of sixteen megabytes each, and a whole screen
  is sent every time a window starts watching. The pictures past the
  budget are left out, and the watcher sees the text with a gap. A
  picture sent while somebody is already watching is not affected: the
  sequence carrying it is part of what the program said.
- **A path on a server is found one round trip late.** The machine is
  asked when the pointer first reaches the text, and the answer is what
  underlines it. Hold still for a moment and it lights up.
- **An OSC payload other than a picture is capped at a kilobyte.** The
  parser keeps that much and throws the rest away. The two sequences
  that carry a picture are read before it sees them, so they are whole;
  a clipboard write longer than a kilobyte is cut short.
- **An APC, PM or SOS string with no terminator grows without bound.**
  The parser buffers it before the emulator sees anything, so it cannot
  be capped from here; it needs a fix in `danielgatis/go-vte`, which
  already caps OSC the same way.
- `-e` splits its argument on spaces, with no quoting.

## Licence

MIT; see [LICENSE](LICENSE). The dependencies are all permissive:
ebitengine and `golang.org/x/*` are Apache-2.0 or BSD, `pkg/sftp` and
`kr/fs` are BSD, and `go-vte`, `go-pty`, `uniseg` and `atotto/clipboard`
are MIT.

## How this was built

Each step was reviewed adversarially before the next one started, which
is where most of the interesting bugs came from — a crash on a
one-column screen, two denial-of-service paths, a deadlock between the
output pump and a device report, and a reaper that threw away a short
command's entire output. The commit messages record what each review
found.
