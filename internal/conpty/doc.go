// Package conpty runs a program in a Windows pseudoconsole: a console
// host that turns what the program does to its console into VT, which
// a terminal reads from a pipe, and turns VT written to another pipe
// into the program's input. It is empty elsewhere.
//
// It uses the ConPTY kakel carries, OpenConsole, where it can, and the
// one Windows has otherwise. The two are called the same way. Windows'
// own, in the console host of Windows 11 24H2, keeps a screen of its
// own and repaints it on a timer, apart from the program's writes: a
// frame of a full-screen animation reaches the terminal cut between two
// repaints, top half new and bottom half old, and the marks of a
// synchronized update arrive wherever they fall among the repaints.
// OpenConsole passes the program's output through as it comes, marks
// and all, so a frame arrives whole between its marks.
package conpty
