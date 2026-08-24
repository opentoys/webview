//go:build windows

package windows

import (
	"runtime"
	"unsafe"
)

// --- ICoreWebView2WebResourceRequestedEventArgs ---

type iCoreWebView2WebResourceRequestedEventArgs struct {
	vtbl *iCoreWebView2WebResourceRequestedEventArgsVtbl
}

// Vtable layout from WebView2.h ICoreWebView2WebResourceRequestedEventArgs.
type iCoreWebView2WebResourceRequestedEventArgsVtbl struct {
	_IUnknownVtbl
	GetRequest        ComProc // 3
	GetResponse       ComProc // 4
	PutResponse       ComProc // 5
	GetRequestDeferral ComProc // 6
}

func (a *iCoreWebView2WebResourceRequestedEventArgs) GetRequest() *iCoreWebView2WebResourceRequest {
	var req *iCoreWebView2WebResourceRequest
	a.vtbl.GetRequest.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&req)),
	)
	return req
}

func (a *iCoreWebView2WebResourceRequestedEventArgs) PutResponse(resp *iCoreWebView2WebResourceResponse) uintptr {
	r, _, _ := a.vtbl.PutResponse.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(resp)),
	)
	runtime.KeepAlive(resp)
	return r
}

func (a *iCoreWebView2WebResourceRequestedEventArgs) GetRequestDeferral() *iCoreWebView2Deferral {
	var def *iCoreWebView2Deferral
	a.vtbl.GetRequestDeferral.Call(
		uintptr(unsafe.Pointer(a)),
		uintptr(unsafe.Pointer(&def)),
	)
	return def
}

// --- ICoreWebView2WebResourceRequest ---

type iCoreWebView2WebResourceRequest struct {
	vtbl *iCoreWebView2WebResourceRequestVtbl
}

// Vtable layout from WebView2.h ICoreWebView2WebResourceRequest.
type iCoreWebView2WebResourceRequestVtbl struct {
	_IUnknownVtbl
	GetUri     ComProc // 3
	GetMethod  ComProc // 4
	GetContent ComProc // 5
	GetHeaders ComProc // 6
}

func (r *iCoreWebView2WebResourceRequest) GetUri() string {
	var pwstr *uint16
	r.vtbl.GetUri.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(&pwstr)),
	)
	if pwstr == nil {
		return ""
	}
	s := wideToString(pwstr)
	pCoTaskMemFree.Call(uintptr(unsafe.Pointer(pwstr)))
	return s
}

func (r *iCoreWebView2WebResourceRequest) GetMethod() string {
	var pwstr *uint16
	r.vtbl.GetMethod.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(&pwstr)),
	)
	if pwstr == nil {
		return ""
	}
	s := wideToString(pwstr)
	pCoTaskMemFree.Call(uintptr(unsafe.Pointer(pwstr)))
	return s
}

func (r *iCoreWebView2WebResourceRequest) GetHeaders() map[string]string {
	var hdrs *iCoreWebView2HttpRequestHeaders
	r.vtbl.GetHeaders.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(&hdrs)),
	)
	if hdrs == nil {
		return nil
	}
	m := make(map[string]string)
	for _, name := range []string{
		"Accept", "Accept-Encoding", "Accept-Language",
		"Content-Type", "User-Agent", "Referer",
	} {
		if v := hdrs.GetHeader(name); v != "" {
			m[name] = v
		}
	}
	return m
}

// GetContent returns the request body as an IStream (nil for GET/HEAD).
func (r *iCoreWebView2WebResourceRequest) GetContent() *iStream {
	var stream *iStream
	r.vtbl.GetContent.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(&stream)),
	)
	return stream
}

// ReadAll reads the stream to exhaustion and releases it.
func (s *iStream) ReadAll() []byte {
	if s == nil {
		return nil
	}
	var body []byte
	buf := make([]byte, 4096)
	for {
		var n uint32
		s.vtbl.Read.Call(
			uintptr(unsafe.Pointer(s)),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			uintptr(unsafe.Pointer(&n)),
		)
		if n == 0 {
			break
		}
		body = append(body, buf[:n]...)
	}
	s.vtbl.Release.Call(uintptr(unsafe.Pointer(s)))
	return body
}

// --- ICoreWebView2WebResourceResponse ---

type iCoreWebView2WebResourceResponse struct {
	vtbl *iCoreWebView2WebResourceResponseVtbl
}

// Vtable layout from WebView2.h ICoreWebView2WebResourceResponse.
type iCoreWebView2WebResourceResponseVtbl struct {
	_IUnknownVtbl
	GetContent      ComProc // 3
	PutContent      ComProc // 4
	GetHeaders      ComProc // 5
	GetStatusCode   ComProc // 6
	PutStatusCode   ComProc // 7
	PutHeaders      ComProc // 8
	GetReasonPhrase ComProc // 9
	PutReasonPhrase ComProc // 10
}

func (r *iCoreWebView2WebResourceResponse) PutStatusCode(code int) uintptr {
	rv, _, _ := r.vtbl.PutStatusCode.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(code),
	)
	return rv
}

func (r *iCoreWebView2WebResourceResponse) PutReasonPhrase(phrase string) uintptr {
	p := utf16PtrFromStr(phrase)
	rv, _, _ := r.vtbl.PutReasonPhrase.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(p)),
	)
	runtime.KeepAlive(p)
	return rv
}

func (r *iCoreWebView2WebResourceResponse) PutHeaders(headers string) uintptr {
	p := utf16PtrFromStr(headers)
	rv, _, _ := r.vtbl.PutHeaders.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(p)),
	)
	runtime.KeepAlive(p)
	return rv
}

func (r *iCoreWebView2WebResourceResponse) PutContent(stream *iStream) uintptr {
	rv, _, _ := r.vtbl.PutContent.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(unsafe.Pointer(stream)),
	)
	runtime.KeepAlive(stream)
	return rv
}

// --- ICoreWebView2HttpRequestHeaders ---

type iCoreWebView2HttpRequestHeaders struct {
	vtbl *iCoreWebView2HttpRequestHeadersVtbl
}

// Vtable layout from WebView2.h ICoreWebView2HttpRequestHeaders.
type iCoreWebView2HttpRequestHeadersVtbl struct {
	_IUnknownVtbl
	GetHeader  ComProc // 3
	GetHeaders ComProc // 4
	Contains   ComProc // 5
	SetHeader  ComProc // 6
	RemoveHeader ComProc // 7
}

func (h *iCoreWebView2HttpRequestHeaders) GetHeader(name string) string {
	pName := utf16PtrFromStr(name)
	var pwstr *uint16
	h.vtbl.GetHeader.Call(
		uintptr(unsafe.Pointer(h)),
		uintptr(unsafe.Pointer(pName)),
		uintptr(unsafe.Pointer(&pwstr)),
	)
	runtime.KeepAlive(pName)
	if pwstr == nil {
		return ""
	}
	s := wideToString(pwstr)
	pCoTaskMemFree.Call(uintptr(unsafe.Pointer(pwstr)))
	return s
}

// --- ICoreWebView2Deferral ---

type iCoreWebView2Deferral struct {
	vtbl *iCoreWebView2DeferralVtbl
}

// Vtable layout from WebView2.h ICoreWebView2Deferral.
type iCoreWebView2DeferralVtbl struct {
	_IUnknownVtbl
	Complete ComProc // 3
}

func (d *iCoreWebView2Deferral) Complete() uintptr {
	r, _, _ := d.vtbl.Complete.Call(uintptr(unsafe.Pointer(d)))
	return r
}

// --- IStream (real COM IStream via CreateStreamOnHGlobal) ---

type iStream struct {
	vtbl *_IStreamVtbl
}

type _IStreamVtbl struct {
	_IUnknownVtbl
	Read    ComProc // 3
	Write   ComProc // 4
	Seek    ComProc // 5
	SetSize ComProc // 6
	CopyTo  ComProc // 7
	Commit  ComProc // 8
	Revert  ComProc // 9
	LockRegion   ComProc // 10
	UnlockRegion ComProc // 11
	Stat    ComProc // 12
	Clone   ComProc // 13
}

// createStreamOnHGlobal creates a real COM IStream backed by global memory.
func createStreamOnHGlobal(data []byte) *iStream {
	var stream *iStream
	r, _, _ := pCreateStreamOnHGlobal.Call(0, 1, uintptr(unsafe.Pointer(&stream)))
	if r != S_OK || stream == nil {
		return nil
	}
	if len(data) > 0 {
		// Write data to the stream.
		var written uint32
		stream.vtbl.Write.Call(
			uintptr(unsafe.Pointer(stream)),
			uintptr(unsafe.Pointer(&data[0])),
			uintptr(len(data)),
			uintptr(unsafe.Pointer(&written)),
		)
		// Seek back to the beginning.
		var li int64
		stream.vtbl.Seek.Call(
			uintptr(unsafe.Pointer(stream)),
			0, // offset
			0, // STREAM_SEEK_SET
			uintptr(unsafe.Pointer(&li)),
		)
	}
	return stream
}

// --- ICoreWebView2WebResourceRequestedEventHandler (COM callback) ---

type iCoreWebView2WebResourceRequestedEventHandler struct {
	vtbl *iCoreWebView2WebResourceRequestedEventHandlerVtbl
	impl webResourceRequestedImpl
}

type iCoreWebView2WebResourceRequestedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type webResourceRequestedImpl interface {
	InvokeWebResourceRequested(sender *iCoreWebView2, args *iCoreWebView2WebResourceRequestedEventArgs) uintptr
}

var webResourceRequestedVtblSingleton = iCoreWebView2WebResourceRequestedEventHandlerVtbl{
	_IUnknownVtbl: _IUnknownVtbl{
		QueryInterface: NewComProc(webResourceRequestedQueryInterface),
		AddRef:         NewComProc(comAddRef),
		Release:        NewComProc(comRelease),
	},
	Invoke: NewComProc(webResourceRequestedInvoke),
}

func webResourceRequestedQueryInterface(this *iCoreWebView2WebResourceRequestedEventHandler, iid *GUID, out **uintptr) uintptr {
	if out != nil {
		*out = (*uintptr)(unsafe.Pointer(this))
	}
	comAddRef((*_IUnknown)(unsafe.Pointer(this)))
	return S_OK
}

func webResourceRequestedInvoke(this *iCoreWebView2WebResourceRequestedEventHandler, sender *iCoreWebView2, args *iCoreWebView2WebResourceRequestedEventArgs) uintptr {
	return this.impl.InvokeWebResourceRequested(sender, args)
}

func newWebResourceRequestedHandler(impl webResourceRequestedImpl) *iCoreWebView2WebResourceRequestedEventHandler {
	return &iCoreWebView2WebResourceRequestedEventHandler{
		vtbl: &webResourceRequestedVtblSingleton,
		impl: impl,
	}
}

// --- Helper methods on existing types ---

func (e *iCoreWebView2Environment) CreateWebResourceResponse(
	reasonPhrase string, statusCode int, headers string, content *iStream,
) *iCoreWebView2WebResourceResponse {
	pPhrase := utf16PtrFromStr(reasonPhrase)
	pHeaders := utf16PtrFromStr(headers)
	var resp *iCoreWebView2WebResourceResponse
	e.vtbl.CreateWebResourceResponse.Call(
		uintptr(unsafe.Pointer(e)),
		uintptr(unsafe.Pointer(content)),
		uintptr(statusCode),
		uintptr(unsafe.Pointer(pPhrase)),
		uintptr(unsafe.Pointer(pHeaders)),
		uintptr(unsafe.Pointer(&resp)),
	)
	runtime.KeepAlive(pPhrase)
	runtime.KeepAlive(pHeaders)
	runtime.KeepAlive(content)
	return resp
}

func (w *iCoreWebView2) AddWebResourceRequested(
	handler *iCoreWebView2WebResourceRequestedEventHandler,
	outToken *eventToken,
) uintptr {
	r, _, _ := w.vtbl.AddWebResourceRequested.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(handler)),
		uintptr(unsafe.Pointer(outToken)),
	)
	return r
}

const webResourceContextAll = 0

func (w *iCoreWebView2) AddWebResourceRequestedFilter(
	pattern string, resourceContext uintptr,
) uintptr {
	p := utf16PtrFromStr(pattern)
	r, _, _ := w.vtbl.AddWebResourceRequestedFilter.Call(
		uintptr(unsafe.Pointer(w)),
		uintptr(unsafe.Pointer(p)),
		resourceContext,
	)
	runtime.KeepAlive(p)
	return r
}
