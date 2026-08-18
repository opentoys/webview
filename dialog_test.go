package webview

import "testing"

func TestDefaultDialog(t *testing.T) {
	w, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Default alert: returns (defaultInput, true).
	result, ok := w.dialog(DialogAlert, "hello", "ignored")
	if result != "ignored" || ok != true {
		t.Fatalf("alert: got (%q, %v), want (ignored, true)", result, ok)
	}
	// Default confirm: returns ("", false).
	result, ok = w.dialog(DialogConfirm, "are you sure?", "")
	if ok != false {
		t.Fatalf("confirm: got (%q, %v), want (\"\", false)", result, ok)
	}
	// Default prompt: returns (defaultInput, true).
	result, ok = w.dialog(DialogPrompt, "enter name:", "Alice")
	if result != "Alice" || ok != true {
		t.Fatalf("prompt: got (%q, %v), want (\"Alice\", true)", result, ok)
	}
}

func TestCustomDialog(t *testing.T) {
	w, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	w.SetDialogHandler(func(kind DialogKind, message, defaultInput string) (string, bool) {
		if kind == DialogConfirm && message == "delete?" {
			return "", true
		}
		return defaultInput, false
	})
	_, ok := w.dialog(DialogConfirm, "delete?", "")
	if !ok {
		t.Fatal("confirm delete?: expected true")
	}
	result, ok := w.dialog(DialogPrompt, "name:", "Bob")
	if result != "Bob" || ok != false {
		t.Fatalf("prompt: got (%q, %v), want (\"Bob\", false)", result, ok)
	}
}
