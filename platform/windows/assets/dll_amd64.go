//go:build amd64

package assets

import _ "embed"

//go:embed amd64/WebView2Loader.dll
var Webview2Loader []byte
