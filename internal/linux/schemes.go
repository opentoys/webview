//go:build linux

package linux

import (
	"errors"
	"fmt"
	"net/http"
	"unsafe"

	"github.com/ebitengine/purego"
)

// --- scheme handling -------------------------------------------------------

// soup3 is true when WebKitGTK links libsoup 3. soupForeachCB is the matching
// callback for soup_message_headers_foreach. Both are initialized once when
// the first custom scheme is registered.
var (
	soup3         bool
	soupForeachCB uintptr
)

// registerSchemes wires each ResourceHandler onto the WebKitWebContext and
// marks the scheme as a secure context. Called from Run before the main loop.
func (p *gtk) registerSchemes() error {
	if len(p.schemeHandlers) == 0 {
		return nil
	}
	if p.webview == 0 {
		return errors.New("webview: register schemes: web view not created")
	}

	glib, err := openFirst("libglib-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load glib: %w", err)
	}
	gobject, err := openFirst("libgobject-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load gobject: %w", err)
	}
	gio, err := openFirst("libgio-2.0.so.0")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load gio: %w", err)
	}

	webkit, err := openFirst("libwebkitgtk-6.0.so.4")
	if err != nil {
		return fmt.Errorf("webview: register schemes: load webkit: %w", err)
	}

	// Detect libsoup version (2 vs 3) by probing the already-loaded soup
	// library without loading a new one (RTLD_NOLOAD = 0x4).
	const rtldNoLoad = 0x4
	var soupLib uintptr
	if h, e := purego.Dlopen("libsoup-3.0.so.0", rtldNoLoad); e == nil {
		soup3 = true
		soupLib = h
	} else if h, e := purego.Dlopen("libsoup-2.4.so.1", rtldNoLoad); e == nil {
		soup3 = false
		soupLib = h
	}
	if soupLib != 0 {
		purego.RegisterLibFunc(&soupMessageHeadersForeach, soupLib, "soup_message_headers_foreach")
		// libsoup3 callback: (name, value, user_data).
		// libsoup2 callback: (hdrs, name, value, user_data).
		soupForeachCB = purego.NewCallback(func(a, b, c, d uintptr) uintptr {
			var name, value uintptr
			if soup3 {
				name, value = a, b
			} else {
				name, value = b, c
			}
			h := (*http.Header)(unsafe.Pointer(d))
			n := cstr(name)
			if n != "" {
				h.Add(n, cstr(value))
			}
			return 0
		})
	}

	var (
		getContext               func(uintptr) uintptr
		registerScheme           func(ctx uintptr, scheme string, cb, data, notify uintptr)
		getSecurityManager       func(uintptr) uintptr
		registerAsSecure         func(sm uintptr, scheme string)
		requestGetURI            func(uintptr) uintptr
		requestGetScheme         func(uintptr) uintptr
		requestGetHTTPMethod     func(uintptr) uintptr
		requestGetHTTPBody       func(uintptr) uintptr
		schemeRequestFinish      func(req, stream uintptr, streamLen int64, contentType string)
		schemeRequestFinishError func(req, err uintptr)
		memInputStreamNew        func(data unsafe.Pointer, length int, destroy uintptr) uintptr
		gInputStreamRead         func(stream, buf uintptr, count uint, cancellable uintptr, gerr *uintptr) int
		schemeGObjectUnref       func(uintptr)
		newErrorLiteral          func(domain uint32, code int32, message string) uintptr
		freeError                func(err uintptr)
		ioErrorQuark             func() uint32
		gFreeAddr                uintptr
	)
	purego.RegisterLibFunc(&getContext, webkit, "webkit_web_view_get_context")
	purego.RegisterLibFunc(&registerScheme, webkit, "webkit_web_context_register_uri_scheme")
	purego.RegisterLibFunc(&getSecurityManager, webkit, "webkit_web_context_get_security_manager")
	purego.RegisterLibFunc(&registerAsSecure, webkit, "webkit_security_manager_register_uri_scheme_as_secure")
	purego.RegisterLibFunc(&requestGetURI, webkit, "webkit_uri_scheme_request_get_uri")
	purego.RegisterLibFunc(&requestGetScheme, webkit, "webkit_uri_scheme_request_get_scheme")
	purego.RegisterLibFunc(&requestGetHTTPMethod, webkit, "webkit_uri_scheme_request_get_http_method")
	purego.RegisterLibFunc(&requestGetHTTPBody, webkit, "webkit_uri_scheme_request_get_http_body")
	purego.RegisterLibFunc(&requestGetHTTPHeaders, webkit, "webkit_uri_scheme_request_get_http_headers")
	purego.RegisterLibFunc(&schemeRequestFinish, webkit, "webkit_uri_scheme_request_finish")
	purego.RegisterLibFunc(&schemeRequestFinishError, webkit, "webkit_uri_scheme_request_finish_error")
	purego.RegisterLibFunc(&memInputStreamNew, gio, "g_memory_input_stream_new_from_data")
	purego.RegisterLibFunc(&gInputStreamRead, gio, "g_input_stream_read")
	purego.RegisterLibFunc(&schemeGObjectUnref, gobject, "g_object_unref")
	purego.RegisterLibFunc(&newErrorLiteral, glib, "g_error_new_literal")
	purego.RegisterLibFunc(&freeError, glib, "g_error_free")
	purego.RegisterLibFunc(&ioErrorQuark, gio, "g_io_error_quark")

	gFreeAddr, err = purego.Dlsym(glib, "g_free")
	if err != nil {
		return fmt.Errorf("webview: register schemes: resolve g_free: %w", err)
	}

	memdup, err := resolveMemdup(glib)
	if err != nil {
		return fmt.Errorf("webview: register schemes: %w", err)
	}

	ctx := getContext(p.webview)
	if ctx == 0 {
		return errors.New("webview: register schemes: web context is nil")
	}
	sm := getSecurityManager(ctx)
	if sm == 0 {
		return errors.New("webview: register schemes: security manager is nil")
	}

	p.schemeCB = purego.NewCallback(func(request uintptr, data uintptr) uintptr {
		eng := lookupPlatform(data)
		if eng == nil {
			return 0
		}
		url := cstr(requestGetURI(request))
		scheme := cstr(requestGetScheme(request))

		eng.mu.Lock()
		handler := eng.schemeHandlers[scheme]
		eng.mu.Unlock()
		if handler == nil {
			const gIOErrorNotFound = 1
			gerr := newErrorLiteral(ioErrorQuark(), gIOErrorNotFound, "resource not found")
			schemeRequestFinishError(request, gerr)
			freeError(gerr)
			return 0
		}

		// Extract HTTP method (nil/empty → GET).
		method := http.MethodGet
		if m := cstr(requestGetHTTPMethod(request)); m != "" {
			method = m
		}

		// Extract HTTP body from GInputStream (nil for GET/HEAD).
		var body []byte
		switch method {
		case "GET", "HEAD", "TRACE", "OPTIONS":
		default:
			if stream := requestGetHTTPBody(request); stream != 0 {
				buf := make([]byte, 4096)
				for {
					var gerr uintptr
					n := gInputStreamRead(stream, uintptr(unsafe.Pointer(&buf[0])), uint(len(buf)), 0, &gerr)
					if n <= 0 {
						break
					}
					body = append(body, buf[:n]...)
				}
				schemeGObjectUnref(stream)
			}
		}

		sr := ResourceRequest{URL: url, Method: method, Headers: extractRequestHeaders(request), Body: body}
		var resp *ResourceResponse
		handler(sr, func(r *ResourceResponse) {
			resp = r
		})
		if resp == nil {
			const gIOErrorNotFound = 1
			gerr := newErrorLiteral(ioErrorQuark(), gIOErrorNotFound, "resource not found")
			schemeRequestFinishError(request, gerr)
			freeError(gerr)
			return 0
		}

		mime := "application/octet-stream"
		if ct := resp.Headers.Get("Content-Type"); ct != "" {
			mime = ct
		} else if ct := resp.Headers.Get("content-type"); ct != "" {
			mime = ct
		}

		respBody := resp.Body
		var dataPtr unsafe.Pointer
		if len(respBody) > 0 {
			dataPtr = memdup(unsafe.Pointer(&respBody[0]), len(respBody))
		}
		stream := memInputStreamNew(dataPtr, len(respBody), uintptr(gFreeAddr))
		schemeRequestFinish(request, stream, int64(len(respBody)), mime)
		schemeGObjectUnref(stream)
		return 0
	})

	for scheme := range p.schemeHandlers {
		registerScheme(ctx, scheme, p.schemeCB, p.id, 0)
		registerAsSecure(sm, scheme)
	}
	return nil
}

// extractRequestHeaders reads the request's HTTP headers via WebKitGTK's
// webkit_uri_scheme_request_get_http_headers (returns a SoupMessageHeaders*),
// then iterates them with soup_message_headers_foreach. The per-version
// callback signature is handled by soupForeachCB / soup3. Returns an empty
// http.Header if headers are unavailable (e.g. soup not loaded).
func extractRequestHeaders(request uintptr) http.Header {
	h := make(http.Header)
	if requestGetHTTPHeaders == nil {
		return h
	}
	hdrs := requestGetHTTPHeaders(request)
	if hdrs == 0 || soupForeachCB == 0 {
		return h
	}
	soupMessageHeadersForeach(hdrs, soupForeachCB, uintptr(unsafe.Pointer(&h)))
	return h
}

// resolveMemdup returns g_memdup2 (GLib >= 2.68) or g_memdup (older).
func resolveMemdup(glib uintptr) (func(mem unsafe.Pointer, size int) unsafe.Pointer, error) {
	addr, err := purego.Dlsym(glib, "g_memdup2")
	if err == nil && addr != 0 {
		var f func(mem unsafe.Pointer, size uint64) unsafe.Pointer
		purego.RegisterFunc(&f, addr)
		return func(mem unsafe.Pointer, size int) unsafe.Pointer { return f(mem, uint64(size)) }, nil
	}
	addr, err = purego.Dlsym(glib, "g_memdup")
	if err == nil && addr != 0 {
		var f func(mem unsafe.Pointer, size uint32) unsafe.Pointer
		purego.RegisterFunc(&f, addr)
		return func(mem unsafe.Pointer, size int) unsafe.Pointer { return f(mem, uint32(size)) }, nil
	}
	return nil, errors.New("neither g_memdup2 nor g_memdup is available")
}
