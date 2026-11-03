//go:build windows

package windows

import (
	"fmt"
	"runtime"
	"unsafe"
)

func (p *Platform) bootstrapScript() string {
	var names []string
	if p.BoundFuncs != nil {
		names = p.BoundFuncs()
	}
	src := bootstrapJS(names)
	if p.BindingScript != nil {
		src += p.BindingScript()
	}
	return src
}

func (p *Platform) RefreshBindings() error {
	p.mu.Lock()
	ready := p.webview != nil && !p.closed
	p.mu.Unlock()
	if !ready {
		return nil
	}
	var err error
	p.MainThread(func() {
		err = p.rebuildBindingScript()
		if err == nil {
			p.webview.ExecuteScript(p.bootstrapScript(), 0)
		}
	})
	return err
}

// Called on the host thread. Re-register bindings before the separate Init
// scripts, preserving their directive prologues and independent execution.
func (p *Platform) rebuildBindingScript() error {
	sources := []string{p.bootstrapScript()}
	p.mu.Lock()
	sources = append(sources, p.userScriptSrcs...)
	p.mu.Unlock()
	p.bindingScriptGeneration++
	for _, id := range p.documentScriptIDs {
		p.removeBindingScript(id)
	}
	p.documentScriptIDs = nil
	for _, src := range sources {
		handler := &scriptCompletedHandler{vtbl: &scriptCompletedVtbl, p: p, generation: p.bindingScriptGeneration}
		if p.bindingScriptHandlers == nil {
			p.bindingScriptHandlers = make(map[*scriptCompletedHandler]struct{})
		}
		p.bindingScriptHandlers[handler] = struct{}{}
		hr := p.webview.AddScriptToExecuteOnDocumentCreated(src, uintptr(unsafe.Pointer(handler)))
		if hr != S_OK {
			delete(p.bindingScriptHandlers, handler)
			return fmt.Errorf("webview: register bindings script failed: 0x%X", hr)
		}
	}
	return nil
}

type scriptCompletedHandler struct {
	vtbl       *scriptCompletedHandlerVtbl
	p          *Platform
	generation uint64
}
type scriptCompletedHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

var scriptCompletedVtbl = scriptCompletedHandlerVtbl{
	_IUnknownVtbl: _IUnknownVtbl{
		QueryInterface: NewComProc(scriptCompletedQueryInterface), AddRef: NewComProc(comAddRef), Release: NewComProc(comRelease),
	},
	Invoke: NewComProc(scriptCompletedInvoke),
}

func scriptCompletedQueryInterface(this *scriptCompletedHandler, iid *GUID, out **uintptr) uintptr {
	if out != nil {
		*out = (*uintptr)(unsafe.Pointer(this))
	}
	return S_OK
}
func scriptCompletedInvoke(this *scriptCompletedHandler, errorCode uintptr, id *uint16) uintptr {
	p := this.p
	defer delete(p.bindingScriptHandlers, this)
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed || errorCode != S_OK {
		return S_OK
	}
	added := wideToString(id)
	if this.generation != p.bindingScriptGeneration {
		p.removeBindingScript(added)
		return S_OK
	}
	p.documentScriptIDs = append(p.documentScriptIDs, added)
	return S_OK
}
func (p *Platform) removeBindingScript(id string) {
	value := utf16PtrFromStr(id)
	p.webview.vtbl.RemoveScriptToExecuteOnDocumentCreated.Call(uintptr(unsafe.Pointer(p.webview)), uintptr(unsafe.Pointer(value)))
	runtime.KeepAlive(value)
}
