# What gridterm does

The whole list. The [README](README.md) has the short version.

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
- **The SSH agent is carried only where you say.** The **SSH agent**
  field in the server dialog lets that machine reach the agent running
  here, so a jump onward from it signs with the keys held here and no
  key is copied over. It is off until you turn it on, per machine, and
  while it is on anyone who is root on that machine can sign with those
  keys for as long as the connection is up. A machine that will not
  carry the agent opens no pane, rather than opening one that quietly
  has no keys.
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

## Telling a program which terminal this is

`TERM` names a kind of terminal and every terminal borrows the same few
names, so a program reading it learns nothing about this one. gridterm
says which it is in two ways:

- **`TERM_PROGRAM` and `TERM_PROGRAM_VERSION`** in every pane it starts.
  A pane in a WSL distribution gets them too: a Windows variable does
  not cross unless `WSLENV` names it, and gridterm adds the two names to
  whatever is already carried.
- **XTVERSION**, `CSI > q`, answered with the same name and version.
  That is the way of asking that survives ssh and tmux, where an
  environment variable does not.

Both say `gridterm`, which is true and which no program has heard of
yet. "What this window calls itself…" in the command palette changes the
name to a terminal a program does know, which is how to make one show
pictures before it has heard of this one. It may then send the rest of
that terminal's sequences, and whatever gridterm does not read lands on
the screen as text. That is the trade, and it is why the honest name is
the default.

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
