//go:build !amd64 && !arm64 && !386

package assets

import "crypto/md5"

// Fallback for unsupported architectures — empty, loader falls back to system DLL.
var Webview2Loader []byte
var Webview2LoaderMD5 = md5.Sum(Webview2Loader)
