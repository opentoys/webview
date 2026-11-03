package webview

import (
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opentoys/webview/internal/types"
)

func fakeDialogs() *dialogService {
	d := newDialogService()
	d.prepare = func() error { return nil }
	d.onUI = func() bool { return false }
	d.cancel = func() {}
	return d
}

func awaitDialog[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("dialog deadlocked")
		var zero T
		return zero
	}
}

func TestStandaloneDialogEntrypoints(t *testing.T) {
	isolatedApplication(t)
	d := fakeDialogs()
	app.dialogs = d
	var kinds []types.DialogKind
	d.show = func(k types.DialogKind, opts FileDialogOptions, _ func() bool) (string, error) {
		kinds = append(kinds, k)
		if k == types.DialogOpen && !reflect.DeepEqual(opts.Filters, []FileFilter{{Name: "Text", Extensions: []string{"txt"}}}) {
			t.Fatalf("filters: %#v", opts)
		}
		if k == types.DialogSave && opts.Filename != "report.txt" {
			t.Fatal(opts)
		}
		return "relative.txt", nil
	}
	for _, running := range []bool{false, true} {
		var done chan error
		if running {
			done = make(chan error, 1)
			go func() { done <- Run() }()
			for {
				app.mu.Lock()
				ready := app.running
				app.mu.Unlock()
				if ready {
					break
				}
				time.Sleep(time.Millisecond)
			}
		}
		calls := []func() (string, error){ChooseDir, func() (string, error) { return ChooseFile(FileFilter{Name: "Text", Extensions: []string{"txt"}}) }, func() (string, error) { return SaveFile(FileDialogOptions{Filename: "report.txt"}) }}
		for _, call := range calls {
			path, err := call()
			if err != nil || !filepath.IsAbs(path) {
				t.Fatalf("%q %v", path, err)
			}
		}
		if running {
			if err := Quit(); err != nil {
				t.Fatal(err)
			}
			if err := awaitDialog(t, done); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(app.windows) != 0 || len(app.trays) != 0 || len(kinds) != 6 {
		t.Fatal("dialogs created application objects")
	}
	if _, err := ChooseDir(); !errors.Is(err, ErrApplicationStopped) {
		t.Fatal(err)
	}
}

func TestDialogErrors(t *testing.T) {
	for _, expected := range []error{ErrDialogCancelled, ErrDialogUnsupported, ErrDialogMainThread} {
		d := fakeDialogs()
		d.show = func(types.DialogKind, FileDialogOptions, func() bool) (string, error) { return "bad", expected }
		if path, err := d.choose(types.DialogOpen, FileDialogOptions{}); path != "" || !errors.Is(err, expected) {
			t.Fatalf("%q %v", path, err)
		}
	}
}

func TestDialogQueueSnapshotAndReentry(t *testing.T) {
	d := fakeDialogs()
	entered := make(chan string, 3)
	release := make(chan struct{})
	var onUI atomic.Bool
	d.onUI = onUI.Load
	var active atomic.Int32
	d.show = func(_ types.DialogKind, opts FileDialogOptions, _ func() bool) (string, error) {
		if active.Add(1) != 1 {
			t.Error("overlapping dialogs")
		}
		defer active.Add(-1)
		entered <- opts.Filename
		if opts.Filename == "first" {
			<-release
		} else if opts.Filters[0].Extensions[0] != "txt" || opts.Filters[0].MIMETypes[0] != "text/plain" {
			t.Error("options were not copied")
		}
		return "/tmp/file", nil
	}
	results := make(chan error, 3)
	go func() { _, err := d.choose(types.DialogSave, FileDialogOptions{Filename: "first"}); results <- err }()
	awaitDialog(t, entered)
	opts := FileDialogOptions{Filename: "second", Filters: []FileFilter{{Extensions: []string{"txt"}, MIMETypes: []string{"text/plain"}}}}
	go func() { _, err := d.choose(types.DialogSave, opts); results <- err }()
	// The queue lock is also the handoff that proves the snapshot has completed.
	for {
		d.mu.Lock()
		n := len(d.requests)
		d.mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	opts.Filters[0].Extensions[0] = "changed"
	opts.Filters[0].MIMETypes[0] = "changed"
	onUI.Store(true)
	if _, err := d.choose(types.DialogOpen, FileDialogOptions{}); !errors.Is(err, ErrDialogReentrant) {
		t.Fatal(err)
	}
	onUI.Store(false)
	close(release)
	if got := awaitDialog(t, entered); got != "second" {
		t.Fatal(got)
	}
	for range 2 {
		if err := awaitDialog(t, results); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDialogQuitInterruptsActiveAndQueued(t *testing.T) {
	d := fakeDialogs()
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	var once sync.Once
	d.cancel = func() { once.Do(func() { close(cancelled) }) }
	d.show = func(types.DialogKind, FileDialogOptions, func() bool) (string, error) {
		close(entered)
		<-cancelled
		return "", ErrDialogCancelled
	}
	results := make(chan error, 8)
	go func() { _, err := d.choose(types.DialogOpen, FileDialogOptions{}); results <- err }()
	awaitDialog(t, entered)
	for range 7 {
		go func() { _, err := d.choose(types.DialogOpen, FileDialogOptions{}); results <- err }()
	}
	stopped := make(chan struct{})
	go func() { d.stop(); close(stopped) }()
	awaitDialog(t, stopped)
	for range 8 {
		if err := awaitDialog(t, results); !errors.Is(err, ErrApplicationStopped) {
			t.Fatal(err)
		}
	}
}

func TestDialogQuitDuringPreparation(t *testing.T) {
	d := fakeDialogs()
	entered := make(chan struct{})
	release := make(chan struct{})
	d.prepare = func() error { close(entered); <-release; return nil }
	d.show = func(types.DialogKind, FileDialogOptions, func() bool) (string, error) {
		t.Error("show after shutdown")
		return "", nil
	}
	result := make(chan error, 1)
	go func() { _, err := d.choose(types.DialogOpen, FileDialogOptions{}); result <- err }()
	awaitDialog(t, entered)
	d.cancel = func() { close(release) }
	d.stop()
	if err := awaitDialog(t, result); !errors.Is(err, ErrApplicationStopped) {
		t.Fatal(err)
	}
}

func TestClosingLastWindowLeavesDialogActive(t *testing.T) {
	isolatedApplication(t)
	d := fakeDialogs()
	app.dialogs = d
	entered := make(chan struct{})
	release := make(chan struct{})
	d.cancel = func() { t.Error("window close cancelled process dialog") }
	d.show = func(types.DialogKind, FileDialogOptions, func() bool) (string, error) {
		close(entered)
		<-release
		return "/tmp/independent", nil
	}
	w := pendingWindow(&lifecyclePlatform{})
	result := make(chan error, 1)
	go func() { _, err := ChooseDir(); result <- err }()
	awaitDialog(t, entered)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-result:
		t.Fatal("window close completed dialog")
	default:
	}
	close(release)
	if err := awaitDialog(t, result); err != nil {
		t.Fatal(err)
	}
	d.cancel = func() {}
	d.show = func(types.DialogKind, FileDialogOptions, func() bool) (string, error) { return "/tmp/after-close", nil }
	if _, err := ChooseFile(); err != nil {
		t.Fatal(err)
	}
}

func TestQuitFromDialogUIThreadUnwinds(t *testing.T) {
	isolatedApplication(t)
	d := fakeDialogs()
	app.dialogs = d
	var onUI atomic.Bool
	d.onUI = onUI.Load
	d.show = func(types.DialogKind, FileDialogOptions, func() bool) (string, error) {
		onUI.Store(true)
		defer onUI.Store(false)
		if err := Quit(); err != nil {
			t.Error(err)
		}
		return "", ErrDialogCancelled
	}
	result := make(chan error, 1)
	go func() { _, err := ChooseDir(); result <- err }()
	if err := awaitDialog(t, result); !errors.Is(err, ErrApplicationStopped) {
		t.Fatal(err)
	}
}
