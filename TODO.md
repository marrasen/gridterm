# To do

Things Marcus has asked for that are not done yet. Newest first within
each group. A line goes when the work is in and reviewed.

## Shells on Windows

Asked for a while back and missed: it was never written down here, and
nothing in the code offers it. Raised again on 2026-09-16.

1. **Choose which shell a new pane runs on Windows.** Today a pane runs
   whatever `COMSPEC` names, falling back to `cmd.exe`, decided by
   `defaultShell` in session/local.go. The only way to get another is
   the `-e` flag, which sets one command for the whole window. The user
   should be able to open a pane on cmd.exe, PowerShell, pwsh or a WSL
   distribution.
   - **What is there to offer.** Found by looking rather than guessed:
     `cmd.exe` from `COMSPEC`, `powershell.exe` and `pwsh.exe` from the
     PATH, and one line per distribution from `wsl.exe -l -q`. A shell
     that is not installed is not offered. The looking happens once and
     off the goroutine that draws, because `wsl.exe` is slow to answer.
   - **Where the choice goes.** The plus on this machine's row, which
     already offers "Terminal", gets a line per shell under it; the
     File menu gets the same. Choosing one opens a pane on it.
     "Terminal" keeps opening the remembered shell, so the common case
     stays one click.
   - **What is remembered.** The last shell chosen, in the settings
     file, the way the agent host is (`stored.AgentHost` and
     `PutAgentHost` in settings/settings.go are the shape to follow). A
     remembered shell that has since gone falls back to the default and
     says so once.
   - **What the session layer needs.** Nothing: `LocalConfig.Command`
     already carries the argv. WSL is the one that needs thought,
     because a Windows working directory has to be translated for it,
     and a pane opened in `C:\Workspace` should land in
     `/mnt/c/Workspace` rather than the distribution's home.
   - **Tests.** Start at the menu line, not at `defaultShell`: choose
     PowerShell from the plus, and the pane's session is started with
     that argv; the pick is remembered and the next "Terminal" uses it;
     a shell that is not installed is not on the menu; a remembered
     shell that has gone falls back and says so.

## Known gaps worth revisiting

- The cursor keeps blinking while the window is in the background.
  Nothing reads `ebiten.IsFocused`, and most terminals either stop the
  blink or draw the cursor hollow once the window loses focus.
- A watcher taking a screen over sees the far end's default cursor.
  `vt.Repaint` puts the cursor back where the program had it but carries
  neither its shape nor its blink, so `DECSCUSR` is lost over the wire.
