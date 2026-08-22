//go:build windows

package windows

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/opentoys/webview/internal/windows/assets"
)

const dll = "WebView2Loader.dll"

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
//  2. exe-directory dll
//  3. current-directory dll
//  4. Write assets to Temp directory
func loadWebView2Loader() (opt WebView2CreateEnvironmentWithOptions, e error) {
	// 1. Env var override.
	dllPath := os.Getenv("X_WEBVIEW2LOADER_DLL")
	if dllPath != "" && isExists(dllPath) {
		return loadDLL(dllPath)
	}

	// 2. exe-directory.
	if dir := exeDir(); dir != "" {
		dllPath = filepath.Join(dir, dll)
		if isExists(dllPath) {
			return loadDLL(dllPath)
		}
	}

	// 3. Current directory
	if isExists(dll) {
		return loadDLL(dll)
	}

	// 4. Temp directory
	dllPath, e = writedll()
	if e == nil {
		return loadDLL(dllPath)
	}
	return nil, fmt.Errorf("webview: WebView2Loader.dll not found; install the WebView2 Runtime, place WebView2Loader.dll next to the executable, or set X_WEBVIEW2LOADER_DLL")
}

// loadDLL loads a DLL from a given path and returns the loader function.
func loadDLL(path string) (WebView2CreateEnvironmentWithOptions, error) {
	dll := syscall.NewLazyDLL(path)
	if err := dll.Load(); err != nil {
		return nil, err
	}
	return makeLoaderFunc(dll.NewProc("CreateCoreWebView2EnvironmentWithOptions")), nil
}

// maxPath is the Windows MAX_PATH constant.
const maxPath = 260

// pGetModuleFileNameW is resolved lazily from kernel32.dll.
var pGetModuleFileNameW = kernel32.NewProc("GetModuleFileNameW")

// exeDir returns the directory of the current executable, or "" on error.
func exeDir() string {
	var buf [maxPath]uint16
	r, _, _ := pGetModuleFileNameW.Call(0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return ""
	}
	return filepath.Dir(syscall.UTF16ToString(buf[:r]))
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

func writedll() (path string, err error) {
	path = filepath.Join(os.TempDir(), hex.EncodeToString(assets.Webview2LoaderMD5[:]), dll)
	return path, writeFileIfNotExists(path, assets.Webview2Loader)
}

// writeFileIfNotExists 文件不存在时才写入，存在则跳过
func writeFileIfNotExists(path string, data []byte) error {
	if isExists(path) {
		return nil
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	return os.WriteFile(path, data, os.ModePerm)
}

func isExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}
