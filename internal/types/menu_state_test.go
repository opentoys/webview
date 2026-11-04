package types

import (
	"testing"
	"time"
)

func TestNormalizeMenuItems(t *testing.T) {
	input := []MenuItem{
		{ID: "leading", Separator: true},
		{ID: "a"},
		{ID: "s1", Separator: true},
		{ID: "s2", Separator: true},
		{ID: "b"},
		{ID: "trailing", Separator: true},
	}
	got := NormalizeMenuItems(input)
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "s1" || got[2].ID != "b" {
		t.Fatalf("%#v", got)
	}
	if len(input) != 6 || input[0].ID != "leading" {
		t.Fatal("changed caller slice")
	}
	if got := NormalizeMenuItems([]MenuItem{{Separator: true}, {Separator: true}}); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestMenuActionsIndependentAndReentrant(t *testing.T) {
	var tray, context MenuActions
	called := make(chan string, 8)
	old := tray.Replace([]MenuItem{{Action: func() { called <- "old" }}})
	contextItems := context.Replace([]MenuItem{{Action: func() { called <- "context" }}})
	current := tray.Replace([]MenuItem{{Action: func() {
		tray.Replace(nil)
		tray.Close()
		called <- "current"
	}}})
	old[0].Action()
	current[0].Action()
	contextItems[0].Action()
	seen := map[string]bool{}
	for range 2 {
		select {
		case value := <-called:
			seen[value] = true
		case <-time.After(time.Second):
			t.Fatal("callback blocked")
		}
	}
	if !seen["current"] || !seen["context"] || seen["old"] {
		t.Fatal(seen)
	}
	context.Close()
	contextItems[0].Action()
	select {
	case value := <-called:
		t.Fatalf("stale callback: %s", value)
	default:
	}
}
