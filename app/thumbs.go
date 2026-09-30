package app

import (
	"github.com/marrasen/kakel/ui/files"
	"github.com/marrasen/kakel/vfs"

	"github.com/marrasen/gunim/paint"
)

// A file pane's icon view asks for the thumbnails of the image files in
// view; they are made a few at a time, on goroutines of their own, from
// the file on whichever machine it is, and kept in files.Thumbnails for
// every window to draw. ThumbsMade counts them, so the windows draw
// again as they arrive.

// NeedThumbs asks for the thumbnails of Names, image files in the
// folder file pane Pane shows.
type NeedThumbs struct {
	Pane  string
	Names []string
}

// mostThumbWork is how many thumbnails are made at once.
const mostThumbWork = 3

// mostThumbBytes is the largest file a thumbnail is made of, which is
// less than the reader takes: a thumbnail is not worth a long wait on
// a slow link.
const mostThumbBytes = 16 << 20

// thumbJob is a thumbnail to make: its key, and the file.
type thumbJob struct {
	key  files.ThumbKey
	from vfs.FS
	at   string
}

// needThumbs queues the thumbnails asked for that are neither made nor
// on their way.
func (a *app) needThumbs(in NeedThumbs) {
	b, ok := a.st.Browsers[in.Pane]
	f := a.filesOf(in.Pane)
	if !ok || f == nil || b.Archive {
		return
	}
	machine := string(a.filesKey(in.Pane))
	for _, name := range in.Names {
		i := -1
		for k, e := range b.Entries {
			if e.Name == name {
				i = k
				break
			}
		}
		if i < 0 || !files.IsImage(name) || b.Entries[i].IsDir() || b.Entries[i].Size > mostThumbBytes {
			continue
		}
		e := b.Entries[i]
		k := files.ThumbKey{Machine: machine, Path: vfs.Join(f, b.Path, name), Mod: e.Mod, Size: e.Size}
		if _, done := files.Thumbnails.Get(k); done || a.thumbWanted[k] {
			continue
		}
		if a.thumbWanted == nil {
			a.thumbWanted = map[files.ThumbKey]bool{}
		}
		a.thumbWanted[k] = true
		a.thumbQueue = append(a.thumbQueue, thumbJob{key: k, from: f, at: k.Path})
	}
	a.pumpThumbs()
}

// pumpThumbs starts making thumbnails while there are some to make and
// room to make them, the newest asked for first: those are in view.
func (a *app) pumpThumbs() {
	for a.thumbWorking < mostThumbWork && len(a.thumbQueue) > 0 {
		j := a.thumbQueue[len(a.thumbQueue)-1]
		a.thumbQueue = a.thumbQueue[:len(a.thumbQueue)-1]
		a.thumbWorking++
		go func() {
			th := files.Thumb{}
			if pic, err := files.ReadImage(j.from, j.at, 4*files.ThumbSide); err != nil {
				th.Err = err.Error()
			} else {
				th.Image = paint.NewImageFit(pic.Img, files.ThumbSide, files.ThumbSide)
			}
			files.Thumbnails.Put(j.key, th)
			a.events <- func() {
				a.thumbWorking--
				delete(a.thumbWanted, j.key)
				a.st.ThumbsMade++
				a.pumpThumbs()
			}
		}()
	}
}
