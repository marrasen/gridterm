# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Connections

- Nothing outstanding. "Files" connects the machine when nothing is
  connected to it, the way "Terminal" does, and opens the pane over the
  connection it makes.

## Copying files

- Nothing outstanding. A copy's row opens a dialog saying what it is on,
  how far it has got and how fast it is going, with Cancel while it runs
  and Repeat once it has finished.

## The file browser

- Nothing outstanding. Ctrl+G goes to a path and offers the drives;
  typing a name jumps to it.

## Sidebar, dividers and the agent hand-over

Asked for on 2026-09-16. In the order they are to be done; the one that
needs an answer from Marcus is last.

1. Done. **Copy progress as a filled row.** A copy of one big file says
   "0 of 1" until it is done. The job's row is to fill its background
   from the left in proportion to the bytes copied (files, when the
   bytes are not known yet). `ui.ListRow` gets a `Fill` from 0 to 1 and a
   `FillBG` in the list's style; `refreshJobsAt` sets it from
   `jobs.Progress`. The note stays as it is. Tests start from a copy
   made in the file manager and read the row the list paints.

2. Done. **An X that clears a finished row.** A greyed connection or a
   finished transfer gets a `×` button at the end of its row. Clicking
   it drops the row, which is what "Clear finished connections" does
   for all of them at once. The list's `OnButton` learns `*conns.Entry`
   keys and calls the entry's `Close`. Tests click the `×` on a dropped
   connection and on a finished copy.

3. Done. **A resize cursor over a divider.** The pointer becomes an east-west
   arrow over the sidebar's divider and over a split between panes
   (north-south for a split into rows), and stays so while dragging.
   `ui.Root` answers what is under a cell; the frame loop asks with the
   pointer's cell and calls `ebiten.SetCursorShape`. Tests ask the root
   over each kind of divider and over a pane.

4. **A prompt for the agent, not a bare code.** Done in part. Handing
   a pane over puts a ready-to-paste instruction on the clipboard and
   shows it in the dialog: run `gridterm -mcp` as an MCP server over
   standard input and output, then call `use_session_code` with the
   code, then `read_pane`, `send_keys` and `wait_for` on that pane. The
   MCP server says the same in the `instructions` field of its
   `initialize` answer, and the tool descriptions were reread for an
   agent that knows nothing about gridterm. Marcus answered the open
   question on 2026-09-16: let the user pick the agent host, remember
   the pick, and write a skill as well. That is items 5 and 6.

5. **Pick the agent host, and remember it.** The hand-over dialog
   offers the host the prompt is for: Claude Code, Codex, Cursor, or
   another host that takes a JSON MCP config. The prompt's setup lines
   follow the pick. The pick is kept in the settings file, so the next
   hand-over starts from it.

6. **A skill for the host.** The dialog offers to write a skill for
   the picked host: a `SKILL.md` that says what gridterm is, how to
   reach the server, and how to work in a pane, with the tool workflow
   and the rules from the prompt. For Claude Code it goes under
   `~/.claude/skills/gridterm/`; for a host whose skill directory is
   not known it goes under gridterm's own config directory and the
   dialog says where. `gridterm -mcp-skill` prints the same file. A
   disk error is reported, never worked around.

7. **Tools enough for real work.** Checked against four jobs on
   2026-09-16: getting a user out of vim, opening top sorted on memory,
   installing midnight commander and copying a folder with it, and
   finding why sshd cannot be reached. What holds today: `send_keys`
   puts bytes in as given, so Escape, `:q!` and Enter work, and so do
   `top` and `M`. What is missing:
   - **Named keys.** An agent should not have to know that Escape is
     `` or that an arrow key depends on the program's mode.
     `send_keys` gets a `keys` list of names (Escape, Enter, Tab, Up,
     Down, Left, Right, Home, End, PageUp, PageDown, Insert, Delete,
     F1 to F12, Ctrl+C and the like) that the pane's own terminal
     encodes the way it encodes a key press here. Midnight commander
     needs Tab, F5, Insert and Enter, and vim needs Escape.
   - **Scrollback.** `read_pane` shows the screen and nothing above it.
     A `lines` argument reads that many lines ending at the bottom,
     reaching into what scrolled off, so `ss -tlnp` and `journalctl`
     can be read whole.
   - **Passwords.** The rules must say that a password prompt is the
     user's to answer: the agent asks the user to type it into the
     pane and then waits with `wait_for`. `sudo apt install` needs it.
   - **A screen that never settles.** `wait_for` with nothing to wait
     for waits for quiet, which `top` never is. The description says
     to use `contains` or `read_pane` for a program that keeps drawing.

8. **Say in the menu bar when this window is served or taken over.**
   Asked for on 2026-09-16. The menu bar gets a right-aligned status in
   red: "Controlled by <name> from <address>" while a client is
   attached, and "Serving on <address>, nobody connected" while the
   port is open with nobody on it. Nothing shows otherwise. Clicking
   it opens the serving dialog, which says the address, the fingerprint
   and who is connected, with a button that kicks the client out and a
   button that stops listening. The menu bar learns a status text with
   a colour and a click handler; nothing else in the bar moves.

## Known gaps worth revisiting

- The cursor keeps blinking while the window is in the background.
  Nothing reads `ebiten.IsFocused`, and most terminals either stop the
  blink or draw the cursor hollow once the window loses focus.
- A watcher taking a screen over sees the far end's default cursor.
  `vt.Repaint` puts the cursor back where the program had it but carries
  neither its shape nor its blink, so `DECSCUSR` is lost over the wire.
