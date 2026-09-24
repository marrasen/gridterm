# Plan: a file pane outlives the connection under it

Issue #23, 24 September. The reported symptom is a dialog:

```
Trouble closing the file panes on Skylake
remote: close SFTP on rdp@marras-skylake:22: EOF
```

A connection dropped, `machineDied` closed every file pane on that
machine (`machines.go:426`), and closing one tries to close an SFTP
session that has already gone. So the window tears down the user's
panes and then complains about how the teardown went.

What Marcus asked for: **do not close them. Connect again when the user
does something.** The same for repeating a file copy.

Each step is one commit of the work, then a read-only adversarial
review of that diff, then a commit of the fixes.

## Why the panes are closed today

`machineFiles` builds the filesystem out of the live connection
(`browse.go:285`), so when that connection goes the filesystem is dead
and every call on it fails. Closing the pane was the only answer the
window had. The comment says so: "it is taken away here, because
nothing else would".

## The design: a filesystem that opens itself again

A wrapper in the main package, around `vfs.FS`, holding the machine's
name and whatever is open on it now:

- **The I/O methods** -- `ReadDir`, `Stat`, `Open`, `Create`, `Mkdir`,
  `Symlink`, `Remove`, `Rename`, `Chmod`, `Home` -- try what is open,
  and on a failure that says the connection has gone, open it again and
  try once more. Once: a second failure is the machine's answer and not
  a stale session.
- **`Roots`, `Sep`, `Name`** answer from what they remember. They are
  asked on the goroutine that draws, which must not go to the network.
- **`Place`** is the machine, not the connection. Today two panes on
  one machine are the same place only while one connection lives, so a
  move between them becomes a copy after a reconnect. Naming the
  machine fixes that and is what `Place` is documented to mean:
  "a value equal for two filesystems on one machine and for nothing
  else".
- **`Close`** closes what is under it and does not open anything.

**Reconnecting from a worker goroutine.** The I/O methods run off the
drawing goroutine already, so they may wait. The work is posted to the
pump, which is where the machines live: connected, hand back a fresh
filesystem; a dial already in flight, queue behind it the way the
"already connecting" dialog's `Wait` does (`dialling.go:111`); nothing
yet, start one. The answer comes back down a channel.

**Reading.** Whatever the connection needs on the way up -- a
passphrase, an unknown host key -- still asks, because it must. What
the user sees is a folder click that takes a moment and may put a box
up, which is what opening a machine has always looked like.

## Step 1 -- the pane survives, and reads again

- `machineDied` stops closing the file panes. The row greys, the way a
  shell's does, so the sidebar says the connection is not live.
- The wrapper, and `machineFiles` returning one for a machine.
- A read after the drop opens the machine again and answers.

**Reading.** This is the whole of the reported bug: the panes stay, and
the dialog that complained about closing them is gone with the closing.

**Done.** The
pane stays, its filesystem is told, and a read afterwards opens the
machine again.

**Reading, found while building it.** A filesystem whose pane has been
closed must open nothing more. A finished copy holds the filesystems it
ran on, and one of those outlives the pane it came from -- so a job
pane drawn after its machine went reached through that copy and opened
a connection nobody had asked for, which put a row on the sidebar out
of nothing. That is also what keeps step 3 out of step 1.

## Step 2 -- everything else the pane does

Stat, open a file, make a directory, rename, remove, chmod. They go
through the same wrapper, so they come free -- but each has its own
failure path in the browser and each needs a test.

**Done.** Make a directory, rename, read a file, delete: each asked for
the way the user asks for it. Rename asks twice, once to see whether
the name is taken and once to rename, and makes one connection between
the two.

**Reading, found while building it.** A reader outlives the browser
pane it was opened from, and the browser letting go of a filesystem a
reader still holds left that filesystem unable to open the machine --
the reader still on screen with nothing behind it. Closing a filesystem
is now the single place it is forgotten, so one still being read
through, or still carrying a job, keeps its machine until the last of
those has finished with it.

Chmod and Symlink are not browser commands: only the copy jobs use
them, so they are step 3's.

## Step 3 -- a copy again

A job holds the filesystem it was given. Holding the wrapper rather
than the session means a repeat opens the machine again by itself.

**Reading.** A job that is *running* when the connection goes is a
different thing and is not this: it has half-written a file, and what
to do about that is the question `jobpane` already asks. This is only
about running it again afterwards.

**Done.** A repeat opens its ends afresh, and a machine that is not
connected is connected to first, one end after the other. A job end
remembers the step its machine was reached by, the way the pane's own
filesystem does, so a repeat onto a typed target works too.

**What this changed.** A repeat used to refuse a machine that was not
connected -- "Could not copy it again". Pressing the button means do
this work, so the window opens the machine and does it, saying so on
the bottom row. The refusal is left for an end that cannot be reached
at all: nothing connected, no route on any list, and no step kept.

## What Marcus settled, 24 September

1. **The bottom row says it is reconnecting.** A folder click that takes
   ten seconds with nothing on screen reads as a window that has
   stopped. The line goes up when the connection is asked for and comes
   off when the machine has answered or failed.

   **Built as a held line, not a timed one.** `say` gives a line four
   seconds, which is right for one saying something worked and wrong
   for one saying what the window is doing: a login can take longer,
   and the row going blank half way through is the symptom the line
   exists to prevent. `sayWhile`/`doneSaying` hold it. Every way of
   waiting says it, not only the one that starts the connection: a
   second pane queueing behind the first waits just as long.

2. **A repeat that has to open a machine ignores a second press.** The
   button used to be instant, so pressing twice meant two copies. It
   now takes the length of a login, and the press in that time is the
   user wondering whether the first one registered.
3. **A rename is followed wherever the machine is.** Connected, still
   being dialled, or dropped with only a file pane holding it: the
   panes, the rows and the finished work all follow. Dropped is the
   likeliest of the three, because a pane outlives its connection now
   and that is when a user tidies the server list. A rename that
   changes the address as well is a different machine under that name,
   and nothing follows it -- the rule the connection has always had,
   now applied to the rest.
4. **A repeat follows a rename.** Work keeps the name its machine had
   when it started. Opening it again under that name would log in to
   the machine a second time and put a second group on the sidebar, so
   the window keeps a trail of what the user renamed and follows it.
   Not matched by address: two machines reached through different jump
   hosts can have one address between them, and a repeat that picked
   the wrong one would write the user's files onto a machine they never
   named. A name given to another machine since stands for that one, so
   work pointing at it is left pointing at it for as long as the list
   says so.
5. **A repeat connects.** Pressing "Do it again" on work whose machine
   has gone opens that machine rather than refusing.

## Left as it is

**Choosing the same saved copy twice while it reconnects starts two.**
The job pane's Repeat ignores a second press, because the button sits
there looking unpressed for the length of a login. The Saved Copies
dialog closes as soon as one is chosen, so the user has to reopen it to
choose again, and doing that is asking for it twice.

**A failed copy's clean-up reconnects.** A job that dies with the
transport removes what it half wrote, and that call goes through the
wrapper, so it opens the machine again to do it. The alternative is a
`.gridterm-part` file left on the machine for good. The user sees
`Reconnecting to X…` for a copy that has just failed.

## Settled while building

1. **A read waits for one connection, and asks again only for someone
   else's.** When the dial this read started settles, the answer is in:
   the machine is there or it is not. Asking again on the strength of
   "the way there is still open" dials the far end again the moment it
   failed, and again, for as long as the jump host stays up -- a pane
   for every attempt and a read that never comes back. It also dialled
   again the moment the user gave up, which is the opposite of what
   giving up means. A dial that was on its way somewhere else says
   nothing about this machine, and that one is worth asking again.

3. **How many panes reconnect at once?** One. They all go through the
   dial queue, and a caller blocked on an answer queues on `answering`
   rather than `waiting`, so a connection that is not made comes back
   as a failure instead of leaving the pane reading for ever.

## How the wrapper knows its connection has gone

Not by reading the error. Telling "the connection went" from "there is
no such file" means classifying whatever sftp, ssh and the net packages
happen to say, and getting that wrong either reconnects over a real
answer or leaves a dead session in place.

`machineDied` already walks the panes on the machine, to close them.
It tells them instead, and a wrapper that has been told opens itself
again on the next call. That is exact, and it is the same walk the bug
was in.
