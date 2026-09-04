//go:build darwin

package darwin

// WKURLSchemeTask response conversion.

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

// fetchShimScript bypasses WKURLSchemeTask's missing POST-body bug for fetch
// requests targeting a registered custom scheme. It leaves all other fetches
// on the browser's native implementation.
func fetchShimScript(handlers map[string]ResourceHandler) string {
	schemes := make([]string, 0, len(handlers))
	for scheme := range handlers {
		schemes = append(schemes, scheme)
	}
	encoded, _ := json.Marshal(schemes)
	return `(function(){const schemes=new Set(` + string(encoded) + `);const nativeFetch=window.fetch.bind(window);let next=0;const pending=new Map();window.__webviewFetchResolve=function(id,ok,p){const x=pending.get(id);if(!x)return;pending.delete(id);if(!ok){x.reject(new TypeError(p));return}const bin=atob(p.body||'');const bytes=new Uint8Array(bin.length);for(let i=0;i<bin.length;i++)bytes[i]=bin.charCodeAt(i);x.resolve(new Response(bytes,{status:p.status,headers:p.headers}))};window.fetch=async function(input,init){const req=new Request(input,init);const scheme=new URL(req.url).protocol.slice(0,-1);if(!schemes.has(scheme))return nativeFetch(input,init);const bytes=new Uint8Array(await req.arrayBuffer());let bin='';for(let i=0;i<bytes.length;i++)bin+=String.fromCharCode(bytes[i]);const id=++next;return new Promise((resolve,reject)=>{pending.set(id,{resolve,reject});window.webkit.messageHandlers.webviewBridge.postMessage(JSON.stringify({__webviewFetch:{id:id,url:req.url,method:req.method,headers:Object.fromEntries(req.headers),body:btoa(bin)}}))})}})();`
}

func (p *Platform) handleFetchShimMessage(raw string) bool {
	var msg struct {
		Fetch *struct {
			ID      int               `json:"id"`
			URL     string            `json:"url"`
			Method  string            `json:"method"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
		} `json:"__webviewFetch"`
	}
	if json.Unmarshal([]byte(raw), &msg) != nil || msg.Fetch == nil {
		return false
	}
	f := msg.Fetch
	data, err := base64.StdEncoding.DecodeString(f.Body)
	if err != nil {
		p.EvalHost(`window.__webviewFetchResolve(` + strconv.Itoa(f.ID) + `,false,"invalid request body")`)
		return true
	}
	scheme := strings.SplitN(f.URL, ":", 2)[0]
	handler := p.schemeHandlers[scheme]
	if handler == nil {
		p.EvalHost(`window.__webviewFetchResolve(` + strconv.Itoa(f.ID) + `,false,"unhandled scheme")`)
		return true
	}
	headers := http.Header{}
	for k, v := range f.Headers {
		headers.Set(k, v)
	}
	handler(ResourceRequest{URL: f.URL, Method: f.Method, Headers: headers, Body: data}, func(resp *ResourceResponse) {
		if resp == nil {
			p.EvalHost(`window.__webviewFetchResolve(` + strconv.Itoa(f.ID) + `,false,"resource not found")`)
			return
		}
		out := struct {
			Status  int         `json:"status"`
			Headers http.Header `json:"headers"`
			Body    string      `json:"body"`
		}{resp.StatusCode, resp.Headers, base64.StdEncoding.EncodeToString(resp.Body)}
		if out.Status == 0 {
			out.Status = http.StatusOK
		}
		payload, _ := json.Marshal(out)
		p.EvalHost(`window.__webviewFetchResolve(` + strconv.Itoa(f.ID) + `,true,` + string(payload) + `)`)
	})
	return true
}

func looksLikeHTML(body []byte) bool {
	s := strings.TrimSpace(string(body))
	s = strings.ToLower(s)
	return strings.HasPrefix(s, "<!doctype") || strings.HasPrefix(s, "<html") || strings.HasPrefix(s, "<head")
}

// respondToSchemeTask sends a ResourceResponse to a WKURLSchemeTask. Must be
// called on the host thread.
func respondToSchemeTask(task objc.ID, resp ResourceResponse, reqURL objc.ID) {
	if resp.StatusCode == 0 {
		resp.StatusCode = 200
	}

	// Inject bootstrap JS into HTML responses so Bind works on custom schemes.
	ct := resp.Headers.Get("Content-Type")
	if ct == "" {
		ct = resp.Headers.Get("content-type")
	}
	if strings.HasPrefix(ct, "text/html") || (ct == "" && len(resp.Body) > 0 && looksLikeHTML(resp.Body)) {
		resp.Body = []byte(prependBootstrap(string(resp.Body)))
	}

	// Build header NSDictionary.
	var hdrDict objc.ID
	if len(resp.Headers) > 0 {
		hdrDict = nsDictionary(resp.Headers)
	}

	// Create NSHTTPURLResponse.
	httpResp := objc.ID(nsHTTPURLResponseClass).Send(allocSel)
	httpResp = httpResp.Send(initWithURLStatusCodeHTTPVersionHeaderFieldsSel,
		reqURL, uintptr(resp.StatusCode), nsString("HTTP/1.1"), hdrDict)
	if httpResp != 0 {
		task.Send(didReceiveResponseSel, httpResp)
	}

	// Send body data.
	if len(resp.Body) > 0 {
		body := resp.Body
		nsData := objc.ID(nsDataClass).Send(dataWithBytesLengthSel,
			unsafe.Pointer(&body[0]), uintptr(len(body)))
		if nsData != 0 {
			task.Send(didReceiveDataSel, nsData)
		}
	}

	// Finish.
	task.Send(schemeFinishSel)
}

// nsDictionary creates an NSDictionary from a Go map[string]string.
func nsDictionary(m http.Header) objc.ID {
	if len(m) == 0 {
		return 0
	}
	keys := make([]objc.ID, 0, len(m))
	vals := make([]objc.ID, 0, len(m))
	for k, v := range m {
		keys = append(keys, nsString(k))
		vals = append(vals, nsString(strings.Join(v, ";")))
	}
	return objc.ID(nsDictionaryClass).Send(dictionaryWithObjectsForKeysCountSel,
		unsafe.Pointer(&vals[0]), unsafe.Pointer(&keys[0]), uintptr(len(m)))
}

// goMapFromNSDictionary converts an NSDictionary (NSString→NSString) to a Go map.
// Uses objectForKey: with known keys or enumerates via block. Simplified: reads
// allKeys and iterates.
func goMapFromNSDictionary(dict objc.ID) http.Header {
	out := http.Header{}
	if dict == 0 {
		return out
	}
	// allKeys returns NSArray of keys.
	allKeysSel := objc.RegisterName("allKeys")
	keys := objc.ID(dict).Send(allKeysSel)
	if keys == 0 {
		return out
	}
	countSel := objc.RegisterName("count")
	objectAtIndexSel := objc.RegisterName("objectAtIndex:")
	n := int(keys.Send(countSel))
	for i := 0; i < n; i++ {
		key := objc.ID(keys).Send(objectAtIndexSel, uintptr(i))
		val := objc.ID(dict).Send(objc.RegisterName("objectForKey:"), key)
		if key != 0 && val != 0 {
			out.Add(goString(key), goString(val))
		}
	}
	return out
}
