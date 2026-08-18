package webview

import "github.com/opentoys/webview/platform/darwin"

type DialogKind = darwin.DialogKind

const (
	DialogAlert   = darwin.DialogAlert
	DialogConfirm = darwin.DialogConfirm
	DialogPrompt  = darwin.DialogPrompt
)

type SizeHint = darwin.SizeHint

const (
	SizeNone  = darwin.SizeNone
	SizeMin   = darwin.SizeMin
	SizeMax   = darwin.SizeMax
	SizeFixed = darwin.SizeFixed
)

type Platform interface {
	Run() error
	Close() error
	SetTitle(title string) error
	SetSize(w, h int, hint SizeHint)
	Navigate(url string) error
	SetHTML(html string) error
	Eval(js string) error
	Dialog(kind DialogKind, message, defaultInput string) (string, bool)
}

type Options struct{ Debug bool }

type W struct {
	p Platform
}

func New(opts Options) (*W, error) {
	return &W{p: newPlatform(opts)}, nil
}

// Run blocks until the window closes or Close is called.
func (w *W) Run() error            { return w.p.Run() }
func (w *W) Close() error          { return w.p.Close() }
func (w *W) SetTitle(title string) { w.p.SetTitle(title) }
func (w *W) SetSize(width, height int, hint SizeHint) {
	w.p.SetSize(width, height, hint)
}
func (w *W) Navigate(url string) error { return w.p.Navigate(url) }
func (w *W) SetHTML(html string) error { return w.p.SetHTML(html) }
func (w *W) Eval(js string) error      { return w.p.Eval(js) }
