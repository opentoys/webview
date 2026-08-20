//go:build 386

package assets

import _ "embed"

//go:embed x86/WebView2Loader.dll
var Webview2Loader []byte
