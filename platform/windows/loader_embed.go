//go:build windows

package windows

// embeddedLoaderDLL holds the WebView2Loader.dll bytes.
// ponytail: embedding deferred — add //go:embed + actual DLL binary when
// distributing. For now, requires WebView2Loader.dll in PATH or exe dir.
var embeddedLoaderDLL []byte
