//go:build darwin

package darwin

// Window state, navigation, and HTML bootstrap preparation.

import (
	"strings"

	"github.com/ebitengine/purego/objc"
)

func (p *Platform) SetTitle(title string) error {
	p.mu.Lock()
	if p.window == 0 {
		// No window yet (called before Run): remember the title and apply it
		// once setup() creates the window.
		p.pendingTitle = title
		p.mu.Unlock()
		return nil
	}
	w := p.window
	p.mu.Unlock()
	mainThread(func() {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, title)
		w.Send(setTitleSel, str)
	})
	return nil
}

func (p *Platform) SetSize(width, height int, hint SizeHint) {
}

func (p *Platform) Navigate(url string) error {
	p.mu.Lock()
	wv := p.webview
	if wv == 0 {
		// No webview yet (called before Run): remember the URL and load it
		// once setup() creates the webview.
		p.pendingURL = url
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()
	mainThread(func() {
		str := objc.ID(nsStringClass).Send(stringWithUTF8Sel, url)
		nsURL := objc.ID(nsURLClass).Send(URLWithStringSel, str)
		req := objc.ID(nsURLRequestClass).Send(requestWithURLSel, nsURL)
		wv.Send(loadRequestSel, req)
	})
	return nil
}

// indexHead returns a byte index in html suitable for inserting a <script> tag
// so that it runs before any user-supplied script. Looks for <head>, </head>,
// <body>, or the first <script>, returning the index AFTER that tag or -1.
func indexHead(html string) int {
	lower := strings.ToLower(html)
	for _, tag := range []string{"<head>", "<head ", "<head\t", "<head\n"} {
		if i := strings.Index(lower, tag); i >= 0 {
			return i + len(tag)
		}
	}
	if i := strings.Index(lower, "</head>"); i >= 0 {
		return i
	}
	if i := strings.Index(lower, "<body"); i >= 0 {
		return i
	}
	if i := strings.Index(lower, "<script"); i >= 0 {
		return i
	}
	return -1
}

// boundFuncNames returns the JS-visible func names from the active platform's
// BoundFuncs. Used by prependBootstrap; activePlatform is set in setup() before
// any SetHTML path runs, so it is always current.
func boundFuncNames() []string {
	if p := activePlatform; p != nil && p.BoundFuncs != nil {
		return p.BoundFuncs()
	}
	return nil
}

// injectBootstrapLocked adds a WKUserScript that runs the bridge bootstrap at
// document start. This mirrors the Linux path where pushUserScript fires on
// every navigation. Caller must hold p.mu.
func (p *Platform) injectBootstrapLocked() {
	src := bootstrapJS(boundFuncNames())
	if src == "" {
		return
	}
	addWKUserScript(p.ucc, src)
}

// prependBootstrap inserts the bridge bootstrap (webviewBridge + func stubs) as
// an inline <script> so it is available before any user-supplied script runs.
func prependBootstrap(html string) string {
	if js := bootstrapJS(boundFuncNames()); js != "" {
		// HTML parsing closes <script> on </script>, </script>, or </SCRIPT>.
		// Escape </ sequences so the script body is safe inside the tag.
		js = strings.ReplaceAll(js, "</", `<\/`)
		tag := "<script>" + js + "</script>"
		if i := indexHead(html); i >= 0 {
			html = html[:i] + tag + html[i:]
		} else {
			html = tag + html
		}
	}
	return html
}

// looksLikeHTML returns true if body starts with common HTML markers.
