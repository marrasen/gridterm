# Plan: the MCP parts of the TODO

What gets built, in what order, and the reading each step is built on.
Everything here comes from `TODO.md`, under "The agent, through MCP".
Marcus may be away while this runs, so each step names the plainest
reading of its TODO line, and he can reverse any of them from here.

Each step is one commit of the work, then read-only adversarial reviews
of that diff, then a commit of the fixes. The TODO lines a step closes
go in the same commit as the work.

## The order, and why

1. The cheap wording and dialog changes first, so the hand-over dialog
   has settled before the tick boxes are added to it.
2. Then the command status, because the boundary of the last command's
   output is read off the same marks.
3. Then the output of the last command, which uses that boundary.
4. Then `clear` as a floor, which is the other boundary a read stops at.
5. Then the tick boxes, which turn the floor into a per-pane choice and
   gate two new tools.
6. Asking the user for a secret last: it is the only step that makes the
   window wait for the user.

## Step 1 -- Shrink the hand-over prompt

TODO "The agent, through MCP" item 7.

`handoverPrompt` in `agents.go` keeps the setup lines and the code, and
drops `mcp.Workflow` and `mcp.Rules`. The MCP server's `instructions`
already carry both and the agent reads them when it connects.

**Reading.** The prompt still says what the pane is and that the code is
the only credential, because an agent that has not connected yet has
nothing else to go on.

## Step 2 -- The hand-over dialog

TODO items 9 and 10.

- "Copy the prompt" only copies. A second button, "Install
  instructions", opens the dialog that says how to add the MCP server.
- That dialog's setup line can be copied.

**Reading.** The copy chord on the instructions dialog copies the setup
command line, not the whole dialog. `ui.Notice` copies its whole
message; this is a confirm, and what the user wants off it is the
command.

## Step 3 -- Say when a command has finished

TODO item 1.

- `ui/term.Reading` carries what `vt.Terminal.Command()` says, read under
  the same lock as the screen, so a screen and an exit status come from
  one moment.
- `agent.Look` carries it to the tools: whether the shell reports status
  at all, whether a command is running, how many have finished, and the
  last status.
- The wait loop ends when a command finishes, as well as on the text, the
  quiet and the program going.
- A shell with no marks: the window notes where the cursor row's prompt
  was when `send_keys` typed, and the wait ends when that prompt is back
  at the bottom of the screen.
- Every answer says which of the two it used, or that neither was
  available.

**Reading.** `Done` is the signal, as the TODO says: a wait that starts
while a command is running ends when `Done` has moved past what it was
when the wait began. A wait with `contains` still ends on the text.

## Step 4 -- The output of the last command

TODO item 2.

`vt` records where the last command's output began, as a line number
that survives scrolling. A new tool, `read_output`, gives the agent the
lines from there to the bottom.

**Reading.** A new tool rather than an argument on `read_pane`, because
`read_pane`'s `lines` counts rows from the bottom and this counts from a
boundary. A shell with no marks has no boundary, and the tool says so and
points at `read_pane`.

## Step 5 -- `clear` keeps the history and hides it from the agent

TODO item 11.

`ED 3` stops dropping the scrollback and marks a floor instead. The user
scrolls up and still sees everything. An agent's read stops at the floor
and is told that is everything there is.

**Reading.** The floor moves only forward, and a reset of the emulator
puts it back. What the user sees does not change at all: `Render`,
`RenderBack` and `History` still see every line.

## Step 6 -- The tick boxes

TODO item 4, first half.

A tick box each on the hand-over dialog: restart a closed connection,
open another pane to the same server, read only, read above a clear.
Every one starts off, what is ticked is remembered, and a box takes
effect at once. The window enforces; the `Use` answer tells the agent
what it may do.

**Reading.** The boxes are one remembered set, not one per pane: the
TODO says "the way the picked agent already is", and the picked agent is
one setting. The set is what the dialog opens on; what is in force is
what that pane was handed over with.

## Step 7 -- The two tools the boxes open

TODO item 4, second half.

- `restart_pane` picks the choice the question on a dead pane offers.
- `open_pane` opens another pane on the machine the pane is on, handed
  over as it opens.

**Reading.** `open_pane` opens no connection: it fails unless the window
already holds one to that machine. On a local pane it opens another pane
on this machine.

## Step 8 -- Let the agent ask for a secret

TODO item 3.

A tool that puts a prompt on the pane, waits until the user has typed
into it and pressed Enter, and returns without the characters ever
reaching the agent.

**Reading.** The user types into the pane itself, so the secret goes to
the program that asked for it and never through gridterm's own field.
The tool answers "the user typed something" or "the user cancelled", and
nothing else.
