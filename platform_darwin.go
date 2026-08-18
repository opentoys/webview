package webview

import "github.com/opentoys/webview/platform/darwin"

func newPlatform(opts Options) Platform {
	return darwin.New()
}
