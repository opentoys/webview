package bridgejs

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestExecutableProxies(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute JavaScript tests")
	}
	object := Proxies([]Binding{
		{Name: "app", Methods: []string{"Greet", "Old", "__proto__"}},
		{Name: "app.Greet"},
		{Name: "__proto__", Methods: []string{"Say"}},
		{Name: "quote\".\n"},
	})
	replacement := Proxies([]Binding{{Name: "app", Methods: []string{"New"}}})
	function := Proxies([]Binding{{Name: "app"}})
	script := `const assert = require('node:assert/strict');
 global.window = global;
 const messages=[];
 const send=msg=>messages.push(JSON.parse(JSON.stringify(msg)));
 ` + Transport(`send(msg);`) + object + `
 (async () => {
 assert.equal(Object.getPrototypeOf(window), Object.getPrototypeOf(global));
 assert.ok(Object.hasOwn(window,'__proto__'));
 assert.ok(Object.hasOwn(app,'__proto__'));
 const original = app;
 const first = app.Greet('张三');
 assert.deepEqual(messages.pop(),{id:1,name:'app',method:'Greet',args:['张三']});
 const literal = window['app.Greet']();
 assert.deepEqual(messages.pop(),{id:2,name:'app.Greet',args:[]});
 const transport = window.webviewBridge;
 ` + Transport(`send(msg);`) + replacement + `
 assert.equal(window.webviewBridge,transport);
 assert.deepEqual(Object.keys(app),['New']);
 assert.equal(typeof window['app.Greet'],'undefined');
 assert.equal(Object.hasOwn(window,'__proto__'),false);
 assert.equal(Object.hasOwn(window,'quote".\n'),false);
 transport.resolve(1,'done');
 transport.resolve(2,'literal');
 assert.equal(await first,'done');
 assert.equal(await literal,'literal');
 const old = original.Old();
 assert.deepEqual(messages.pop(),{id:3,name:'app',method:'Old',args:[]});
 transport.reject(3,'removed');
 await assert.rejects(old,/removed/);
 ` + function + `
 assert.equal(typeof app,'function');
 const fn = app(42);
 assert.deepEqual(messages.pop(),{id:4,name:'app',args:[42]});
 transport.resolve(4,42);
 assert.equal(await fn,42);
 ` + object + `
 assert.equal(typeof app.Greet,'function');
 window.app = 'page owned';
 ` + Proxies(nil) + `
 assert.equal(window.app,'page owned');
 assert.deepEqual(Object.keys(transport._pending),[]);
 })().catch(err=>{
 console.error(err);
 process.exitCode=1
});`
	cmd := exec.Command(node, "-")
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("JavaScript failed: %v\n%s", err, output)
	}
}

func TestExecutableReadOnlyProperties(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	src := Proxies([]Binding{{Name: "app", Methods: []string{"Greet"}, Properties: map[string]json.RawMessage{
		"Name":      json.RawMessage(`"张三"`),
		"nested":    json.RawMessage(`{"__proto__":{"safe":true},"list":[1,2]}`),
		"__proto__": json.RawMessage(`"own"`),
		"quote\".":  json.RawMessage(`null`),
	}}})
	script := `'use strict';
 const assert=require('node:assert/strict');
 global.window=global;` + src + `
 assert.equal(app.Name,'张三');
 assert.equal(app['__proto__'],'own');
 assert.equal(Object.getPrototypeOf(app),Object.prototype);
 assert.ok(Object.hasOwn(app.nested,'__proto__'));
 assert.equal(app.nested.__proto__.safe,true);
 assert.equal(Object.getPrototypeOf(app.nested),Object.prototype);
 assert.throws(()=>{app.Name='other'},TypeError);
 assert.throws(()=>{delete app.Name},TypeError);
 assert.throws(()=>{app.nested.list.push(3)},TypeError);
 assert.throws(()=>{app.nested.__proto__.safe=false},TypeError);
 assert.equal(typeof app.Greet,'function');
 ` + Proxies([]Binding{{
		Name:       "app",
		Methods:    []string{},
		Properties: map[string]json.RawMessage{"Name": json.RawMessage(`"李四"`)},
	}}) + `
 assert.equal(app.Name,'李四');
 assert.equal(typeof app.Greet,'undefined');
 ` + Proxies(nil) + `assert.equal(typeof window.app,'undefined');`
	cmd := exec.Command(node, "-")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}
