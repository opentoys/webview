package webview

import (
	"errors"
	"path/filepath"
	"testing"
)

type dialogPlatformStub struct {
	file, dir       string
	fileErr, dirErr error
	save            string
	saveErr         error
	filters         []FileFilter
	saveOpts        FileDialogOptions
}

func (p *dialogPlatformStub) Run() error                                { return nil }
func (p *dialogPlatformStub) Close() error                              { return nil }
func (p *dialogPlatformStub) SetTitle(string) error                     { return nil }
func (p *dialogPlatformStub) SetSize(int, int, SizeHint)                {}
func (p *dialogPlatformStub) Navigate(string) error                     { return nil }
func (p *dialogPlatformStub) SetHTML(string) error                      { return nil }
func (p *dialogPlatformStub) Eval(string) error                         { return nil }
func (p *dialogPlatformStub) Init(string) error                         { return nil }
func (p *dialogPlatformStub) InterceptResource(string, ResourceHandler) {}
func (p *dialogPlatformStub) SetMenus([]Menu)                           {}
func (p *dialogPlatformStub) MainThread(f func())                       { f() }
func (p *dialogPlatformStub) ChooseFile(filters ...FileFilter) (string, error) {
	p.filters = filters
	return p.file, p.fileErr
}
func (p *dialogPlatformStub) SaveFile(opts FileDialogOptions) (string, error) {
	p.saveOpts = opts
	return p.save, p.saveErr
}
func (p *dialogPlatformStub) ChooseDir() (string, error)           { return p.dir, p.dirErr }
func (p *dialogPlatformStub) SetFilesDropped(func([]string)) error { return nil }

func TestChoosePathForwarding(t *testing.T) {
	p := &dialogPlatformStub{file: "/tmp/file.txt", dirErr: ErrDialogCancelled}
	w := &W{p: p}
	if got, err := w.ChooseFile(); err != nil || got != p.file {
		t.Fatalf("ChooseFile() = %q, %v", got, err)
	}
	if got, err := w.ChooseDir(); got != "" || !errors.Is(err, ErrDialogCancelled) {
		t.Fatalf("ChooseDir() = %q, %v", got, err)
	}
}

func TestChooseAndSaveForwarding(t *testing.T) {
	p := &dialogPlatformStub{file: "relative.txt", saveErr: ErrDialogCancelled}
	w := &W{p: p}
	if got, err := w.ChooseFile(FileFilter{Name: "Text", Extensions: []string{"txt"}}); err != nil || !filepath.IsAbs(got) {
		t.Fatalf("ChooseFile = %q, %v", got, err)
	}
	if len(p.filters) != 1 || p.filters[0].Extensions[0] != "txt" {
		t.Fatalf("ChooseFile filters were not forwarded: %#v", p.filters)
	}
	opts := FileDialogOptions{Filename: "report.pdf"}
	if got, err := w.SaveFile(opts); got != "" || !errors.Is(err, ErrDialogCancelled) {
		t.Fatalf("SaveFile = %q, %v", got, err)
	}
	if p.saveOpts.Filename != opts.Filename {
		t.Fatalf("SaveFile options were not forwarded: %#v", p.saveOpts)
	}
}
