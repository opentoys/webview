//go:build darwin

package darwin

// WKURLSchemeTask response conversion.

import (
	"net/http"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego/objc"
)

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
