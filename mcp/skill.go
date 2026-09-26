package mcp

import "fmt"

// Skill is the skill that tells an agent what gridterm is and how to
// work in the panes the user shared, with setup the words for how its
// own agent program reaches the server.
//
// One text for both of gridterm's windows, so a skill one wrote reads
// as unedited to the other.
func Skill(setup string) string {
	return fmt.Sprintf(`---
name: gridterm
description: Work in the terminal panes the user shared with you in gridterm, through its MCP server
---

# Working in gridterm panes

gridterm is a terminal on the user's machine. The user puts panes into a share -- on whatever
machines, as whatever user -- and gives you one code for the whole share. You work in those panes
through gridterm's MCP server, and the user watches everything you do.

## Reaching the server

The server runs on the user's machine, on standard input and output (stdio), because the port
inside a session code is on the loopback address. If you do not have gridterm's tools, it has not
been added here yet.

%s

## Getting the panes

The user starts a share in gridterm, adds panes to it, and gets one session code for the whole
share. Ask the user for the code if you have not been given one. Call use_session_code with it
before anything else. The answer lists the panes, and every other tool takes a pane's name.

The share is not a fixed set. The user adds panes and takes them out while you work, so call
list_panes when you want to know what you have now.

## Working in a pane

%s

## Rules

%s
`, setup, Workflow, Rules)
}
