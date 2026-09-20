# gridterm: what to demo

A list of everything gridterm does, grouped, with a box against each
one to tick. Then what is planned, so a question from the room has an
answer.

Nothing here is new work. It is the README's feature list and the two
to-do files, rearranged for picking.

## If you only have ten minutes

A suggestion, not a plan. These four are the ones that are hard to get
anywhere else, in an order where each leans on the one before.

1. **A sidebar instead of tabs.** Open a shell, a file browser and a
   tunnel, and show that everything open is one list under the machine
   it is on.
2. **One window working inside another.** Serve one gridterm, connect
   from a second, open a pane on the first machine, and type in it
   from both sides at once.
3. **A pane handed to an agent.** One code, one prompt, and the agent
   works in a pane you are watching and can take back.
4. **Click what a server printed.** `vite dev` on a server, click the
   address it prints, and the browser opens through a tunnel gridterm
   made on the spot.

The rest of this file is the full list.

---

# What works

## The terminal itself

- [ ] **A real terminal.** bash, vim and less all run: alternate
      screen, scroll regions, scrollback, 256 and true colour, bold,
      dim, italic, underline, strikethrough, reverse video, window
      title, cursor shapes, a cursor that blinks when the program asks,
      device reports, bracketed paste and mouse modes.
- [ ] **Wide characters and combining marks.** CJK and emoji take two
      columns; a base character and its marks share one cell.
- [ ] **Box drawing that joins up.** The box and block characters are
      drawn in code at the exact cell size, so a framed TUI has
      unbroken lines. Good side-by-side against another terminal.
- [ ] **A real key pipeline.** Press, release and OS repeat with
      modifiers, correlated with the text they produced, including
      application cursor mode, which vim and readline need.
- [ ] **Mouse, selection and clipboard.** Programs that ask for the
      mouse get it; hold Shift to select anyway. Drag to select,
      Alt+drag for a rectangle. `Ctrl+Shift+C` and `Ctrl+Shift+V`,
      middle click to paste.
- [ ] **Search this pane's scrollback.** A terminal cannot be searched
      — it is a grid a program is still drawing on — so the text is
      handed to the file viewer, which already has the find key, line
      numbers and a selection. `Ctrl+R` in the viewer takes the text
      again, to catch up with a pane that has said more since.

## Drawing, and what it costs

Worth a minute with an engineering audience. The numbers are the point.

- [ ] **Batched rendering.** A full screen of text is one
      `DrawTriangles` call for the backgrounds plus one per atlas page
      for the glyphs — typically two in total, however much text is on
      screen.
- [ ] **Damage tracking.** Writing a cell that already holds the same
      thing does not dirty its row, so an idle screen draws nothing at
      all. Only two things dirty a row on a clock: the sidebar's pulse
      on the active connection, and a blinking cursor. A steady cursor
      dirties nothing.
- [ ] **No cgo on Windows.** `go build` and that is the whole
      toolchain.

## Panes, the sidebar and getting around

- [ ] **A sidebar instead of a row of tabs.** Everything open —
      terminals, file panes, tunnels, transfers — under the machine it
      is on, with this one at the top. Every saved server is listed
      whether or not anything is connected. `Ctrl+Shift+B` hides it.
- [ ] **A plus on every machine** that drops a menu of what can be
      opened there.
- [ ] **Rows that say what they are doing.** A hand-drawn kind icon,
      green for open, brightening and dimming while bytes go past, grey
      once finished. Nothing polls and nothing ticks — it is worked out
      afresh each frame from when the last byte went by.
- [ ] **A cross on any row that can be closed**, under the pointer. Not
      on a machine's own row: closing that takes everything riding on
      it.
- [ ] **Show every pane at once.** `Ctrl+Shift+A` draws every pane on a
      grid, each one live and shrunk by the GPU. Arrows walk them,
      Enter goes, Escape leaves you where you were.
- [ ] **Splits.** Split right, split down, and take a pane back out of
      its split.
- [ ] **A command palette** that finds every command by name, and says
      the key that runs it.
- [ ] **Shortcuts you can change.** "Write a starting keyboard
      shortcuts file" on the Help menu writes `keys.json` with every
      shortcut you have now. Delete a line and that chord goes back to
      the default.

## Machines and connections

- [ ] **Local shells and SSH behind one interface.** Nothing above the
      session layer can tell them apart.
- [ ] **Connections, not just shells.** One SSH connection carries
      several things at once, so a second terminal on a machine is a
      second channel rather than a second login.
- [ ] **Servers are saved** to a JSON file under the OS configuration
      directory. It holds no secret. A list that cannot be read is
      reported and never written over.
- [ ] **One machine reached through another.** A saved server can say
      it sits behind another one; the second connection rides inside a
      channel of the first, so no local port is opened for it.
- [ ] **`Ctrl+Shift+T` opens a terminal like the one you are in**, on
      the same machine and the same shell.
- [ ] **Commands you keep.** Tick "Remember this command" and it gets
      its own line in the palette, run on the machine and in the
      directory it was saved with.

## Security, and how it asks

The bits to say out loud if anyone in the room owns the servers.

- [ ] **Unknown host keys are shown, not assumed.** A fingerprint in a
      dialog, and only an explicit yes records it. A key that changed
      is refused with no button to press.
- [ ] **Secrets are asked for in the window.** Key passphrase, account
      password, one-time code. An unlocked key stays in memory only,
      never written anywhere, so the second connection asks nothing.
- [ ] **Key authentication only between windows**, from an
      `authorized_keys` file in gridterm's own directory, not the one
      in `~/.ssh`. Nothing listens until you ask it to.
- [ ] **A tunnel open to the network asks first**, as does every remote
      forward, because where the far machine binds it is its own
      decision.

## One gridterm working inside another

- [ ] **Take a window over.** Serve one on a port you opt into; another
      window on another machine connects. Its sidebar appears under
      that window's name, and a pane opened there is drawn here.
- [ ] **The window being served keeps working** and says who is in it.
      Closing the connection gives it its screen back.
- [ ] **Join a shell that is already running over there.** Pick a row
      from what that window has open and the pane starts with the
      screen as it stands. Both people see it and either can type. It
      keeps running when this window lets go.
- [ ] **The files of the window taken over**, as SFTP on a channel of
      the same connection — no second login.

## Panes shared with an agent

- [ ] **One code for a share.** Set the panes up across whatever
      machines, as whatever user, put them in a share, and give the
      agent the one code. It reaches no other pane, no connection and
      no file except through the panes you shared.
- [ ] **Tick boxes per pane** for what the agent may do beyond reading
      and typing: restart a closed connection, open another pane on the
      same machine, read only, read above a clear. Every box starts
      off.
- [ ] **Add and remove panes while it works.** A pane added is there on
      the agent's next call; one taken out is gone at once.
- [ ] **The agent can ask you for a password.** A line appears in the
      pane, what you type goes to the program, and the agent is told
      you typed something and never what.
- [ ] **"What the agent typed"** shows the record for the pane you are
      on — what it sent, not what the shell ran. It lives as long as
      the pane.
- [ ] **"Copy the prompt"** puts one prompt on the clipboard that
      carries the code and says how that host adds this window's MCP
      server. Claude Code, Codex, Cursor, or any host that takes JSON.

## Files

- [ ] **A file manager with as many panes as you want**, each on any
      machine — this one, a server, or five of each. Tab and Shift+Tab
      move, Enter descends, Backspace goes up, Space marks.
- [ ] **Moving files is a clipboard, not a direction.** F5 copies, F6
      cuts, F7 pastes into whichever pane you have gone to. A copy can
      be pasted again and again; a cut lands once.
- [ ] **A key bar along the bottom** that says which key does what, and
      clicking a key runs it.
- [ ] **The files inside WSL.** Every distribution installed is a line
      on the plus for this machine.
- [ ] **A reader for a file, without a shell.** F3 opens, F4 tails. It
      works the way `less` does: "/" to search, "n" and "N", ":" to go
      to a line, `Ctrl+H` for a hex dump.
- [ ] **A tailed file** is asked about three times a second and stays
      at its end; scroll back and it leaves you where you put yourself.
- [ ] **A strip beside the file**, where a code editor puts its
      minimap: the shape of the whole file at once, the pane's place in
      it as a box, and a click to go there. `Ctrl+M` turns it off.
- [ ] **A JSON log read as a log.** A file of JSON lines lays itself
      out in columns — time, level, message, then the rest of the
      fields — with the level coloured for what it means, and a second
      strip column marking which parts of the file went wrong. `Ctrl+J`
      puts the raw JSON back. Good with a real log from a server.
- [ ] **Code is coloured** by what the file is called, markdown gets
      its headings, and a picture file shows the picture.
- [ ] **File work in the background.** Copying, moving and deleting, on
      one machine or between two, with progress and a way to stop it. A
      name already there is asked about and never decided alone.
- [ ] **A file is written beside its name and moved onto it at the
      end**, so what is at that name is either the old file or the
      whole of the new one, never half of either.
- [ ] **Files dragged into a pane** land in the directory that shell is
      in, and nothing is typed. A pane on a server has the file copied
      there first.

## What a program can say, and what happens when it does

This is the group with the most "oh, nice" in it.

- [ ] **Links in the output.** Ctrl and a click follows a link a
      program declared, or an address written out in the text.
- [ ] **File paths in the output.** A compiler's `vt/image.go:42:8`
      opens the viewer at line 42; a directory opens the browser. The
      path is checked against the disk first, so a run of characters
      naming nothing is just text.
- [ ] **It works on a server too.** The far machine is asked over the
      connection the window already has, and the answer is kept, so a
      path lights up a moment after the pointer reaches it.
- [ ] **What a dev server printed is clickable.** `localhost:5173` with
      no scheme counts, which is what vite prints. **From a pane on a
      server it opens a tunnel first** and sends the browser to this
      end of it — that server's localhost is not yours.
- [ ] **Where it goes is written along the bottom row** while you hold
      ctrl, because a program can put any address under any words.
- [ ] **A picture in the output.** OSC 1337: the pane holds it on the
      line it landed on and it scrolls with the text. It travels to a
      window watching the pane.
- [ ] **A message from a program** (OSC 9) goes on the pane's row, into
      the window's log, and up as a Windows notification.
- [ ] **How far along it is** (OSC 9;4) reads on the row as "42%",
      "working", "failed at 70%".
- [ ] **What colour is the theme?** OSC 4, 10 and 11 are answered, so a
      program can pick something that shows. Setting a colour is
      ignored — the colours are the window's.

## Shell integration

- [ ] **gridterm sets the shell up itself.** As a shell starts it types
      one line in and clears the pane. Nothing to install, no profile
      to edit. On for this machine; a tick per server.
- [ ] **What it buys:** relative paths become clickable, dropped files
      know where to land, and an agent can read the last command's
      output and its exit code instead of a rectangle of the screen.
- [ ] **Each shell in its own words.** PowerShell wraps the prompt you
      already have; the Command Prompt says where it is as a plain path
      because it cannot build a URL.

## Tunnels

- [ ] **Three kinds.** A port here for a service over there, a port
      over there for one here, or a SOCKS5 proxy that reaches whatever
      it is asked for as the far machine sees it.
- [ ] **The row says what it is carrying:** how many streams, how fast,
      how many failed.
- [ ] **Click a tunnel to open its pane.** What it has been doing, a
      button to close it, and a button to watch what goes through it.
- [ ] **Watching is off until asked for**, because a tunnel carries
      whatever it carries. Show an HTTP request and response going past
      in readable text.
- [ ] **Tunnels you keep** get a line on the palette, the way commands
      do.

---

# Planned

From `TODO.md` and `MARCUS_TODO.md`. Grouped by how settled each one
is, so a question in the room gets a straight answer.

## Next, in order

1. **The file viewer.** The minimap and the JSON log viewer are in.
   What is left of it: the scrollback viewer keeping the terminal's
   colours, and filtering a log by level and by field, which is most
   of what makes the bugreport viewer useful.
2. **Pasting a picture into a terminal** — done on Windows, the rest of
   the platforms to go.

## Waiting on a decision

- **A context menu.** Everything it needs is already in the toolkit and
  the plan is written down. Six questions are open: whether right click
  in a program that owns the mouse needs Shift, whether a sidebar row
  gets a menu, which pane the menu acts on, what nine lines go on a
  terminal's menu, whether the file browser gets one, and whether
  Shift+F10 is worth binding.

## Asked for, not started

The big ones from Marcus's own list.

- **Serve over the Teilen relay.** A one-time share through the
  in-house relay: a stream key and an encryption key, end-to-end, no
  authorized keys and no open port.
- **Run as a backend.** gridterm on a server with no window, so you
  close the client on one machine and carry on from another.
- **Copy with colours**, and "copy as an image" for pasting into a
  chat.
- **Archives in the file browser**, browsed as if they were folders.
- **Full screen** — the app with no sidebar and no menu bar.
- **A clipboard history** to paste from.
- **Agents opening tunnels** over SSH, behind a tick box.
- **An Open menu of its own**, listing saved servers without "Connect
  to" in front of each one.
- **Dropdown fields that look like dropdowns**, with an icon that opens
  a picker.
- **A mobile app.** Ebiten supports it; getting the keyboard right is
  the challenge. After Linux.

## Open work, by subject

Smaller and already written up.

- **Hyperlinks.** A path with a space in it reads as two words. The
  column in `file:42:8` is thrown away. A server's shell setup covers
  bash and zsh and would trip an unusual login shell.
- **Pictures.** Kitty and sixel are not read — OSC 1337 is the one.
  A picture cannot be copied or selected. Four megabytes of picture
  travel with a screen and the rest are left out. A size in pixels is
  guessed at eight by sixteen.
- **Notifications.** Only Windows gets a pop-up, and it is a
  notification-area balloon rather than a WinRT toast, so no buttons
  and no click back into the window. Progress has no bar; the taskbar
  button is where Windows Terminal puts it.
- **Tunnels.** Watching writes bytes as text, which suits a web server
  and not a binary protocol — the hex view the file viewer already has
  is what that wants. A tunnel is not watched from a window that took
  this one over.
- **The log pane.** Two thousand lines and nothing older, nothing
  searchable, and it goes when the window does.
- **Pictures, pasting and dropping.** Only Windows reads a picture off
  the clipboard. Nothing clears the pictures left on a machine reached
  by SSH.
- **Panes and the sidebar.** A pane on a taken-over window cannot be
  reconnected. The sidebar's order goes stale while it is shut.
- **Release.** Binaries for Windows and Linux, macOS if it is easy, and
  two version numbers rather than one.

## Known gaps

Honest limits, in case they come up.

- **Colour emoji do not render.** The font library cannot read the
  bitmap tables they use. Emoji ZWJ sequences show their first glyph.
- **Blink is parsed and ignored.**
- **OSC 52 clipboard writes are parsed but not applied.** Reads are
  deliberately never answered: replying would let any program that can
  write to the terminal read your clipboard.
- **An OSC payload other than a picture is capped at a kilobyte** by
  the parser, so a very long clipboard write is cut short.
- **A click faster than one frame is missed.** ebiten reports the mouse
  as polled state. No human manages it; a test harness does.
- **File panes share the width evenly** and the split cannot be
  dragged.
- **A watched pane is not resized to suit the watcher.**
- **`-e` splits its argument on spaces**, with no quoting.
