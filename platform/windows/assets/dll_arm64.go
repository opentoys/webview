//go:build arm64

package assets

import _ "embed"

//go:embed arm64/WebView2Loader.dll
var Webview2Loader []byte
