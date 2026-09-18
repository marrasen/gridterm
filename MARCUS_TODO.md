# Marcus's notes

Whatever Marcus thinks of, written down here as it comes. Nothing is
sorted and nothing is answered: this is the inbox.

A note is read, and then it goes. What is still to do moves to TODO.md
in the words the work needs, what is done goes with the commit that did
it, and what needs an answer from Marcus goes to the top of TODO.md.

# Throw out client

If a remote is connected and I stop sharing, the client throws a generic network error dialog.
Provide a reason to the client so it can show "Remote stopped the connection" (or something better).

This way, if it is an actual network error, we can show "Reconnect?" in the error dialog.

# Closing client kills hosts window

I started a pane on the host from the client, when I exited the client, the pane on the host disappeared.
This is probably related to a problem with host panes launched from a client doesn't show up on the host. I don't
think we have fixed that yet.

# Remember file copy

A file copy can be repeated after they have completed, we noted that copying a log and then copying it again is
a reasonable process. I would like to add a "Remember"-button to the file copy, so that it is saved over a restart.

That way I can copy that log file again the next time without even opening a file browser pane.

# More on Remotes

A remote pane shows up as gray on the client until I click it, doesn't seem like it live updates.
The phrase "Take over" is from my first request that the client took over a pane completely, but now we actually
share panes instead, they are still available on the host and the client at the same time. 

The hosts wording is "Serve this window...", the clients "Take over" could be maybe "Share host panes"? Or if you come
up with something better.

# Bugs
- "Show every pane..." breaks when changing the window size
