package types

import (
	"errors"
	"sync"
)

var ErrMenuUnsupported = errors.New("webview: native menu extension unsupported")

// MenuActions owns one menu's callbacks. Replacement invalidates native items
// still visible in an open popup; callbacks run outside native UI stacks.
type MenuActions struct {
	mu         sync.Mutex
	generation uint64
	closed     bool
}

func (a *MenuActions) Replace(items []MenuItem) []MenuItem {
	a.mu.Lock()
	a.generation++
	generation := a.generation
	a.closed = false
	a.mu.Unlock()
	result := NormalizeMenuItems(items)
	for i := range result {
		fn := result[i].Action
		if fn == nil {
			continue
		}
		result[i].Action = func() {
			a.mu.Lock()
			valid := !a.closed && a.generation == generation
			a.mu.Unlock()
			if valid {
				go fn()
			}
		}
	}
	return result
}
func (a *MenuActions) Close() { a.mu.Lock(); a.closed = true; a.generation++; a.mu.Unlock() }

// NormalizeMenuItems removes leading, trailing and repeated separators.
func NormalizeMenuItems(items []MenuItem) []MenuItem {
	out := make([]MenuItem, 0, len(items))
	for _, item := range items {
		if item.Separator && (len(out) == 0 || out[len(out)-1].Separator) {
			continue
		}
		out = append(out, item)
	}
	if len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}
