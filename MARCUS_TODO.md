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

# Panes behave strange when remote connecting

1. I start a new gridterm instance, it has one pane, "Command Prompt"
2. I start serving
3. Connect from a client
4. The host now have 3 panes in the sidebar, 2 x "Command Prompt" and on 
   "serving marcusj@m-station". Clicking the "serving" one does nothing
5. The client now has 3 panes that belongs to the host. One has a green 
   icon and is active, it says "C:\WINDOWS\system32\cmd.exe" (not the same
   title as on the host), and two called "Command Prompt" that are gray that
   become green when I click them. The last one is a duplicate of the first one
6. Closing them on the client does nothing

Also, I really don't like the new pulsating animation, it pulses irregularly
and is too loud, it actually makes me get a headache, it needs to be much more subtle.
