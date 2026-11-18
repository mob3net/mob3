package browser

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestEscapeStopsDeferredImageLoad(t *testing.T) {
	doc := ParseHTML(`<html><body><img src="pic.png" alt="pic"></body></html>`)
	u := mustParse(t, "https://example.com/")
	loader := NewLoader()
	loader.noImages = false
	loader.deferImages = true
	b := NewBrowser(60, 20)
	b.loader = loader
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.loadSeq = 1
	b.deferredImages = true
	b.imageCancel = func() {}

	b.HandleKey("esc")
	if b.deferredImages {
		t.Fatal("Esc left the deferred image load active")
	}
	if !b.imagesPaused {
		t.Fatal("Esc did not pause deferred images")
	}
	if b.status != "Stopped loading images" {
		t.Fatalf("status = %q", b.status)
	}

	// A later timer-driven rebuild must not restart the paused batch.
	b.js = &jsRuntime{}
	b.rebuild()
	if b.deferredImages {
		t.Fatal("rebuild restarted a paused image load")
	}
}

func TestDeferredImageBatchesStayActiveUntilDone(t *testing.T) {
	doc := ParseHTML(`<html><body><img src="pic.png" alt="pic"></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := NewBrowser(60, 20)
	b.loader = NewLoader()
	b.loader.noImages = false
	b.page = &Page{URL: u, Base: u, Root: doc}
	b.url = u
	b.loadSeq = 1
	b.deferredImages = true
	img := doc.FindFirst("img")
	if img == nil {
		t.Fatal("image missing")
	}
	batch := imageBatch{seq: 1, results: []imageLoadResult{{
		Node: img, Src: "pic.png", Img: image.NewRGBA(image.Rect(0, 0, 8, 8)),
	}}}
	if !b.applyDeferredImageBatch(batch) {
		t.Fatal("partial batch did not apply")
	}
	if !b.deferredImages {
		t.Fatal("a partial batch cleared the deferred state")
	}
	if !b.applyDeferredImageBatch(imageBatch{seq: 1, done: true}) {
		t.Fatal("done batch did not request a progress repaint")
	}
	if b.deferredImages {
		t.Fatal("the done batch did not finish the deferred load")
	}
}

func TestImageTasksPrioritiseViewport(t *testing.T) {
	doc := ParseHTML(`<html><body>` +
		`<img id="d" src="d.png"><img id="c" src="c.png">` +
		`<img id="b" src="b.png"><img id="a" src="a.png"></body></html>`)
	byID := map[string]*Node{}
	doc.FindAll(func(n *Node) bool {
		if n.Tag == "img" {
			byID[n.Attr("id")] = n
		}
		return false
	})
	b := NewBrowser(80, 20)
	b.scroll = 0
	b.layout = &Layout{Frags: []Frag{
		{Kind: fragText, Node: byID["d"], Y: 100},
		{Kind: fragText, Node: byID["c"], Y: 25},
		{Kind: fragText, Node: byID["b"], Y: 5},
		{Kind: fragText, Node: byID["a"], Y: 0},
	}}
	tasks := []imageTask{
		{node: byID["d"], src: "d.png"},
		{node: byID["c"], src: "c.png"},
		{node: byID["b"], src: "b.png"},
		{node: byID["a"], src: "a.png"},
	}
	b.sortImageTasks(tasks)
	var got []string
	for _, task := range tasks {
		got = append(got, task.node.Attr("id"))
	}
	// a/b are visible (stable DOM order puts b before a), c is one screen
	// away, d is far down the document.
	want := []string{"b", "a", "c", "d"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("priority order = %v, want %v", got, want)
		}
	}
}

func TestDeferredImageProgressBar(t *testing.T) {
	b := NewBrowser(60, 20)
	b.page = &Page{Root: ParseHTML(`<html><body></body></html>`)}
	b.deferredImages = true
	b.imageTotal = 10
	b.progress = 0.9
	if !b.applyDeferredImageBatch(imageBatch{seq: b.loadSeq, results: make([]imageLoadResult, 4)}) {
		t.Fatal("partial batch did not request a repaint")
	}
	if b.progress < 0.93 || b.progress > 0.94 {
		t.Fatalf("partial image progress = %v, want about 0.936", b.progress)
	}
	if !b.deferredImages {
		t.Fatal("partial batch finished the load")
	}
	if !b.applyDeferredImageBatch(imageBatch{seq: b.loadSeq, results: make([]imageLoadResult, 6), done: true}) {
		t.Fatal("done batch did not request a repaint")
	}
	if b.progress != 1 || b.progressHold == 0 {
		t.Fatalf("finished image progress = %v hold=%d, want complete", b.progress, b.progressHold)
	}
	if b.deferredImages {
		t.Fatal("done batch left the deferred load active")
	}
}

func TestFooterShowsImageLoadingCount(t *testing.T) {
	b := NewBrowser(60, 20)
	b.deferredImages = true
	b.imageTotal = 5
	b.imageDone = 2
	if got := b.footerLeft(); got != " Loading images 2/5" {
		t.Fatalf("footer = %q", got)
	}
	b.deferredImages = false
	if got := b.footerLeft(); got != " Ready" {
		t.Fatalf("footer after images = %q", got)
	}
}

func TestRefreshImagesAfterScript(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	page := newJSPage(`<html><body><img id="x" alt="lazy"></body></html>`,
		`document.getElementById("x").src = "`+dataURL+`";`)
	b := NewBrowser(60, 20)
	b.loader.noImages = false
	b.page = page
	b.url = page.URL
	b.startJS(page)
	if !b.refreshImages() {
		t.Fatal("post-script images were not discovered")
	}
	img := page.Root.FindFirst("img")
	if img == nil || img.Img == nil {
		t.Fatal("image set by script was not loaded")
	}
	if !strings.HasPrefix(img.ImgSrc, "data:image/png;base64,") {
		t.Fatalf("image source = %q", img.ImgSrc)
	}
	if b.refreshImages() {
		t.Fatal("second refresh reloaded an unchanged image")
	}
}
