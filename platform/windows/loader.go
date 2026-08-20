//go:build windows

package windows

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/opentoys/webview/platform/windows/assets"
	"golang.org/x/sys/windows"
)

// WebView2CreateEnvironmentWithOptions is the function signature from
// WebView2Loader.dll's CreateCoreWebView2EnvironmentWithOptions.
// Args: (browserExecutableFolder, userDataFolder, environmentOptions, completedHandler).
type WebView2CreateEnvironmentWithOptions func(
	browserExecutableFolder, userDataFolder, environmentOptions uintptr,
	handler *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler,
) uintptr

// loadWebView2Loader loads CreateCoreWebView2EnvironmentWithOptions from
// WebView2Loader.dll.
//
// Search order:
//  1. X_WEBVIEW2LOADER_DLL env var (explicit path)
//  2. Embedded DLL (assets.Webview2Loader) → extracted to temp
//  3. System DLL (PATH / exe directory)
func loadWebView2Loader() (WebView2CreateEnvironmentWithOptions, error) {
	// 1. Env var override.
	if p := os.Getenv("X_WEBVIEW2LOADER_DLL"); p != "" {
		if fn, err := loadDLL(p); err == nil {
			return fn, nil
		}
	}

	// 2. Embedded DLL (architecture-specific, extracted to temp).
	if len(assets.Webview2Loader) > 0 {
		if fn, err := extractAndLoad(assets.Webview2Loader); err == nil {
			return fn, nil
		}
	}

	// 3. System DLL (searches PATH, exe dir).
	dll := syscall.NewLazyDLL("WebView2Loader.dll")
	if err := dll.Load(); err == nil {
		return makeLoaderFunc(dll.NewProc("CreateCoreWebView2EnvironmentWithOptions")), nil
	}

	// 4. Explicit exe-directory search.
	if dir := exeDir(); dir != "" {
		dllPath := filepath.Join(dir, "WebView2Loader.dll")
		if _, err := os.Stat(dllPath); err == nil {
			if fn, err := loadDLL(dllPath); err == nil {
				return fn, nil
			}
		}
	}

	return nil, fmt.Errorf("webview: WebView2Loader.dll not found; install the WebView2 Runtime, place WebView2Loader.dll next to the executable, or set X_WEBVIEW2LOADER_DLL")
}

// extractAndLoad writes embedded DLL bytes to a temp file and loads it.
// The filename includes a hash to avoid stale DLLs across versions.
func extractAndLoad(data []byte) (WebView2CreateEnvironmentWithOptions, error) {
	h := sha256.Sum256(data)
	name := fmt.Sprintf("webview2loader_%x.dll", h[:8])
	tmp := filepath.Join(os.TempDir(), name)

	// Reuse existing file if hash matches.
	if existing, err := os.ReadFile(tmp); err == nil {
		eh := sha256.Sum256(existing)
		if eh == h {
			return loadDLL(tmp)
		}
	}

	if err := os.WriteFile(tmp, data, 0755); err != nil {
		return nil, err
	}
	return loadDLL(tmp)
}

// loadDLL loads a DLL from a given path and returns the loader function.
func loadDLL(path string) (WebView2CreateEnvironmentWithOptions, error) {
	dll := syscall.NewLazyDLL(path)
	if err := dll.Load(); err != nil {
		return nil, err
	}
	return makeLoaderFunc(dll.NewProc("CreateCoreWebView2EnvironmentWithOptions")), nil
}

// exeDir returns the directory of the current executable, or "" on error.
func exeDir() string {
	var buf [windows.MAX_PATH]uint16
	n, err := windows.GetModuleFileName(0, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		return ""
	}
	return filepath.Dir(windows.UTF16ToString(buf[:n]))
}

func makeLoaderFunc(proc *syscall.LazyProc) WebView2CreateEnvironmentWithOptions {
	return func(browserExe, userData, envOpts uintptr,
		handler *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler) uintptr {
		r, _, _ := proc.Call(browserExe, userData, envOpts,
			uintptr(unsafe.Pointer(handler)))
		runtime.KeepAlive(handler)
		return r
	}
}
