package browser

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func mcpResponses(t *testing.T, out string) map[int]map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(out))
	resps := make(map[int]map[string]any)
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			break
		}
		id, _ := m["id"].(float64)
		resps[int(id)] = m
	}
	return resps
}

func mcpText(t *testing.T, resp map[string]any) string {
	t.Helper()
	result, _ := resp["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		return ""
	}
	item, _ := content[0].(map[string]any)
	text, _ := item["text"].(string)
	return text
}

func TestMCPProtocol(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><head><title>Home</title></head><body>
			<h1>Welcome</h1><a href="/next">Next page</a>
		</body></html>`,
		"/next": `<html><body><h1>Second page</h1></body></html>`,
	})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"navigate","arguments":{"url":"https://example.com/"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"snapshot","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"click","arguments":{"link":1}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"evaluate","arguments":{"expression":"1+1"}}}`,
		`{"jsonrpc":"2.0","id":7,"method":"ping","params":{}}`,
		`{"jsonrpc":"2.0","id":8,"method":"no.such.method","params":{}}`,
	}, "\n")
	var out, errOut bytes.Buffer
	if code := RunMCP(a, strings.NewReader(input), &out, &errOut); code != 0 {
		t.Fatalf("RunMCP = %d: %s", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q", errOut.String())
	}
	resps := mcpResponses(t, out.String())
	if len(resps) != 8 {
		t.Fatalf("responses = %d, want 8; out=%s", len(resps), out.String())
	}
	init, _ := resps[1]["result"].(map[string]any)
	if init["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocol = %v", init["protocolVersion"])
	}
	tools, _ := resps[2]["result"].(map[string]any)["tools"].([]any)
	if len(tools) < 10 {
		t.Fatalf("tools = %d", len(tools))
	}
	nav := mcpText(t, resps[3])
	if !strings.Contains(nav, "Welcome") || !strings.Contains(nav, "[1] Next page") {
		t.Fatalf("navigate result = %q", nav)
	}
	if got := mcpText(t, resps[4]); !strings.Contains(got, "Links:") {
		t.Fatalf("snapshot result = %q", got)
	}
	if got := mcpText(t, resps[5]); !strings.Contains(got, "Second page") {
		t.Fatalf("click result = %q", got)
	}
	if got := mcpText(t, resps[6]); got != "2" {
		t.Fatalf("evaluate = %q", got)
	}
	if err, _ := resps[8]["error"].(map[string]any); err == nil || err["code"] != float64(-32601) {
		t.Fatalf("unknown method response = %+v", resps[8])
	}
}

func TestMCPToolErrorsAndParseErrors(t *testing.T) {
	a := newTestAutomation(t, map[string]string{"/": `<html><body><p>x</p></body></html>`})
	input := strings.Join([]string{
		`not json`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"click","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"no-such-tool","arguments":{}}}`,
	}, "\n")
	var out, errOut bytes.Buffer
	if code := RunMCP(a, strings.NewReader(input), &out, &errOut); code != 0 {
		t.Fatalf("RunMCP = %d: %s", code, errOut.String())
	}
	resps := mcpResponses(t, out.String())
	if len(resps) != 3 {
		t.Fatalf("responses = %d, want 3: %s", len(resps), out.String())
	}
	parse, _ := resps[0]["error"].(map[string]any)
	if parse["code"] != float64(-32700) {
		t.Fatalf("parse response = %+v", resps[0])
	}
	for _, id := range []int{1, 2} {
		result, _ := resps[id]["result"].(map[string]any)
		if result["isError"] != true {
			t.Fatalf("tool %d not marked as error: %+v", id, resps[id])
		}
	}
}

func TestMCPClickType(t *testing.T) {
	a := newTestAutomation(t, map[string]string{
		"/": `<html><body><input name="q" value=""></body></html>`,
	})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"navigate","arguments":{"url":"https://example.com/"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"click","arguments":{"selector":"input[name=q]"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"type","arguments":{"text":"hello"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"evaluate","arguments":{"expression":"document.querySelector('input').value"}}}`,
	}, "\n")
	var out, errOut bytes.Buffer
	if code := RunMCP(a, strings.NewReader(input), &out, &errOut); code != 0 {
		t.Fatalf("RunMCP = %d: %s", code, errOut.String())
	}
	resps := mcpResponses(t, out.String())
	if got := mcpText(t, resps[4]); got != "hello" {
		t.Fatalf("input value = %q", got)
	}
}
