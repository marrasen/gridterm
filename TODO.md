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

4. **A prompt for the agent, not a bare code.** Handing a pane over
   puts a ready-to-paste instruction on the clipboard and shows it in
   the dialog: run `gridterm -mcp` as an MCP server over standard input
   and output (with the line that adds it to Claude Code, and the JSON
   for other hosts), then call `use_session_code` with the code, which
   gives a pane id, then `read_pane`, `send_keys` (with `
` for Enter)
   and `wait_for` on that pane, and nothing else. The port is inside the
   code, so no address is needed; the agent has to run on this machine.
   The MCP server also says the same in the `instructions` field of its
   `initialize` answer, so an agent that is already connected learns the
   workflow without the prompt. The tool descriptions are reread as an
   agent that knows nothing about gridterm would read them.

   **Question for Marcus:** which agent hosts should the prompt give a
   config line for? Claude Code is one (`claude mcp add gridterm --
   gridterm -mcp`). Codex, Cursor and a generic `.mcp.json` snippet are
   the others on offer. And is a prompt enough, or should gridterm also
   write a skill file (`gridterm -mcp-skill`) for hosts that install
   skills? The recommendation is the prompt plus the `instructions`
   field, and no skill file until a host needs one.

## Known gaps worth revisiting

- The cursor keeps blinking while the window is in the background.
  Nothing reads `ebiten.IsFocused`, and most terminals either stop the
  blink or draw the cursor hollow once the window loses focus.
- A watcher taking a screen over sees the far end's default cursor.
  `vt.Repaint` puts the cursor back where the program had it but carries
  neither its shape nor its blink, so `DECSCUSR` is lost over the wire.
