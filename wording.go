package main

// The words this window says, in one place.
//
// Every button title, field label and dialog title is a constant here
// rather than a string literal where it is used. Two reasons, and the
// second is the one that costs real time:
//
// A word said in two places drifts the first time one of them is
// reworded. That is how the MCP server came to tell an agent to tick a
// box by a name the dialog no longer used: two copies, one edited.
//
// And a test has to find a button before it can press one. Given a
// literal, the only handle is the wording, so every test that presses
// Cancel is coupled to the word "Cancel" without caring what it says.
// Rewording then breaks a hundred tests that were never about wording.
// Given a constant, the handle is the constant and rewording costs
// nothing.
//
// See WORDING.md for how these are chosen.

// The buttons. Rule 5 in WORDING.md fixes most of these to one verb from
// a small set, so most dialogs draw from the first group.
const (
	btnAdd     = "Add"
	btnChange  = "Change"
	btnOK      = "OK"
	btnShow    = "Show"
	btnType    = "Type"
	btnCancel  = "Cancel"
	btnClose   = "Close"
	btnRetry   = "Retry"
	btnWait    = "Wait"
	btnSave    = "Save"
	btnCreate  = "Create"
	btnDelete  = "Delete"
	btnRemove  = "Remove"
	btnReplace = "Replace"
	btnSkip    = "Skip"
	btnOpen    = "Open"
	btnRun     = "Run"
	btnConnect = "Connect"
)

// The buttons that say something a single verb cannot: an action with an
// object, or a second answer to the same question.
const (
	btnCheckUpdates  = "Check for updates"
	btnAddKey        = "Add key"
	btnAddNote       = "Add note"
	btnAddSecret     = "Add secret"
	btnCopy          = "Copy"
	btnCopyPrompt    = "Copy prompt"
	btnCopyPublicKey = "Copy public key"
	btnDontAskAgain  = "Don't ask again"
	btnExit          = "Exit"
	btnExport        = "Export"
	btnGenerate      = "Generate"
	btnGo            = "Go"
	btnNotNow        = "Not now"
	btnOpenLink      = "Open link"
	btnReconnect     = "Reconnect"
	btnRemoveKey     = "Remove key"
	btnRemovePane    = "Remove pane"
	btnRename        = "Rename"
	btnRepeat        = "Repeat"
	btnReplaceAll    = "Replace all"
	btnServe         = "Serve"
	btnSetup         = "Setup"
	btnSignIn        = "Sign in"
	btnSkipAll       = "Skip all"
	btnStopServing   = "Stop serving"
	btnStopSharing   = "Stop sharing"
	btnUnlock        = "Unlock"
	btnWriteSkill    = "Write skill"
)

// The field labels.
const (
	fldAgent        = "Agent"
	fldCommand      = "Command"
	fldComment      = "Comment"
	fldConfirmPass  = "Confirm passphrase"
	fldDirection    = "Direction"
	fldDirectory    = "Directory"
	fldFile         = "File"
	fldFolders      = "Folders"
	fldFor          = "For"
	fldForwardAgent = "Forward SSH agent"
	fldForwardTo    = "Forward to"
	fldHost         = "Host"
	fldJumpHost     = "Jump host"
	fldKeyFile      = "Key file"
	fldListenOn     = "Listen on"
	fldName         = "Name"
	fldNewNote      = "New note"
	fldNewSecret    = "New secret"
	fldNote         = "Note"
	fldPassphrase   = "Passphrase"
	fldPassword     = "Password"
	fldPath         = "Path"
	fldPort         = "Port"
	fldSecret       = "Secret"
	fldServer       = "Server"
	fldShellSetup   = "Shell setup"
	fldTermProgram  = "TERM_PROGRAM"
	fldType         = "Type"
)

// The tick boxes that save what a dialog was filled in with. One wording
// for all three, because they are one idea.
const (
	fldSaveCommand = "Save this command"
	fldSaveCopy    = "Save this copy"
	fldSaveTunnel  = "Save this tunnel"
	fldShowSecret  = "Show the secret"
)

// The tick box on the new key dialog, which is not one of those three:
// it does not save what was typed, it says not to type one at all.
//
// The label is the short name and the field's hint says the rest, so
// the row beside it stays a row rather than a sentence.
const fldGeneratePass = "Generate passphrase"

// The dialog titles that are a fixed phrase. A title built from a name
// is made where it is used, out of the parts below.
const (
	dlgAuthentication   = "Authentication"
	dlgConnectServer    = "Connect to Server"
	dlgConnectWindow    = "Connect to Window"
	dlgConnectionLost   = "Connection lost"
	dlgExit             = "Exit gridterm?"
	dlgGoTo             = "Go to Directory"
	dlgKeyCreated       = "Key created"
	dlgNewDirectory     = "New Directory"
	dlgNewestRelease    = "Newest release"
	dlgPassword         = "Password"
	dlgReplaceSkill     = "Replace existing skill?"
	dlgServeWindow      = "Serve This Window"
	dlgServingWindow    = "Serving this window"
	dlgSecretsWritten   = "Secrets written"
	dlgSkillWritten     = "Skill written"
	dlgTermProgram      = "Terminal Identity"
	dlgUnknownHostKey   = "Unknown host key"
	dlgUpdateAvailable  = "Update available"
	dlgUnlockKey        = "Unlock Private Key"
	dlgWaitingForServer = "Waiting for server"
)

// The halves a title built from a name is made of, so the wording is
// still in one place when the name is not.
const (
	dlgAddKey            = "Add "
	dlgAlreadyConnecting = "Already connecting to "
	dlgDelete            = "Delete "
	dlgExportTo          = "Export every secret to "
	dlgSecretsOn         = "Create the secrets on "
	dlgOpenToNetwork     = "Open "
	dlgRemove            = "Remove "
	dlgRename            = "Rename "
	dlgReplaceFile       = "Replace "
	dlgRunCommandOn      = "Run Command on "
	dlgSetUp             = "Set up "
	dlgSocksVia          = "SOCKS proxy via "
	dlgTunnelVia         = "Tunnel via "
)
