# Using kakel

Keys, the shortcuts file, sharing panes with an agent, and where things
are kept. [FEATURES.md](FEATURES.md) says what each of these is for.

## Keys

Kakel comes with:

| Key | |
|---|---|
| `Shift+PageUp` / `Shift+PageDown` | scroll the scrollback |
| mouse wheel | scroll, or arrow keys on the alternate screen |
| drag | select; `Alt+drag` selects a rectangle |
| `Shift+drag` | select even while a program owns the mouse |
| `Ctrl+Shift+C` / `Ctrl+Shift+V` | copy and paste |
| middle click | paste |
| `Ctrl+=` / `Ctrl+-` / `Ctrl+0` | font size, from 8 to 96 pixels |
| `Ctrl+Shift+B` | open or close the Servers pane |
| `Ctrl+Shift+L` | go to the Servers pane |
| `Ctrl+Shift+N` | connect to a server |
| `Shift+Win+K` | the launcher, from any program, on Windows |
| `Ctrl+Shift+A` | show every pane at once |
| `Ctrl+PageDown` / `Ctrl+PageUp` | the next / previous tab |
| `Ctrl+Shift+PageDown` / `Ctrl+Shift+PageUp` | move the tab right / left |
| `F11` | fill the screen with the panes |

These are the ones you need most. "Shortcuts and Commands" on the Help
menu, or `Ctrl+Shift+H`, lists every command, the key that runs it, and
the name the shortcuts file calls it by, then the keys of the file pane
and the reader.

## Changing a shortcut

To change a shortcut, choose Options → Start a File → Shortcuts. It
writes `keys.json` holding every shortcut you have now. Edit it, then
choose Options → Read Again → Shortcuts.

The file says what to change, not what the whole window does:

- Add a line to put a command on another chord. To move it, set the old
  chord to `"nothing"` as well, or the command runs on both.
- Delete a line and that chord goes back to what kakel comes with.
- Shortcuts added to a later kakel arrive on their own. A built-in
  chord that a later kakel moves does not, because your file still
  names the old one.

Every chord in the file runs before the pane sees it, so a chord a
program in the pane needs stops reaching it. A chord you would type,
such as a plain letter, is refused for that reason: hold `Ctrl`, `Alt`
or `Super`, or use a function key.

The file browser's own keys are not in the file.

## Every pane at once

Pane → Go To → All Panes…, or `Ctrl+Shift+A`, draws every pane at
once on a grid, each one live and shrunk to fit. The arrows walk them,
`Enter` goes to the one marked and `Escape` leaves you where you were. A
click goes straight there. `Ctrl+Shift+A` again closes it. The images are shrunk by the GPU rather
than cell by cell, and a window with nothing happening in it still skips
the frames it would have skipped anyway.

## More than one window

In All Panes, drag a pane's tile out to put it in another window. Let it go over
another kakel window and it moves there; that window lights up while
the pane is over it. Let it go outside every window and it opens a
window of its own, where you let it go. A window's only pane stays
where it is.

Each window has its own panes and its own pane in front. The Servers
pane lists the panes of every window, and clicking one in another
window brings that window to the front.
The pin in the title bar, before minimize, keeps a window above other
programs' windows; View › Always on Top does the same. Questions and
notices show in the window you last worked in. A
window whose last pane moves away closes. Its close button asks before
closing the panes still in it, and the last window's asks as Exit does.

## Sharing a pane with an agent

Click into a terminal and choose Share → Share Panes…. The first time,
that starts a share with that pane. After that it opens the Agent Share
dialog: the one code, a tick box for each terminal pane that puts it in
the share or takes it out, and which agent the prompt is for -- Claude
Code, Codex, Cursor, or another host that takes a JSON MCP config. The
agent picked is remembered for next time. The "Agent Share" chip on the
menu bar opens the same dialog.

The dialog's buttons:

- **Copy Prompt** puts a prompt on the clipboard and does nothing else.
  Paste the whole of it to the agent. It carries the code and says how
  that host adds kakel's `kakel -mcp` server. What the tools do and
  what the rules are come from the server's own instructions once the
  agent connects, so the prompt does not repeat them.
- **Copy Code** copies the code alone.
- **Setup…** shows the command line, or the JSON for a host set up by a
  file, that adds the server, with a button to copy it.
- **Write Skill** saves a `SKILL.md` where that host reads skills from,
  and says where it went. `kakel -mcp-skill` prints the same file.
- **Stop Sharing** ends the share, and the code stops working. Taking
  the last pane out does the same.

Share → Permissions… shows four tick boxes for the pane you are on,
saying what the agent may do there beyond reading the pane and typing
into it: read only, read cleared scrollback, open more panes, and
restart the connection. Every box starts off, and a change takes effect
at once. An agent that hits a password prompt can ask you to type it
into the pane: a line appears saying what it wants, what you type goes
to the program, and the agent is told you typed something and never
what.

Kakel writes down what an agent types, and Share → Typing History shows
it for the pane you are on. You hand the pane over, you give the access
and you hold the secrets, so what the agent does in there is yours to
read. It is what the agent sent, not what the shell ran: a line it
edited before pressing Enter is there as it was typed. A secret you type
at the agent's asking is not in it, because you typed that yourself.
The record lives as long as the pane: it is capped, it says how many of
the oldest entries it has dropped, and it goes when the pane closes.

## Serving this window, and working in another

Serving this window and connecting to another are on the Share menu
rather than on a key. "Serve This Window…" asks for the port and where
to listen, and then shows the address and the host key's fingerprint to
check. "Connect to Window…" asks for the address and the key file to
offer. Connecting opens nothing over there: what
that window has open lands in the Servers pane under its name, and the plus
on that heading opens a pane on it. The servers that window is
connected to get headings of their own, and their plus opens things on
them through it. Nothing listens until you ask it to,
and the keys allowed in are the ones you list in an `authorized_keys`
file in kakel's own directory, not the one in `~/.ssh`.

## Where kakel keeps its files

Kakel keeps its files where the operating system puts a program's.
Make a directory called `kakel-files` beside `kakel.exe` and it
keeps them there instead, so one machine can hold several copies with
files of their own. Options → File Locations… names every file and says
how to move them.

Kakel used to be called gridterm. On its first start it renames
gridterm's old directories: `gridterm` becomes `kakel`, and
`gridterm-files` becomes `kakel-files`.

## The file manager

In the file manager: `Tab` and `Shift+Tab` move between panes, `Enter`
opens, `Backspace` goes up and `Space` marks. `F5` or `Ctrl+C` copies,
`F6` or `Ctrl+X` cuts, and `F7` or `Ctrl+V` pastes. `F3` views a file,
`F4` follows one as it grows, and `Ctrl+D` closes the pane. The bar
along the bottom shows the rest, and clicking a key on it runs that key.
