package conns

// Each calls fn for every connection on the list, in the order they were
// opened, and stops early when fn says so.
//
// It is for a caller that only reads: it asks nothing of the heap, where
// Groups builds a slice of groups and a slice of rows inside each. The
// panel asks once a frame whether anything about the list has changed,
// and a frame that built two slices to answer "no" would be two slices a
// frame for nothing.
//
// The lock is held for the whole walk, so fn must not touch the registry.
func (r *Registry) Each(fn func(e *Entry) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if !fn(e) {
			return
		}
	}
}
