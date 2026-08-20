//go:build !amd64 && !arm64 && !386

package assets

// Fallback for unsupported architectures — empty, loader falls back to system DLL.
var Webview2Loader []byte
