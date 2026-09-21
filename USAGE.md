# Using gridterm

Keys, the shortcuts file, sharing panes with an agent, and where things
are kept. [FEATURES.md](FEATURES.md) says what each of these is for.

## Keys

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
| `F11` | fill the screen with the panes |

## Changing a shortcut

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

## Every pane at once

"Show every pane" on the Go menu, or `Ctrl+Shift+A`, draws every pane at
once on a grid, each one live and shrunk to fit. The arrows walk them,
`Enter` goes to the one marked and `Escape` leaves you where you were. A
click goes straight there. The pictures are shrunk by the GPU rather
than cell by cell, and a window with nothing happening in it still skips
the frames it would have skipped anyway.

## Sharing a pane with an agent

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

## Serving this window, and working in another

Serving this window and connecting to another are on the menu rather
than on a key: "Serve this window…" asks for the port and says the
fingerprint to check, and "Connect to another window…" asks for the
address and the key to offer. Connecting opens nothing over there: what
that window has open lands on the sidebar under its name, and the plus
on that heading opens a pane on it. Nothing listens until you ask it to,
and the keys allowed in are the ones you list in an `authorized_keys`
file in gridterm's own directory, not the one in `~/.ssh`.

## Where gridterm keeps its files

gridterm keeps its files where the operating system puts a program's.
Make a directory called `gridterm-files` beside `gridterm.exe` and it
keeps them there instead, so one machine can hold several copies with
files of their own. "Where gridterm keeps its files" on the Help menu
names every file and says how to move them.

## The file manager

In the file manager: `Tab` and `Shift+Tab` move between panes, `Enter`
descends, `Backspace` goes up, `Space` marks, `F2` renames, `F5` copies,
`F6` cuts, `F7` pastes, `F8` deletes, `F9` makes a directory and `F10`
closes the pane. The bar along the bottom says the same thing, and
clicking a key on it runs that key.

In a dialog, a field that usually holds one of a few answers offers them:
`Ctrl+Down` and `Ctrl+Up` step through what is saved.
