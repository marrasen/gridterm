// Package quiet starts helper programs without a window of their own.
//
// kakel has no console on Windows, so a console program it starts, such
// as wsl.exe listing the distributions or cmd opening a link, is given a
// console window of its own, which flashes up and goes. Hide asks for
// none.
package quiet
