package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"
)

type fakeAutomation struct {
	nav string
}

func (f *fakeAutomation) Navigate(_ context.Context, target string) error {
	f.nav = target
	return nil
}
func (f *fakeAutomation) Snapshot() string { return "URL: " + f.nav + "\nLinks:\n[1] next" }
func (f *fakeAutomation) Screenshot(int) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
func (f *fakeAutomation) RunCommand(_ context.Context, name string, args json.RawMessage) (string, error) {
	switch name {
	case "navigate":
		var p struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(args, &p)
		f.nav = p.URL
		return f.Snapshot(), nil
	case "snapshot":
		return f.Snapshot(), nil
	case "evaluate":
		return "2", nil
	default:
		return "", errUnknownTool(name)
	}
}

type unknownTool string

func (e unknownTool) Error() string    { return "unknown tool " + string(e) }
func errUnknownTool(name string) error { return unknownTool(name) }

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
	a := &fakeAutomation{}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"navigate","arguments":{"url":"https://example.com/"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"snapshot","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"evaluate","arguments":{"expression":"1+1"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"ping","params":{}}`,
		`{"jsonrpc":"2.0","id":7,"method":"no.such.method","params":{}}`,
	}, "\n")
	var out, errOut bytes.Buffer
	if code := RunMCP(a, strings.NewReader(input), &out, &errOut); code != 0 {
		t.Fatalf("RunMCP = %d: %s", code, errOut.String())
	}
	resps := mcpResponses(t, out.String())
	if len(resps) != 7 {
		t.Fatalf("responses = %d, want 7; out=%s", len(resps), out.String())
	}
	if got := mcpText(t, resps[4]); !strings.Contains(got, "Links:") || !strings.Contains(got, "example.com") {
		t.Fatalf("snapshot = %q", got)
	}
	if got := mcpText(t, resps[5]); got != "2" {
		t.Fatalf("evaluate = %q", got)
	}
	if err, _ := resps[7]["error"].(map[string]any); err == nil || err["code"] != float64(-32601) {
		t.Fatalf("unknown method = %+v", resps[7])
	}
}

func TestMCPScreenshotAndErrors(t *testing.T) {
	a := &fakeAutomation{}
	input := strings.Join([]string{
		`not json`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"screenshot","arguments":{"scale":1}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"no-such-tool","arguments":{}}}`,
	}, "\n")
	var out, errOut bytes.Buffer
	if code := RunMCP(a, strings.NewReader(input), &out, &errOut); code != 0 {
		t.Fatalf("RunMCP = %d: %s", code, errOut.String())
	}
	resps := mcpResponses(t, out.String())
	if parse, _ := resps[0]["error"].(map[string]any); parse["code"] != float64(-32700) {
		t.Fatalf("parse = %+v", resps[0])
	}
	result, _ := resps[1]["result"].(map[string]any)
	content, _ := result["content"].([]any)
	img, _ := content[0].(map[string]any)
	if img["type"] != "image" || img["mimeType"] != "image/png" {
		t.Fatalf("image = %+v", img)
	}
	raw, err := base64.StdEncoding.DecodeString(img["data"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(raw)); err != nil {
		t.Fatalf("bad PNG: %v", err)
	}
	result, _ = resps[2]["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("unknown tool not marked error: %+v", resps[2])
	}
}
