package browser

import (
	"strings"
	"time"

	"browser/internal/engine"
)

type jsRuntime = engine.JSRuntime
type jsImportTask = engine.ImportTask
type FormSubmission = engine.FormSubmission

// newJSRuntime creates the engine runtime behind the browser host adapter.
func newJSRuntime(b *Browser, page *Page) *jsRuntime {
	j := engine.NewJSRuntime(newJSHost(b), page)
	if b != nil {
		j.SetMemLimit(b.JSMemLimit)
	}
	return j
}

const (
	jsPageBudget = 8 * time.Second
	jsMaxScripts = 2000
)

func removeNoscript(root *Node)                 { engine.RemoveNoscript(root) }
func setAttr(n *Node, name, value string)       { engine.SetAttr(n, name, value) }
func removeAttr(n *Node, name string)           { engine.RemoveAttr(n, name) }
func serializeNode(b *strings.Builder, n *Node) { engine.SerializeNode(b, n) }

func textEscape(s string) string { return engine.TextEscape(s) }

func attrValue(n *Node, name string) string { return engine.AttrValue(n, name) }
