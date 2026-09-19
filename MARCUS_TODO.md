# Marcus's notes

Whatever Marcus thinks of, written down here as it comes. Nothing is
sorted and nothing is answered: this is the inbox.

A note is read, and then it goes. What is still to do moves to TODO.md
in the words the work needs, what is done goes with the commit that did
it, and what needs an answer from Marcus goes to the top of TODO.md.

# Copy with colors

It would be cool if I could copy the contents of a pane and keep the colors. If we could copy it as "rich text",
and also have an option to "copy as image", if I want to paste into a chat or something. I don't know if "rich text" 
is the best to use though.

# File viewer
- Select and copy text
- Color code JSON

# Forms
- Fields using dropdown should have an icon making it obvious, clicking it opens a picker
- Select text using shift, copy, cut

# Show all panes
File browser don't always render when zooming out

# Icons
- Remotes: The "serving marcusj@" row in the sidebar has the terminal icon, it should have a remote icon instead, it's not a terminal
- View file should have it's on icon, and maybe Follow
- Copy and Move could use own icons as well

# Tunnel and SOCKS proxy over remote connection
- Add support for tunnels and SOCKS proxy over a remote connection

# Serve over Teilen Relay

Look at the "teilen" project (G:\Workspace\teilen). It's our in-house relay service. Serving gridterm using this should mean:
- User configures a relay server (no default) and an optional proxy server in gridterm on both machines
- A relay share is a one time share, it can't be automatically started again
- When user clicks share in the host, a "stream key" and an encryption key is shown and copied to the clipboard
- In the "Connect to another window", the user switches to "Teilen relay" and pastes the two keys
- Both gridterms connects to the relay server and encrypts the data the same way teilen does, end-to-end
- We re-use our wire format, but don't require any authorized keys to serve
