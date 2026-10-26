package webview

import (
	"errors"
	"testing"
)

type dialogPlatformStub struct {
	file, dir       string
	fileErr, dirErr error
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
func (p *dialogPlatformStub) ChooseFile() (string, error)               { return p.file, p.fileErr }
func (p *dialogPlatformStub) ChooseDir() (string, error)                { return p.dir, p.dirErr }

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
