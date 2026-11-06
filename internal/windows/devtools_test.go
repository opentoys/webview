//go:build windows

package windows

import (
	"strings"
	"testing"
)

func TestShowDevToolsRequiresDebugAndRunningWindow(t *testing.T) {
	p := &Platform{}
	if err := p.ShowDevTools(); err == nil || !strings.Contains(err.Error(), "Debug") {
		t.Fatalf("disabled: %v", err)
	}
	p.Debug = true
	if err := p.ShowDevTools(); err == nil || !strings.Contains(err.Error(), "running window") {
		t.Fatalf("not started: %v", err)
	}
}
