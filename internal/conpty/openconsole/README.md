# OpenConsole

`conpty.dll` and `OpenConsole.exe` here are Microsoft's ConPTY, the
pseudoconsole Windows Terminal runs its shells in, for x64. They come
unchanged from the NuGet package
[Microsoft.Windows.Console.ConPTY](https://www.nuget.org/packages/Microsoft.Windows.Console.ConPTY)
1.25.260930003 (`runtimes/win-x64/native/conpty.dll` and
`build/native/runtimes/x64/OpenConsole.exe`), built from
[microsoft/terminal](https://github.com/microsoft/terminal).

kakel runs its local panes on Windows through them rather than through
the ConPTY Windows itself has, which repaints the screen on a timer and,
under a full-screen animation, paints frames torn across. See DESIGN.md,
"What ConPTY passes on".

To update them, take both files from the same version of the package,
and change `bundleVersion` in `../bundle_windows_amd64.go` to match.

    MIT License

    Copyright (c) Microsoft Corporation. All rights reserved.

    Permission is hereby granted, free of charge, to any person obtaining a copy
    of this software and associated documentation files (the "Software"), to deal
    in the Software without restriction, including without limitation the rights
    to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
    copies of the Software, and to permit persons to whom the Software is
    furnished to do so, subject to the following conditions:

    The above copyright notice and this permission notice shall be included in all
    copies or substantial portions of the Software.

    THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
    IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
    FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
    AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
    LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
    OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
    SOFTWARE.
