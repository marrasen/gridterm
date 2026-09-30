package view

import (
	"image/color"
	"path"
	"strings"

	"github.com/marrasen/kakel/vfs"

	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/theme"
)

// A file pane shows each entry's kind as an icon before its name,
// picked by what the entry is and what its name ends in, and coloured
// by kind so a folder of mixed files reads at a glance.

// The kinds' colours, each a theme token so a theme can change them.
var (
	fileFolder  = theme.Color("kakel.file.folder", color.NRGBA{R: 0xe8, G: 0xb3, B: 0x4a, A: 0xff})
	fileImage   = theme.Color("kakel.file.image", color.NRGBA{R: 0xc0, G: 0x84, B: 0xfc, A: 0xff})
	fileCode    = theme.Color("kakel.file.code", color.NRGBA{R: 0x5e, G: 0x9c, B: 0xff, A: 0xff})
	fileArchive = theme.Color("kakel.file.archive", color.NRGBA{R: 0xf0, G: 0x8a, B: 0x4b, A: 0xff})
	fileMedia   = theme.Color("kakel.file.media", color.NRGBA{R: 0xf4, G: 0x72, B: 0xb6, A: 0xff})
	fileRun     = theme.Color("kakel.file.run", color.NRGBA{R: 0x4a, G: 0xde, B: 0x80, A: 0xff})
	fileLink    = theme.Color("kakel.file.link", color.NRGBA{R: 0x2d, G: 0xd4, B: 0xbf, A: 0xff})
	filePlain   = theme.Color("kakel.file.plain", color.NRGBA{R: 0x8a, G: 0x93, B: 0xa6, A: 0xff})
)

// fileKind is how an entry is shown: its icon and its colour.
type fileKind struct {
	icon *icon.Icon
	ink  *theme.Token[color.NRGBA]
}

// kindsByExt are the kinds of file told by the end of their names.
var kindsByExt = map[string]fileKind{}

func init() {
	add := func(k fileKind, exts ...string) {
		for _, e := range exts {
			kindsByExt[e] = k
		}
	}
	add(fileKind{icon.FileImage, &fileImage}, ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".tif", ".tiff", ".svg", ".ico", ".heic", ".avif")
	add(fileKind{icon.FileCode, &fileCode}, ".go", ".c", ".h", ".cc", ".cpp", ".hpp", ".rs", ".py", ".js", ".mjs", ".ts", ".tsx", ".jsx",
		".java", ".kt", ".cs", ".rb", ".php", ".swift", ".lua", ".pl", ".r", ".scala", ".zig", ".css", ".scss", ".html", ".htm", ".vue", ".sql")
	add(fileKind{icon.FileJson, &fileCode}, ".json", ".yaml", ".yml", ".toml", ".xml", ".ini", ".conf", ".cfg", ".env")
	add(fileKind{icon.FileTerminal, &fileRun}, ".sh", ".bash", ".zsh", ".fish", ".ps1", ".bat", ".cmd", ".exe", ".msi", ".appimage", ".deb", ".rpm")
	add(fileKind{icon.FileArchive, &fileArchive}, ".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".zst", ".7z", ".rar", ".jar", ".whl", ".iso")
	add(fileKind{icon.FileAudio, &fileMedia}, ".mp3", ".wav", ".flac", ".ogg", ".m4a", ".aac", ".opus")
	add(fileKind{icon.FileVideo, &fileMedia}, ".mp4", ".mkv", ".mov", ".avi", ".webm", ".m4v")
	add(fileKind{icon.FileSpreadsheet, &fileCode}, ".csv", ".tsv", ".xls", ".xlsx", ".ods")
	add(fileKind{icon.FileText, &filePlain}, ".txt", ".md", ".log", ".pdf", ".doc", ".docx", ".odt", ".rtf")
	add(fileKind{icon.FileKey, &fileArchive}, ".pem", ".key", ".crt", ".cer", ".pub", ".gpg", ".asc")
}

// kindOf returns how entry e is shown.
func kindOf(e vfs.Entry) fileKind {
	switch {
	case e.IsLink() && e.IsDir():
		return fileKind{icon.FolderSymlink, &fileLink}
	case e.IsLink():
		return fileKind{icon.FileSymlink, &fileLink}
	case e.Archive:
		return fileKind{icon.FolderArchive, &fileArchive}
	case e.IsDir():
		return fileKind{icon.Folder, &fileFolder}
	}
	if k, ok := kindsByExt[strings.ToLower(path.Ext(e.Name))]; ok {
		return k
	}
	if e.Mode&0o111 != 0 {
		return fileKind{icon.FileTerminal, &fileRun}
	}
	return fileKind{icon.File, &filePlain}
}
