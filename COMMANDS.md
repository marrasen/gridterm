# Everything in Ctrl+Shift+K — rewrite

A change sheet against `Everything in Ctrl+Shift+K.md`, matched on the
command IDs. No ID changes. Each table gives the title and the
`Also found under` words as they should read afterwards, in full. Keys
and the Switch column are unchanged and not repeated. Notes stay as
they are unless an instruction says otherwise.

---

# Rules

The house rules and vocabulary from the dialog sheet apply here
unchanged; both sets are in [WORDING.md](WORDING.md). Four more, specific
to a command title:

1. **A title is a name, in Title Case, two to four words, verb first:**
   `Close Pane`, `Reload Themes`, `Open Tunnel…`. No articles, no
   "this pane's", no commas, no clauses.
2. **A title stands alone.** It is read in a flat list with no menu or
   header around it, and it heads an error notice. So `Split Right`,
   never bare `Right`. A menu row may be shorter than the title only
   where its header already says the missing word; those rows are
   listed at the end.
3. **`…` means the command asks something before it acts.** A command
   that shows something and asks nothing has none: `About gridterm`,
   `File Locations`, `Typing History`. This is the classic rule, it is
   narrower than "opens a dialog", and it settles `server.connect` and
   `server.add`: both ask, both get `…`.
4. **Every word a title loses goes into `Also found under`**, along
   with the ordinary synonyms and both spellings. Whoever learned the
   old wording still finds the command by typing it.

A toggle is named for the thing it shows, and the tick says the rest.
"Show or hide" and "on or off" never appear in a title.

---

# Global instructions

- **P1. Error heading.** `(*app).reporting` heads the notice with
  `<Title> failed`, with the `…` stripped: `Open Tunnel failed`,
  `Copy failed`, `Reload Themes failed`. Every title below reads
  correctly in that frame, so no command needs a second string.
- **P2. The palette draws the tick** for a command whose `On` is true,
  as the menus do. Without it `Show Sidebar` cannot say which way it
  will go.
- **P3. Title Case for names, sentence case for sentences.** A dialog
  title that names a thing follows its command: `File Locations`,
  `Saved Copies`, `New SSH Key`. A dialog title that is a sentence or a
  question stays as the dialog sheet has it: `Remove <name>?`,
  `Already connecting to <name>`. The dialog sheet wrote the names in
  sentence case; the shared constants below correct that.
- **P4. A dialog opened by a command is titled with the command's
  title, minus the `…`.** Where the dialog names its target it may
  drop the verb: `Tunnel via <host>`, `Run Command on <where>`.
- **P5. The `…` is part of the registered title, never part of a
  shared constant.** Constants hold the bare name; registration adds
  the `…` where rule 3 calls for it, and only there.

---

# 1. Text

| ID | Title | Also found under |
|---|---|---|
| `edit.copy` | Copy | |
| `edit.paste` | Paste | |
| `edit.pasteImage` | Paste Image as File… | picture, screenshot, path |
| `pane.scrollback` | Find in Scrollback | search, history, buffer, save, view, open |
| `view.scrollUp` | Scroll Page Up | back, scrollback |
| `view.scrollDown` | Scroll Page Down | forward, scrollback |

**Instructions:**

- **pane.scrollback** — open the viewer with its find prompt already
  active, so the title is true the moment the command runs.
- **edit.pasteImage** — keep the `…` only if it asks where to write the
  file. If it writes and types the path unasked, drop it.

# 2. What is drawn

| ID | Title | Also found under |
|---|---|---|
| `font.increase` | Increase Font Size | zoom in, bigger, larger |
| `font.decrease` | Decrease Font Size | zoom out, smaller |
| `font.reset` | Reset Font Size | zoom, default, actual size |
| `view.theme` | Choose Theme… | colour, color, colors, scheme |
| `view.themesReload` | Reload Themes | colour, color, colors, reread |
| `view.themesStart` | New Theme File | colour, color, colors, write, create, edit |
| `view.fullScreen` | Full Screen | fill, maximise, maximize, hide the sidebar |
| `pane.titles` | Show Pane Titles | hide, toggle, names, line |
| `sidebar.toggle` | Show Sidebar | hide, toggle, connections, panel |

Built at run time: `Font: <name>` stays as it is.

**Instructions:**

- **view.themesStart** — no `…`: it writes the file and reports, it
  asks nothing.
- The three font size titles were already what every other program
  calls them. Only the capitals change.

# 3. Panes

| ID | Title | Also found under |
|---|---|---|
| `pane.open` | New Terminal | pane, shell |
| `shell.default` | New Terminal, Default Shell | pane |
| `conn.terminal` | New Terminal Here | like this one, same shell, same server, duplicate, clone |
| `pane.splitRight` | Split Right… | vertical |
| `pane.splitDown` | Split Down… | horizontal |
| `pane.popOut` | Pop Out Pane | unsplit, detach, take out of its split |
| `pane.close` | Close Pane | |
| `view.switcher` | All Panes… | show every pane, switcher, overview, grid |
| `pane.nextInSidebar` | Next Pane | down the sidebar |
| `pane.previousInSidebar` | Previous Pane | up the sidebar |
| `pane.next` | Next Recent Pane | last used, used before, mru, switch |
| `pane.previous` | Previous Recent Pane | last used, back the other way, mru, switch |
| `sidebar.focus` | Focus Sidebar | go to the connections, panel |
| `sidebar.closeRow` | Close Selected Row | close this connection, sidebar |

Built at run time: `New pane on <shell>` → `New Terminal: <shell>`, so
the shells sort directly under `New Terminal`.

**Instructions:**

- **pane.splitRight, pane.splitDown** — `…` because D54 asks before
  anything is split, and cancelling it leaves the pane whole. If the
  half opens first and D54 only fills it, drop the `…`.
- **pane.next / pane.nextInSidebar** — the plain names go to the
  sidebar pair, because that is the order on screen. The `Ctrl+Tab`
  pair carries the one qualifying word, `Recent`. The four now sort
  into two adjacent pairs.

# 4. This machine, and the one a pane is on

| ID | Title | Also found under |
|---|---|---|
| `conn.command` | Run Command… | here, program, execute |
| `conn.tunnel` | Open Tunnel… | forward, port, local, remote |
| `conn.socks` | Open SOCKS Proxy… | tunnel, dynamic |
| `conn.files` | Browse Files Here | file browser, sftp, folder, directory |
| `files.goTo` | Go to Directory… | folder, path, cd, drive |
| `conn.log` | Connection Log | how this was reached, route, hops |
| `conn.disconnect` | Disconnect | close the connection, machine, server, log out |
| `conn.clearFinished` | Clear Finished | connections, rows, sidebar, ended |
| `shell.setup` | Shell Setup | shell integration, working directory, osc 7, on, off |
| `shell.termProgram` | Terminal Identity… | term_program, compatibility, pictures, images, calls itself |

**Instructions:**

- `Here` stays on exactly two titles, `New Terminal Here` and
  `Browse Files Here`, because without it both read as local. The
  others open a dialog that names the machine in its title.

# 5. Saved servers

| ID | Title | Also found under |
|---|---|---|
| `server.connect` | Connect to Server… | ssh, host, machine |
| `server.add` | Add Server… | new, save |
| `server.editThis` | Edit This Server… | |
| `server.forget` | Remove This Server… | forget, delete |
| `server.reload` | Reload Server List | reread |
| `sshkey.make` | New SSH Key… | make, create, generate, keygen, ed25519 |
| `sshkey.lock` | Lock SSH Keys | forget unlocked keys, passphrase, agent, try again |

Built at run time:

| ID pattern | Title |
|---|---|
| `server.open.<name>` | `Connect to <name>` |
| `server.edit.<name>` | `Edit <name>…` |
| `conn.terminal.<name>` | `New Terminal on <name>` |
| `conn.files.<name>` | `Browse Files on <name>` |
| `conn.files.<name>.<n>` | `Browse <folder> on <name>` |
| `conn.saved.<n>` | `Run <command> on <name>` |
| `conn.savedtunnel.<n>` | `Open Tunnel <tunnel> via <name>` |

Fallback for a tunnel that will not parse:
`Open <kind> Tunnel via <name>`.

**Instructions:**

- In the run-time `Also found under` words, add `saved` wherever
  `remembered` appears, and keep both.

# 6. Serving and other windows

| ID | Title | Also found under |
|---|---|---|
| `serve.window` | Serve This Window… | share, listen, remote |
| `serve.attach` | Connect to Window… | another, attach, take over, remote, share panes |

# 7. Agents

| ID | Title | Also found under |
|---|---|---|
| `agent.hand` | Share Pane with Agent… | hand over, add this pane to the share |
| `agent.take` | Stop Sharing Pane | take this pane out of the share, remove, unshare |
| `agent.share` | Agent Share | show the share, code, prompt, skill, setup |
| `agent.typed` | Typing History | what the agent typed, input, sent |

**Instructions:**

- **agent.hand** — the mismatch is settled by dropping the second
  wording. `Share Pane with Agent…` is true whether or not a share is
  open, so the menu row stops changing and `shareItem` can go.
- **agent.share, agent.typed** — no `…`; both show and ask nothing.
  Registration stops adding it (P5).

# 8. Files and copies

| ID | Title | Also found under |
|---|---|---|
| `files.copies` | Saved Copies… | remembered, file, again, repeat |

# 9. The window and the program

| ID | Title | Also found under |
|---|---|---|
| `palette.open` | All Commands… | show, palette, search |
| `menu.open` | Focus Menu Bar | show |
| `help.shortcuts` | Shortcuts and Commands | keys, keyboard, help |
| `help.files` | File Locations | where gridterm keeps its files, settings, config, folder, portable |
| `view.log` | Window Log | show what the window has logged, debug, errors, what went wrong |
| `shortcuts.write` | New Shortcuts File | write, starting, keyboard, create |
| `shortcuts.reload` | Reload Shortcuts | keyboard, reread |
| `app.about` | About gridterm | version |
| `app.exit` | Exit | quit, close this window |

**Instructions:**

- **help.shortcuts** — your title was right and my menu sheet was
  wrong. It said `Keys and Commands` a few lines after ruling that
  Keys means SSH keys and Shortcuts means the keyboard. The Help row
  becomes `Shortcuts and Commands`.
- **shortcuts.reload** — split `keysReloadTitle`. The command keeps
  it. D41 becomes the status line text `Shortcuts reloaded`, its own
  string. D42's body quotes the menu path, `Options › Reload ›
  Shortcuts`, not the title.

---

# Titles that are shared with something else

| Constant | Becomes | Note |
|---|---|---|
| `helpTitle` | `Shortcuts and Commands` | D53 follows |
| `filesTitle` | `File Locations` | D43 follows |
| `logTitle` | `Window Log` | the log pane's name follows |
| `keysTitle` | `New Shortcuts File` | |
| `keysReloadTitle` | `Reload Shortcuts` | command only; see `shortcuts.reload` |
| `copiesTitle` | `Saved Copies` | D37 and D38 follow; `…` at registration |
| `typedTitle` | `Typing History` | D28 follows; no `…` |
| `switcherTitle` | `All Panes` | `…` at registration |
| `scrollbackTitle` | `Find in Scrollback` | |
| `carryOwnTitle` | split in two | the D43 button is `Make Portable`, D44's title is `Made Portable` |
| `makeKeyTitle` | `New SSH Key` | D13 follows; `…` at registration |
| `paneBoxesTitle` | `Agent Permissions` | D22 |
| `shareTitle` | `Agent Share` | D23, and now `agent.share` too |
| `serveAgainTitle` | `Resume serving?` | a question, so sentence case |

Dialog titles from the dialog sheet that P3 moves to Title Case, and
that no constant above already covers: D01 `Connect to Server`, D02
`Connect to Window`, D05 `Unlock Private Key`, D10 `Connection Log —
<name>`, D11 `Add Server`, D19 `Serve This Window`, D30 `New
Directory`, D33 `Go to Directory`, D39 `Choose Theme`, D45 `Terminal
Identity`, D52 `About gridterm`.

---

# Menu rows shorter than their title

Only where the header above the row already says the missing word.
Every other menu row is the registered title.

The shells are not in the table. One command is registered per shell
found, so their ids are made at run time and there is nothing fixed to
list. Their rows follow the same rule: `New Terminal: Command Prompt` in
the palette, `Command Prompt` under `New Terminal In` on the File menu
and under `Terminal` on a machine's plus menu.

| ID | Title | Header | Row |
|---|---|---|---|
| `pane.splitRight` | Split Right… | Split | Right… |
| `pane.splitDown` | Split Down… | Split | Down… |
| `pane.popOut` | Pop Out Pane | Split | Pop Out |
| `pane.nextInSidebar` | Next Pane | Go To | Next |
| `pane.previousInSidebar` | Previous Pane | Go To | Previous |
| `pane.next` | Next Recent Pane | Go To | Next Recent |
| `pane.previous` | Previous Recent Pane | Go To | Previous Recent |
| `sidebar.focus` | Focus Sidebar | Go To | Sidebar |
| `font.increase` | Increase Font Size | Font | Larger |
| `font.decrease` | Decrease Font Size | Font | Smaller |
| `font.reset` | Reset Font Size | Font | Reset |
| `view.scrollUp` | Scroll Page Up | Scrollback | Page Up |
| `view.scrollDown` | Scroll Page Down | Scrollback | Page Down |
| `sidebar.toggle` | Show Sidebar | — *(View menu)* | Sidebar |
| `pane.titles` | Show Pane Titles | — *(View menu)* | Pane Titles |
| `conn.terminal` | New Terminal Here | Open Here | Terminal |
| `conn.command` | Run Command… | Open Here | Command… |
| `conn.files` | Browse Files Here | Open Here | Files |
| `conn.tunnel` | Open Tunnel… | Open Here | Tunnel… |
| `conn.socks` | Open SOCKS Proxy… | Open Here | SOCKS Proxy… |
| `pane.close` | Close Pane | Close | Pane |
| `conn.disconnect` | Disconnect | Close | Machine |
| `view.themesReload` | Reload Themes | Reload | Themes |
| `shortcuts.reload` | Reload Shortcuts | Reload | Shortcuts |
| `server.reload` | Reload Server List | Reload | Server List |
| `shell.default` | New Terminal, Default Shell | New Terminal In | Default shell |
| `sshkey.make` | New SSH Key… | SSH Keys | New Key… |
| `sshkey.lock` | Lock SSH Keys | SSH Keys | Lock Keys |
| `view.theme` | Choose Theme… | — *(Options menu)* | Theme… |
| `agent.hand` | Share Pane with Agent… | Agent | Share Pane… |
| `agent.take` | Stop Sharing Pane | Agent | Stop Sharing Pane |
| `agent.share` | Agent Share | Agent | Show Share |
| `serve.window` | Serve This Window… | Windows | Serve This One… |
| `serve.attach` | Connect to Window… | Windows | Connect to Another… |

Two rows here correct my menu sheet: `Go To` loses `Last Used,
Reversed` for the `Recent` pair, and the Agent row `Remove Pane`
becomes `Stop Sharing Pane` so the menu, the palette and D22's button
all use one phrase. D22's second button becomes `Stop sharing pane`.
