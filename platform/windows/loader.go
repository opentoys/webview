//go:build windows

package windows

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// WebView2CreateEnvironmentWithOptions is the function signature from WebView2Loader.dll.
type WebView2CreateEnvironmentWithOptions func(
	reserved, browserExecutableFolder, userDataFolder, environmentOptions uintptr,
	handler *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler,
) uintptr

// loadWebView2Loader loads CreateCoreWebView2EnvironmentWithOptions from
// WebView2Loader.dll. Searches: exe directory, PATH, then embedded fallback.
func loadWebView2Loader() (WebView2CreateEnvironmentWithOptions, error) {
	// Try system DLL first (searches PATH, exe dir).
	dll := syscall.NewLazyDLL("WebView2Loader.dll")
	if err := dll.Load(); err == nil {
		proc := dll.NewProc("CreateCoreWebView2EnvironmentWithOptions")
		return makeLoaderFunc(proc), nil
	}

	// Try exe directory.
	var buf [windows.MAX_PATH]uint16
	n, err := windows.GetModuleFileName(0, &buf[0], uint32(len(buf)))
	if err == nil && n > 0 {
		dir := filepath.Dir(windows.UTF16ToString(buf[:n]))
		dllPath := filepath.Join(dir, "WebView2Loader.dll")
		if _, statErr := os.Stat(dllPath); statErr == nil {
			dll2 := syscall.NewLazyDLL(dllPath)
			if loadErr := dll2.Load(); loadErr == nil {
				proc := dll2.NewProc("CreateCoreWebView2EnvironmentWithOptions")
				return makeLoaderFunc(proc), nil
			}
		}
	}

	// Try embedded DLL (written to temp).
	if len(embeddedLoaderDLL) > 0 {
		tmp := filepath.Join(os.TempDir(), "webview2loader.dll")
		if err := os.WriteFile(tmp, embeddedLoaderDLL, 0755); err == nil {
			dll3 := syscall.NewLazyDLL(tmp)
			if loadErr := dll3.Load(); loadErr == nil {
				proc := dll3.NewProc("CreateCoreWebView2EnvironmentWithOptions")
				return makeLoaderFunc(proc), nil
			}
		}
	}

	return nil, fmt.Errorf("webview: WebView2Loader.dll not found; install the WebView2 Runtime or place WebView2Loader.dll next to the executable")
}

func makeLoaderFunc(proc *syscall.LazyProc) WebView2CreateEnvironmentWithOptions {
	return func(reserved, browserExe, userData, envOpts uintptr,
		handler *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler) uintptr {
		r, _, _ := proc.Call(reserved, browserExe, userData, envOpts,
			uintptr(unsafe.Pointer(handler)))
		return r
	}
}
