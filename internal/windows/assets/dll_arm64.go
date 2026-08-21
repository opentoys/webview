//go:build arm64

package assets

import (
	"crypto/md5"
	_ "embed"
)

//go:embed arm64/WebView2Loader.dll
var Webview2Loader []byte
var Webview2LoaderMD5 = md5.Sum(Webview2Loader)
