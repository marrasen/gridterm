# Every dialog in gridterm — rewrite

A change sheet against `Every_dialog_in_gridterm.md`, matched on the
D-ids. Each entry gives the on-screen text as it should read after the
change, in full, so nothing has to be inferred from a diff. `Code:`,
`Opened from:` and `Notes:` lines are not repeated; they stay as they
are unless an instruction says otherwise. Focus stays where it is
unless stated.

`Body: none` means the dialog has no body lines at all.

---

# House rules

These are what every entry below was written to. They are also in
[WORDING.md](WORDING.md), which is where a new dialog should be written
from: this file is the change sheet, that one is the standing rule.

1. **Say what it is. Never what it is not.** No "not a path", "rather
   than", "nothing else", "nothing changes", "not what the shell ran".
2. **Use the words software uses.** Lost, invalid, failed, not found,
   cancel, retry, create, remove, save, reload. A program does not
   "go", "give up", "let go", "hold", "forget", "take", "kick out" or
   "say something else".
3. **The title is the message.** The body adds only what the title
   cannot: which machine, which path, one consequence. If the title
   says it all, there is no body.
4. **Never explain the program's reasoning.** The user needs the
   choice, not why gridterm has to offer it.
5. **Buttons are one verb**, from a small fixed set: `OK` `Cancel`
   `Close` `Retry` `Wait` `Save` `Create` `Delete` `Remove` `Replace`
   `Skip` `Open` `Run` `Connect`. No pronouns: never `Make it`,
   `Close it`, `Keep them`, `Leave it`.
6. **`Cancel` cancels what the dialog is about. `Close` dismisses a
   dialog and leaves things running.** Same meaning everywhere (D04,
   D09, D34).
7. **Errors are two or three words, sentence case, no full stop:**
   `Invalid passphrase`, `Enter a command`.
8. **Placeholders are a format or the word `Optional`.** Never a
   sentence, never "what to call it".
9. **If a sentence exists to excuse a behaviour, change the
   behaviour.** Retry limits, fields that are silently ignored and
   toggle buttons are all fixed below rather than explained.
10. **A warning earns its place only where the action exposes
    something or cannot be undone**, and then it is one sentence
    stating the consequence (D08, D17, D29, agent forwarding in D11).

## Vocabulary

| Was | Is |
|---|---|
| went, goes, gone | lost, closed, stopped, not found |
| give up | cancel |
| let go of | disconnect from |
| forget / forgotten | remove |
| remember / kept | save / saved |
| make | create |
| reread | reload |
| chord | shortcut |
| picture | image |
| things | items |
| hand-over | share |
| take (a menu line) | choose |
| through (a server) | jump host |

---

# Global instructions

- **G1. `Copy` on a Notice only when there is something to copy:** a
  path, an error, a log, a key. Give `ui.Notice` a flag. A notice that
  says "done" has `OK` alone.
- **G2. Success is not a dialog.** When an action worked and there is
  nothing to read, show one line in the status line and no dialog.
  Applies to D41 and D46.
- **G3. Cycling fields explain themselves.** `ui.Form` draws
  `Ctrl+↑/↓` beside a cycling field while it has focus. Delete every
  body sentence about stepping, choosing, ticking with space, or
  clearing a box.
- **G4. Per-field hints.** A field may carry one hint line, drawn at
  the bottom of the form only while that field has focus. This
  replaces the paragraph-per-field bodies (D11, D19).
- **G5. Fields that do not apply are disabled**, not accepted and then
  explained away on save (D11).
- **G6. A dialog title matches the menu row that opens it**, minus the
  ellipsis. Menu rows that change as a result are listed at the end.

---

# 1. Connecting to machines

## D01
- **Title:** `Connect to server`
- **Body:** none
- **Fields:** `Server` — placeholder `[user@]host[:port]`
- **Buttons:** `Connect` · `Cancel`

## D02
- **Title:** `Connect to window`
- **Body:**
  ```
  The other window must be serving, with this
  machine's public key in its authorized_keys.
  ```
- **Fields:**
  - `Host` — placeholder `host[:<servePort>]`
  - `Key file` — placeholder `Optional`
- **Buttons:** `Connect` · `Cancel`

## D03
- **Title:** `Connection lost`
- **Body:** `<name>` — and, when there is a reason, `<why>` on a second line
- **Buttons:** `Reconnect` · `Close`

**Instructions:** Same shape as D06: the title says what, the body says
which.

## D04
- **Title:** `Already connecting to <name>`
- **Body:** none
- **Buttons:** `Wait` · `Retry` · `Cancel`
- **Focus:** `Wait`

**Instructions:** `Wait` joins the attempt in progress. `Retry` cancels
that attempt and dials again. `Cancel` closes the dialog and drops this
request; the attempt in progress carries on. I used `Cancel` where you
said `Abort`: Abort reads as "kill the connection attempt", which is
what Retry does, and Cancel already means "close this, change nothing"
in every other dialog. Swap the word if you still prefer it.

## D05
- **Title:** `Unlock private key`
- **Body:** the key's path
- **Fields:** `Passphrase` — masked
- **Buttons:** `Unlock` · `Cancel`
- **Error:** `Invalid passphrase`

**Instructions:** Remove the retry limit and all three counting
variants. The key file is on the local disk, so a limit protects
nothing. The dialog stays open, the field is cleared, and the user
types again or presses Cancel. I kept the word passphrase because D06
asks for the account password, and the two words are the only thing
telling the user which secret is wanted.

## D06
- **Title:** `Password`
- **Body:** `<user>@<host>`
- **Fields:** `Password` — masked
- **Buttons:** `Sign in` · `Cancel`
- **Error:** `Invalid password`

**Instructions:** Show the error when the server rejects the password
and asks again. No count here either; the server decides when it has
had enough, and that arrives as a connection error.

## D07
- **Title:** `Authentication`
- **Body:** `<user>@<host> asks:` then the server's text
- **Fields:** as today
- **Buttons:** `OK` · `Cancel`

**Instructions:** gridterm's own line stays first, for the reason in
the Notes.

## D08
- **Title:** `Unknown host key`
- **Body:**
  ```
  <addr> is not in known_hosts.

  <keytype>  <fingerprint>

  Verify the fingerprint before connecting.
  ```
- **Buttons:** `Connect` · `Cancel`
- **Focus:** `Cancel`

## D09
- **Title:** `Waiting for server`
- **Body:** `<user>@<host>:` then the server's lines, then
  `Continues automatically when you are done.`
- **Buttons:** `Open link` *(only with exactly one URL)* · `Copy` ·
  `Close` · `Cancel` *(Cancel only when the connection can be stopped)*
- **Focus:** `Close`

**Instructions:** This dialog usually carries a sign-in link, so it
needs `Copy`, which copies the server's lines. With that, the sentence
about the pane is dropped. `Close` dismisses the dialog and keeps
waiting. `Cancel` stops the connection.

`Open link` is drawn only when the server's lines hold exactly one URL.
With none or several, the button is absent and `Copy` does the job.

- **What counts as a URL:** `http://` or `https://` only, found in the
  text after `serve.Plain` has stripped it. Trailing `.` `,` `)` `>` and
  quotes are trimmed, and the opening bracket or quote with them. No
  other schemes, ever: the text comes from the server, and `file:` or a
  custom protocol handler must not be reachable from it.
- **Action:** hands the URL to the system's default browser. The dialog
  stays open, since the connection continues by itself and the dialog
  closes when it settles.
- **Never opened automatically.** Only on the button press.
- **Focus stays on `Close`**, so a stray Enter cannot launch a browser at
  an address the server chose.
- **The body is unchanged**, so the URL stays visible before the button
  is pressed.
- **On failure:** a D57 notice titled `Could not open the link`.

The same address written twice counts as one: the button still means one
thing. `onlyLink` in `ask.go` is where this lives.

## D10
- **Title:** `Connection log — <name>`
- **Body:** the log
- **Buttons:** `Copy` · `OK`

**Instructions:** One title. The log itself shows whether it is still
connecting.

---

# 2. The saved server list

## D11
- **Title:** `Add server`, or `Edit <name>`
- **Body:** none
- **Fields:**
  - `Name` — no placeholder
  - `Type` — cycles `SSH` / `gridterm window`
  - `Server` — placeholder `[user@]host[:port]`
  - `Key file` — placeholder `Optional`
  - `Jump host` — placeholder `Optional`
    - hint: `Connect through another saved server`
  - `Folders` — placeholder `Comma-separated paths`
    - hint: `Where the file browser opens on this server`
  - `Shell setup` — cycles `No` / `Yes`
    - hint: `Tracks the directory and where each command ends. bash and zsh only.`
  - `Forward SSH agent` — cycles `No` / `Yes`
    - hint: `The server can use your keys for onward connections. So can root on the server.`
- **Buttons:** `Save` · `Remove` *(edit only)* · `Cancel`

**Instructions:** The whole body goes; G3 and G4 replace it. When Type
is `gridterm window`, disable `Jump host` and `Forward SSH agent` and
ignore any `user@`. Delete both lines that are added on save. Disable
`Jump host` when no other server is saved, and delete the sentence
that says so.

## D12
- **Title:** `Remove <name>?`
- **Body:**
  - connected, to a machine or a window:
    `<name> is connected. Removing it closes the connection and its panes.`
    — with things reached through it:
    `Removing it closes the connection and everything through it: <what>.`
  - still connecting: `Removing it cancels the connection in progress.`
  - nothing open: none
- **Buttons:** `Remove` · `Cancel`
- **Focus:** `Cancel`

---

# 3. SSH keys

## D13
- **Title:** `New SSH key`
- **Body:**
  ```
  Creates an ed25519 key pair.
  The public key is saved as <file>.pub.
  ```
- **Fields:**
  - `File` — pre-filled; placeholder `Private key path`
  - `Comment` — placeholder `Optional`
  - `Passphrase` — placeholder `Optional`, masked
  - `Confirm passphrase` — masked, no placeholder
- **Buttons:** `Create` · `Cancel`
- **Error:** `Passphrases do not match`

## D14
- **Title:** `Key created`
- **Body:**
  ```
  Private key:  <path>
  Public key:   <path>.pub

  <the public key line>

  To install it on a server:
    ssh-copy-id -i <path>.pub user@host
  ```
- **Buttons:** `Copy public key` · `Copy` · `OK`

**Instructions:** No sentence about where the command runs. The action
button copies the public key line alone, which is the one thing anyone
wants from this dialog.

---

# 4. Tunnels

## D15
- **Title:** `Tunnel via <host>`
- **Body:** none
- **Fields:**
  - `Direction` — cycles `Local — listen here` /
    `Remote — listen on <host>`
  - `Listen on` — placeholder `[address:]port`; cycles the saved tunnels
  - `Forward to` — placeholder `host:port`
  - `Save this tunnel` — tick box, off
- **Buttons:** `Open` · `Cancel`
- **Focus:** `Open`

**Instructions:** Direction moves from the buttons into a field. Local
and Remote are the words every SSH client uses, the two options
describe themselves, and the body that had to explain the buttons is no
longer needed. It also makes D15 and D16 end the same way: `Open` ·
`Cancel`. Picking a saved tunnel sets the direction too.

## D16
- **Title:** `SOCKS proxy via <host>`
- **Body:** `A local SOCKS port. Connections go out from <host>.`
- **Fields:** `Listen on` — placeholder `[address:]port`
- **Buttons:** `Open` · `Cancel`

## D17
- **Title:** `Open <address:port> to the network?` — or
  `Open a port on <host> to the network?`
- **Body:**
  - tunnel: `Anyone who can reach <where> on that port is connected to <target>, with no authentication.`
  - SOCKS: `Anyone who can reach <where> on that port can connect to anything <host> can reach, with no authentication.`
  - remote forward adds: `<host> chooses where it listens. With GatewayPorts on, that is its whole network.`
- **Buttons:** `Open` · `Cancel`
- **Focus:** `Cancel`

---

# 5. Running a command

## D18
- **Title:** `Run command on <where>`
- **Body:** none
- **Fields:**
  - `Command` — no placeholder; cycles the saved commands
  - `Directory` — placeholder `Optional`
  - `Save this command` — tick box, off
- **Buttons:** `Run` · `Cancel`
- **Error:** `Enter a command`

---

# 6. Serving this window

## D19
- **Title:** `Serve this window`
- **Body:**
  ```
  A connected window can open shells, use the ones
  running, and read and write files as you.

  Allowed keys, from <authorized_keys path>:
    <one line per key>
  ```
- **Fields:**
  - `Port` — pre-filled; hint: `0 picks a free port`
  - `Listen on` — cycles `This machine only` / `All networks`
- **Buttons:** `Serve` · `Cancel`

**Instructions:** The text after the fields goes. `All networks` needs
no paragraph, and the Tailscale remark belongs in documentation.

## D20
- **Title:** `Serving this window`
- **Body:** the listening address, then `Connected:` with
  `  <name> from <addr>` per client, or `No one is connected.`
- **Buttons:** `Close` · `Disconnect <name>` *(or `Disconnect all`)* ·
  `Stop serving`
- **Focus:** `Close`

## D21
- **Title:** `Resume serving?`
- **Body:**
  ```
  Port:       <port, or "any free port">
  Listen on:  <where>
  ```
- **Buttons:** `Serve` · `Not now` · `Don't ask again`
- **Focus:** `Serve`

---

# 7. Agents

## D22
- **Title:** `Agent permissions`
- **Body:** `The agent can read this pane and type into it. Changes apply immediately.`
- **Fields** (tick boxes):
  - `Restart the connection`
  - `Open more panes`
  - `Read only`
  - `Read cleared scrollback`
- **Buttons:** `Close` · `Remove pane`
- **Focus:** `Close`

**Instructions:** The paragraph about where the code lives goes.
`Close` because nothing is pending; every box has already applied.

## D23
- **Title:** `Agent share`
- **Body:**
  ```
  An agent with this code can read and type in the panes below.

    <code>
  ```
  and after the rows: `<shortcut> copies the code.`
- **Fields:** as today
- **Buttons:** `Copy prompt` · `Setup` · `Write skill` · `Close` ·
  `Stop sharing`
- **Focus:** `Copy prompt`

## D24
- **Title:** `Set up <host>`
- **Body:** that host's setup lines, and nothing after them
- **Buttons:** `<host.copyTitle()>` · `Close`

**Instructions:** The button already says what it copies, so the
sentence describing the button goes, and the one about the shortcut
with it. The paragraph about gridterm's own path goes too; the setup
lines simply use `gridterm`. Keep every `copyTitle()` to two words:
`Copy command`, `Copy config`.

## D25
- **Title:** `Skill written`
- **Body:** `<path>` then `Restart <host> to load it.`
  — or, when the location is unknown: `<path>` then
  `Copy it to where <host> reads skills from.`
- **Buttons:** `Copy` · `OK`

## D26
- **Title:** `Replace existing skill?`
- **Body:**
  ```
  <path>

  The existing skill has been edited.
  Replacing it discards those edits.
  ```
- **Buttons:** `Replace` · `Cancel`
- **Focus:** `Cancel`

**Instructions:** Focus moves to `Cancel`, in line with D12, D17, D29
and D32: the button that changes nothing.

## D27
- **Title:** `gridterm path not found`
- **Body:** `The prompt uses "gridterm" as the command. It works when gridterm is on the PATH.`
- **Buttons:** `OK`

## D28
- **Title:** `Typing history`
- **Body:** `Input as the agent sent it, edits included.` then the column
  of times
- **Buttons:** `Copy` · `OK`

---

# 8. Files

## D29
- **Title:** `Delete <name>?` — or `Delete <n> items?`
- **Body:** `<filesystem>: <path>` then `This cannot be undone.`
- **Buttons:** `Delete` · `Cancel`
- **Focus:** `Cancel`

## D30
- **Title:** `New directory`
- **Body:** `<filesystem>: <path>`
- **Fields:** `Name` — no placeholder
- **Buttons:** `Create` · `Cancel`

## D31
- **Title:** `Rename <name>`
- **Fields:** `Name` — pre-filled, no placeholder
- **Buttons:** `Rename` · `Cancel`

**Instructions:** An unchanged name closes the dialog and does nothing.
The error line goes.

## D32
- **Title:** `Replace <name>?`
- **Body:** `Existing <kind> on <filesystem>: <size>, modified <when>.`
- **Buttons:** `Replace` · `Replace all` · `Skip` · `Skip all` · `Cancel`
- **Focus:** `Skip`

**Instructions:** Dismissing the dialog stops the whole job today, with
no button that says so. Add `Cancel`, which stops the job, and map Esc
to it.

## D33
- **Title:** `Go to directory`
- **Fields:** `Path` — pre-filled, no placeholder
- **Buttons:** `Go` · `Cancel`
- **Error:** `Enter a path`

## D34
- **Title:** as today
- **Body:** as today
- **Buttons while running:** `Cancel` · `Close`
- **Buttons when finished:** `Repeat` *(copies only)* · `Close`
- **Fields when finished:** `Save this copy` — tick box *(copies only)*
- **Focus:** `Close`

**Instructions:** The `Remember` / `Forget` button that changes its
name becomes a tick box, matching `Save this tunnel` and `Save this
command`. Ticked means it is in Saved Copies.

## D35
- **Title:** `Copied <n> files to <dir> on <machine>`
- **Body:** none — or `<n> failed. Details are on their rows.`
  — or `Already exists.`
- **Buttons:** `Copy` · `OK`

## D36
- **Title:** `File copied`
- **Body:** `<path> on <machine>`
- **Buttons:** `Copy` · `OK`

## D37
- **Title:** `Saved copies`
- **Body:** `None saved. A finished copy has a "Save this copy" box.`
- **Buttons:** `OK`

## D38
- **Title:** `Saved copies`
- Rows and `×` as today.

---

# 9. Looks and settings

## D39
- **Title:** `Theme`
- **Rows:** the theme in use is noted `current`

## D40
- **Title:** `Theme file created`
- **Body:**
  ```
  <path>

  A copy of <theme>. Edit it, then choose
  Options › Reload › Themes.
  ```
- **Buttons:** `Copy` · `OK`

## D41
**Instructions:** No dialog (G2). Status line: `Shortcuts reloaded`.
If a dialog has to stay, it is the title `Shortcuts reloaded`, no
body, `OK`.

## D42
- **Title:** `Shortcuts file created`
- **Body:**
  ```
  <path>

  Contains every current shortcut. Edit it, then
  choose Options › Reload › Shortcuts.
  ```
- **Buttons:** `Copy` · `OK`

**Instructions:** The two paragraphs on how overrides work move into
the file itself, as a comment block at the top. That is where someone
editing the file is looking when they need them.

## D43
- **Title:** `File locations`
- **Body:** one line per file, with these labels: `Settings`,
  `Saved servers`, `Themes`, `Shortcuts`, `Authorized keys`,
  `Known windows`, `Serving key`. Then the `~/.ssh` note cut to one
  line. Then either `Portable: files are kept beside gridterm.` or
  nothing.
- **Buttons:** `Make portable` *(same condition as today)* · `Copy` · `OK`
- **Focus:** `OK`

**Instructions:** "A copy that carries its own files" is portable mode,
and that is the word people search for. The three steps go, since the
button does them.

## D44
- **Title:** `Made portable`
- **Body:** one line per action: `Created <dir>`, `Copied <file>`
- **Buttons:** `Copy` · `OK`

## D45
- **Title:** `Terminal identity`
- **Fields:** `TERM_PROGRAM` — as today
- **Body:**
  ```
  Programs read TERM_PROGRAM to identify the terminal.
  Blank reports gridterm.

  Another name can enable features such as inline images.
  It can also produce sequences that gridterm shows as text.

  Applies to new panes.
  ```
- **Buttons:** `Save` · `Cancel`

## D46
**Instructions:** No dialog (G2). Status line:
`Shell setup on — applies to new panes`, or `off`. If a dialog has to
stay: title `Shell setup <on|off>`, body `Applies to new panes.`, `OK`.

## D47
- **Title:** `Shell not found`
- **Body:** `<shell> is no longer installed. The default shell was opened. Choose another under File › New Terminal In.`
- **Buttons:** `OK`

## D48
- **Title:** `WSL shells unavailable`
- **Body:** `Could not list the WSL distributions:` then `<error>`
- **Buttons:** `Copy` · `OK`

## D49
- **Title:** `Could not read some fonts`
- **Body:** `<error>`
- **Buttons:** `Copy` · `OK`

## D50
- **Title:** `Could not read some fonts`
- **Body:** `While looking for a fallback for a missing character:` then `<error>`
- **Buttons:** `Copy` · `OK`

---

# 10. The window itself

## D51
- **Title:** `Exit gridterm?`
- **Body:** `Still open: <list>.` — the last item reads `an agent share`
- **Buttons:** `Exit` · `Cancel`
- **Focus:** `Cancel`

## D52
- **Title:** `About gridterm`
- **Body:**
  ```
  A GPU-rendered terminal emulator for Windows.

  Version: v0.2.0
  ```
  The version is what the build calls itself: a tag for a release, and
  `dev-<commit>` for a build from a working tree.
- **Buttons:** `Check for updates` · `Copy` · `OK`
- **Focus:** `OK`
- **Notes:** `Copy`, because the version is the first thing a bug report
  needs. `Check for updates` opens D58 or D59, or says the answer on the
  bottom row when there is nothing to fetch.

## D53
- **Title:** `Keys and commands`
- Body and buttons as today.

## D54
- **Title:** `Split with`
- **Error:** `Window too small`

## D55
- **Error:** `Window too small`

## D56
No change.

## D57

**Instructions:** One shape only: `Could not <verb> <object>`. A
partial success is `<What worked>, but <what did not>`. Several titles
that differ only in internals merge, because the body carries the
error and the user cannot act on the difference.

```
A command could not be closed                      → Could not close the command
Could not clear that row                           → Could not clear the row
Could not close that pane                          → Could not close the pane
Could not close the connection the file was read through
                                                   → Could not close the file's connection
Could not close the pane                           (keep)
Could not close the pane that was connecting       → Could not close the pane
Could not close the reader                         → Could not close the file
Could not close the scrollback                     (keep)
Could not connect                                  (keep)
Could not connect to <name>                        (keep)
Could not copy to the clipboard                    (keep)
Could not finish <job>                             (keep)
Could not forget which shell to open               → Could not save the settings
Could not let go of <addr>                         → Could not disconnect from <addr>
Could not let go of a filesystem                   → Could not disconnect the filesystem
Could not let go of <name>                         → Could not disconnect from <name>
Could not make the directory                       → Could not create the directory
Could not open a terminal here                     → Could not open a terminal
Could not open <at>                                (keep)
Could not open it on <name>                        → Could not open on <name>
Could not open that tunnel's pane                  → Could not open the tunnel's pane
Could not open the tunnel                          (keep)
Could not paste                                    (keep)
Could not paste a picture into <name>              → Could not paste the image into <name>
Could not paste a picture onto <host>              → Could not paste the image to <host>
Could not read a directory                         → Could not read the directory
Could not read <name>                              (keep)
Could not remember what the serve dialog was set to → Could not save the settings
Could not remember what this hand-over allows      → Could not save the settings
Could not remember which agent this was for        → Could not save the settings
Could not remember which shell to open             → Could not save the settings
Could not rename it                                → Could not rename <name>
Could not run it on <where>                        → Could not run the command on <where>
Could not say what is being served                 → Could not show the serving status
Could not show what this window is serving         → Could not show the serving status
Could not start it again in this pane              → Could not restart the command
Could not take that file                           → Could not copy the file
Could not write the skill for <host>               (keep)
Go to                                              → Could not go to <path>
That could not be done                             → Operation failed
The colour themes could not be found               → Could not find the themes
The colour themes could not be read                → Could not read the themes
The font size could not be changed                 → Could not change the font size
The key was made and not added to the list         → Key created, but not added to the list
The key was made and this window cannot say where to install it
                                                   → Key created, but install steps are unavailable
The server list could not be read                  → Could not read the server list
The server was saved and its key was not added to the list
                                                   → Server saved, but its key was not added
The settings could not be read                     → Could not read the settings
The theme's typeface could not be read             → Could not read the theme's font
The tunnel <label> stopped                         → Tunnel <label> stopped
The window serving <name> was lost                 → Connection to <name> lost
This copy could not be given files of its own      → Could not make portable
Trouble closing <name>                             → Could not close <name>
```

---

# The two the update check opens

## D58
- **Title:** `Update available`
- **Body:**
  ```
  v0.2.0 is the newest release; this build is v0.1.0.
  https://github.com/marrasen/gridterm/releases/tag/v0.2.0
  ```
- **Buttons:** `Open` · `Copy` · `OK`
- **Focus:** `OK`
- **Opened from:** `Check for updates` on D52, when the newest release
  is later than this build.

## D59
- **Title:** `Newest release`
- **Body:**
  ```
  v0.2.0 is the newest release; this build is dev-3e62f4547e66.
  https://github.com/marrasen/gridterm/releases/tag/v0.2.0
  ```
- **Buttons:** `Open` · `Copy` · `OK`
- **Focus:** `OK`
- **Opened from:** `Check for updates` on D52, when this build is not a
  release and so has no order against one.

---

# Answers that are a line, not a dialog

`Check for updates` on D52 puts these on the bottom row through
`(*app).say`, because there is nothing to fetch and nothing to answer:

- `v0.2.0 is the newest release` — this build is that release.
- `This build is later than the newest release, v0.2.0` — what building
  from `main` gives.
- `Checking for updates…` — while the question is out.

---

# Menu rows that follow from this

So titles and menus keep matching (G6) and the vocabulary holds:

- `Forget This Server…` → `Remove This Server…`
- `Remembered Copies…` → `Saved Copies…`
- The Options row that opens D45 → `Terminal Identity…`
- The status-bar hints and palette names that still say reread,
  remember, forget or make should follow the vocabulary table too.
