package darwin

type DialogKind int

const (
	DialogAlert DialogKind = iota
	DialogConfirm
	DialogPrompt
)

type SizeHint int

const (
	SizeNone SizeHint = iota
	SizeMin
	SizeMax
	SizeFixed
)

type Platform struct{}

func New() *Platform { return &Platform{} }

func (p *Platform) Run() error                  { return nil }
func (p *Platform) Close() error                { return nil }
func (p *Platform) SetTitle(title string) error { return nil }
func (p *Platform) SetSize(width, height int, hint SizeHint) {
}
func (p *Platform) Navigate(url string) error { return nil }
func (p *Platform) SetHTML(html string) error { return nil }
func (p *Platform) Eval(js string) error      { return nil }
func (p *Platform) Dialog(kind DialogKind, message, defaultInput string) (string, bool) {
	return defaultInput, false
}
