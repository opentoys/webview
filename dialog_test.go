package webview

type dialogPlatformStub struct{}

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
func (p *dialogPlatformStub) SetFilesDropped(func([]string)) error      { return nil }
