# Plan: one code for a share of panes

Asked for on 18 September, after two panes in one agent turned out to
need two codes and two pasted prompts.

What it is: the user starts a share, gets one code, and adds panes to it.
The agent uses the code once and finds the panes with `list_panes`, then
and afterwards. Panes in one share can be on different machines, so "set
this up across three boxes" is one code and one prompt.

Each step is one commit of the work, then read-only adversarial reviews
of that diff, then a commit of the fixes.

## What Marcus settled, 18 September

1. **The tick boxes stay per pane.** Read only on the build being
   watched, typing on the one being worked in.
2. **A pane joins from the menu**, which reads "Share this pane with an
   agent" and then "Add this pane to the share".
3. **The one-pane hand-over goes.** A share of one pane is the same
   thing, and two ways in is two things to explain.
4. **The menu bar says "Remote controlled" and "Sharing with an agent"**,
   short, each on a background so it reads as a button, each opening a
   dialog with the detail the bar used to spell out. The share dialog
   takes panes out of the share.

## What a share is

One code, one set of panes, and one hand-over record per pane inside it.
The record is what it is today -- the boxes, the reading cache, the
prompt the agent last typed at -- and the code moves up to the share.

**A pane's name carries its share.** An agent's pane names become
`<share>.<pane>`, and the MCP server puts the window's port in front as
it already does: `54321/3.7`. That is what lets a connection check, on
its own, that a pane belongs to a share it holds. Nothing else about
names changes: they are opaque to the agent and never come round again.

**Reading: what a share is worth.** The set can change while the agent
works. That is the thing today cannot do: a pane added now needs a new
code and a call the agent has to be told to make.

## Step 1 -- the share, and one code for it

- `agents` holds shares by code. A hand-over belongs to a share.
- `Use` answers with every pane in the share, not one.
- `panes` answers from the share as it stands, so a pane taken out stops
  being listed and a pane added shows up. That also closes two things a
  reviewer found: the list could name a pane the user had taken back, and
  it showed sizes from the moment the pane was taken.
- Authorisation becomes "this pane is in a share this connection holds",
  which is stricter than today's "this id names some live hand-over".
- The protocol says 4: a window of the third version answers `use` with
  one pane, and an agent would hold one pane of a share and never know.

**Reading.** Taking the last pane out of a share ends the share and the
code stops working. Nothing is written down between runs: a share is as
long-lived as the panes in it, like a hand-over today.

## Step 2 -- the dialogs

- **Adding a pane** opens that pane's boxes: the four tick boxes and
  nothing else, since the code belongs to the share.
- **The share dialog** carries the code, the panes in it with what each
  one allows, and a way to take one out. "Copy the prompt",
  "Instructions" and "Write the skill" live here, because they are about
  the share and not about a pane.
- The menu item is "Share this pane with an agent", and reads "Add this
  pane to the share" once a share exists.

**Reading.** One share at a time, unless a second is asked for later. Two
shares means two codes, and the menu would have to ask which one a pane
joins; nothing so far wants that.

## Step 3 -- the menu bar

- `ui.Menubar` takes a list of status chips rather than one line: text, a
  foreground, a background, and what a press does.
- "Remote controlled" replaces "Controlled by <name> from <addr>". The
  name, the address and how many are connected move into the dialog the
  chip opens.
- "Sharing with an agent" is the second chip, and opens the share dialog.
- A window that is serving and has nobody connected says "Serving", in
  the quieter colour it uses now.

**Reading.** The chips are drawn right to left in a fixed order, and one
that will not fit is dropped rather than shortened: a chip cut in half
says nothing.

## Step 4 -- the words

The prompt, the server's instructions and the tools say that a code names
a share, that `list_panes` is how the agent learns what is in it, and
that the set can change while it works.
