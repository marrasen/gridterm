package files

import (
	"container/list"
	"sync"
	"time"

	"github.com/marrasen/gunim/paint"
)

// Thumbnails are the small pictures a file pane's icon view shows for
// image files: made from the file on whichever machine it is, off the
// goroutines that draw, and kept here for every window to draw from.
// The oldest go once they take more than ThumbBudget bytes.

// ThumbSide is the size a thumbnail is made at, either way, in pixels:
// enough for an icon view's tile on a screen at twice the usual scale.
const ThumbSide = 192

// ThumbBudget is how many bytes of thumbnails are kept.
const ThumbBudget = 64 << 20

// ThumbKey names one thumbnail: the file, where it is, and as it was
// when it was made, so a file that changes is made again.
type ThumbKey struct {
	Machine string
	Path    string
	Mod     time.Time
	Size    int64
}

// Thumb is a thumbnail, or why none could be made: a file that would
// not decode is not tried again until it changes.
type Thumb struct {
	Image *paint.Image
	Err   string
}

// Thumbs keeps thumbnails. It is safe to use from any goroutine.
type Thumbs struct {
	mu    sync.Mutex
	byKey map[ThumbKey]*list.Element
	order *list.List
	bytes int
}

type thumbEntry struct {
	key   ThumbKey
	thumb Thumb
	bytes int
}

// Thumbnails are the thumbnails kakel has made.
var Thumbnails = NewThumbs()

// NewThumbs returns an empty store.
func NewThumbs() *Thumbs {
	return &Thumbs{byKey: map[ThumbKey]*list.Element{}, order: list.New()}
}

// Get returns the thumbnail for k, and whether one was made or tried.
func (t *Thumbs) Get(k ThumbKey) (Thumb, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	el, ok := t.byKey[k]
	if !ok {
		return Thumb{}, false
	}
	t.order.MoveToFront(el)
	return el.Value.(*thumbEntry).thumb, true
}

// Put keeps th for k, letting the oldest go past the budget.
func (t *Thumbs) Put(k ThumbKey, th Thumb) {
	n := 64
	if th.Image != nil {
		w, h := th.Image.Size()
		n += 4 * w * h
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if el, ok := t.byKey[k]; ok {
		t.bytes -= el.Value.(*thumbEntry).bytes
		t.order.Remove(el)
	}
	t.byKey[k] = t.order.PushFront(&thumbEntry{key: k, thumb: th, bytes: n})
	t.bytes += n
	for t.bytes > ThumbBudget && t.order.Len() > 1 {
		last := t.order.Back()
		e := last.Value.(*thumbEntry)
		t.order.Remove(last)
		delete(t.byKey, e.key)
		t.bytes -= e.bytes
	}
}
