package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEscapeStopsDeferredImageLoad(t *testing.T) {
	doc := ParseHTML(`<html><body><img src="pic.png" alt="pic"></body></html>`)
	u := mustParse(t, "https://example.com/")
	loader := newTestLoader()
	loader.NoImages = false
	loader.DeferImageLoad = true
	b := newTestSession(60, 20)
	b.loader = loader
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.LoadSeq = 1
	b.DeferredImages = true
	b.ImageCancel = func() {}

	b.HandleKey("esc")
	if b.DeferredImages {
		t.Fatal("Esc left the deferred image load active")
	}
	if !b.ImagesPaused {
		t.Fatal("Esc did not pause deferred images")
	}
	if b.status != "Stopped loading images" {
		t.Fatalf("status = %q", b.status)
	}

	// A later timer-driven rebuild must not restart the paused batch.
	b.JS = &jsRuntime{}
	b.rebuild()
	if b.DeferredImages {
		t.Fatal("rebuild restarted a paused image load")
	}
}

func TestDeferredImageBatchesStayActiveUntilDone(t *testing.T) {
	doc := ParseHTML(`<html><body><img src="pic.png" alt="pic"></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := newTestSession(60, 20)
	b.loader = newTestLoader()
	b.loader.NoImages = false
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.LoadSeq = 1
	b.DeferredImages = true
	img := doc.FindFirst("img")
	if img == nil {
		t.Fatal("image missing")
	}
	batch := imageBatch{Seq: 1, Results: []imageLoadResult{{
		Node: img, Src: "pic.png", Img: image.NewRGBA(image.Rect(0, 0, 8, 8)),
	}}}
	if !b.applyDeferredImageBatch(batch) {
		t.Fatal("partial batch did not apply")
	}
	if !b.DeferredImages {
		t.Fatal("a partial batch cleared the deferred state")
	}
	if !b.applyDeferredImageBatch(imageBatch{Seq: 1, Done: true}) {
		t.Fatal("done batch did not request a progress repaint")
	}
	if b.DeferredImages {
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
	b := newTestSession(80, 20)
	b.Scroll = 0
	b.Layout = &Layout{Frags: []Frag{
		{Kind: FragText, Node: byID["d"], Y: 100},
		{Kind: FragText, Node: byID["c"], Y: 25},
		{Kind: FragText, Node: byID["b"], Y: 5},
		{Kind: FragText, Node: byID["a"], Y: 0},
	}}
	tasks := []imageTask{
		{Node: byID["d"], Src: "d.png"},
		{Node: byID["c"], Src: "c.png"},
		{Node: byID["b"], Src: "b.png"},
		{Node: byID["a"], Src: "a.png"},
	}
	b.sortImageTasks(tasks)
	var got []string
	for _, task := range tasks {
		got = append(got, task.Node.Attr("id"))
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
	b := newTestSession(60, 20)
	b.Page = &Page{Root: ParseHTML(`<html><body></body></html>`)}
	b.DeferredImages = true
	b.ImageTotal = 10
	b.Progress = 0.9
	if !b.applyDeferredImageBatch(imageBatch{Seq: b.LoadSeq, Results: make([]imageLoadResult, 4)}) {
		t.Fatal("partial batch did not request a repaint")
	}
	if b.Progress < 0.93 || b.Progress > 0.94 {
		t.Fatalf("partial image progress = %v, want about 0.936", b.Progress)
	}
	if !b.DeferredImages {
		t.Fatal("partial batch finished the load")
	}
	if !b.applyDeferredImageBatch(imageBatch{Seq: b.LoadSeq, Results: make([]imageLoadResult, 6), Done: true}) {
		t.Fatal("done batch did not request a repaint")
	}
	if b.Progress != 1 || b.ProgressHold == 0 {
		t.Fatalf("finished image progress = %v hold=%d, want complete", b.Progress, b.ProgressHold)
	}
	if b.DeferredImages {
		t.Fatal("done batch left the deferred load active")
	}
}

func TestFooterShowsImageLoadingCount(t *testing.T) {
	b := newTestSession(60, 20)
	b.DeferredImages = true
	b.ImageTotal = 5
	b.ImageDone = 2
	if got := b.footerLeft(); got != " Loading images 2/5" {
		t.Fatalf("footer = %q", got)
	}
	b.DeferredImages = false
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
	b := newTestSession(60, 20)
	b.loader.NoImages = false
	b.Page = page
	b.URL = page.URL
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

func TestFitImageCapsDimensions(t *testing.T) {
	l := newTestLoader()
	l.SetImageCap(200, 150)
	if b := l.FitImage(image.NewNRGBA(image.Rect(0, 0, 400, 300))).Bounds(); b.Dx() != 200 || b.Dy() != 150 {
		t.Fatalf("square cap = %dx%d, want 200x150", b.Dx(), b.Dy())
	}

	// The narrower side follows the aspect ratio instead of being clamped
	// independently.
	l.SetImageCap(100, 100)
	if b := l.FitImage(image.NewNRGBA(image.Rect(0, 0, 400, 100))).Bounds(); b.Dx() != 100 || b.Dy() != 25 {
		t.Fatalf("wide cap = %dx%d, want 100x25", b.Dx(), b.Dy())
	}

	small := image.NewNRGBA(image.Rect(0, 0, 50, 40))
	if got := l.FitImage(small); got != small {
		t.Fatalf("image inside the cap was resampled: %v", got.Bounds())
	}

	l.SetImageCap(0, 0)
	big := image.NewNRGBA(image.Rect(0, 0, 400, 300))
	if got := l.FitImage(big); got != big {
		t.Fatal("a disabled cap should not resample")
	}
}

func TestImageDownscaledOnLoad(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 400, 300))); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	u := mustParse(t, "https://example.com/")
	root := ParseHTML(`<html><body><img src="` + dataURL + `" alt="pic"></body></html>`)
	loader := newTestLoader()
	loader.NoImages = false
	loader.SetImageCap(100, 100)
	if !loader.LoadImages(context.Background(), root, u) {
		t.Fatal("image was not loaded")
	}
	img := root.FindFirst("img")
	if img == nil || img.Img == nil {
		t.Fatal("decoded image missing")
	}
	if b := img.Img.Bounds(); b.Dx() != 100 || b.Dy() != 75 {
		t.Fatalf("decoded size = %dx%d, want 100x75", b.Dx(), b.Dy())
	}
}

func TestSessionSetsImageCapFromViewport(t *testing.T) {
	b := newTestSession(177, 42)
	if b.loader.ImageCapW() != 177*8 || b.loader.ImageCapH() != 42*16 {
		t.Fatalf("cap = %dx%d, want %dx%d", b.loader.ImageCapW(), b.loader.ImageCapH(), 177*8, 42*16)
	}
	b.Width, b.Height = 80, 24
	b.applyImageCap()
	if b.loader.ImageCapW() != 1024 || b.loader.ImageCapH() != 512 {
		t.Fatalf("small viewport cap = %dx%d, want the 1024x512 floor", b.loader.ImageCapW(), b.loader.ImageCapH())
	}
}

func TestRecoverImageConvertsPanic(t *testing.T) {
	if _, err := recoverImage(func() (image.Image, error) { panic("boom") }); err == nil {
		t.Fatal("decoder panic was not converted to an error")
	}
	want := errors.New("bad image")
	if _, err := recoverImage(func() (image.Image, error) { return nil, want }); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestSafeDecodeRecoversDecoderPanic(t *testing.T) {
	image.RegisterFormat("panic-test", "panicimg",
		func(io.Reader) (image.Image, error) { panic("decode boom") },
		func(io.Reader) (image.Config, error) { panic("config boom") })
	if _, err := SafeDecodeConfig([]byte("panicimg data")); err == nil {
		t.Fatal("config decoder panic was not converted")
	}
	if _, err := SafeDecodeImage([]byte("panicimg data")); err == nil {
		t.Fatal("image decoder panic was not converted")
	}
}

// deferredImageFixture builds a page whose deferred image load is already
// active, so applying a batch never starts a real fetch.
func deferredImageFixture(t *testing.T, html string) *Session {
	t.Helper()
	doc := ParseHTML(html)
	u := mustParse(t, "https://example.com/")
	loader := newTestLoader()
	loader.NoImages = false
	loader.DeferImageLoad = true
	b := newTestSession(60, 20)
	b.loader = loader
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.LoadSeq = 1
	b.DeferredImages = true
	b.rebuild()
	return b
}

func nodeByID(doc *Node, id string) *Node {
	var found *Node
	doc.FindAll(func(n *Node) bool {
		if found == nil && n.Attr("id") == id {
			found = n
		}
		return false
	})
	return found
}

func nodeRow(ly *Layout, n *Node) int {
	for i := range ly.Frags {
		if ly.Frags[i].Node == n {
			return ly.Frags[i].Y
		}
	}
	return -1
}

func TestDeferredImageBelowViewportKeepsLayout(t *testing.T) {
	b := deferredImageFixture(t, `<html><body><p>top</p>`+
		`<div style="height:2000px"></div><img id="far" src="far.png" alt="far"></body></html>`)
	far := nodeByID(b.Page.Root, "far")
	if far == nil {
		t.Fatal("fixture image missing")
	}
	row := nodeRow(b.Layout, far)
	if row <= b.viewportHeight() {
		t.Fatalf("fixture image row %d is not below the fold", row)
	}
	before := b.Layout
	batch := imageBatch{Seq: 1, Results: []imageLoadResult{{
		Node: far, Src: "far.png", Img: image.NewRGBA(image.Rect(0, 0, 8, 8)),
	}}}
	if !b.applyDeferredImageBatch(batch) {
		t.Fatal("below-fold batch did not request the progress repaint")
	}
	if b.Layout != before {
		t.Fatal("below-fold image forced a layout rebuild")
	}
	if b.PendingImageRow != row {
		t.Fatalf("pending row = %d, want %d", b.PendingImageRow, row)
	}
	if far.Img == nil {
		t.Fatal("decoded image was not installed on the node")
	}
}

func TestDeferredImageInViewportRebuilds(t *testing.T) {
	b := deferredImageFixture(t, `<html><body><img id="near" src="near.png" alt="near"></body></html>`)
	near := nodeByID(b.Page.Root, "near")
	before := b.Layout
	batch := imageBatch{Seq: 1, Results: []imageLoadResult{{
		Node: near, Src: "near.png", Img: image.NewRGBA(image.Rect(0, 0, 8, 8)),
	}}}
	if !b.applyDeferredImageBatch(batch) {
		t.Fatal("visible batch did not request a repaint")
	}
	if b.Layout == before {
		t.Fatal("visible image did not rebuild the layout")
	}
	if b.PendingImageRow != 0 {
		t.Fatalf("visible image left a pending row %d", b.PendingImageRow)
	}
}

func TestDeferredImageWithoutFragmentRebuilds(t *testing.T) {
	b := deferredImageFixture(t, `<html><body><img id="ghost" src="ghost.png" style="display:none"></body></html>`)
	ghost := nodeByID(b.Page.Root, "ghost")
	if nodeRow(b.Layout, ghost) >= 0 {
		t.Fatal("display:none image was laid out")
	}
	batch := imageBatch{Seq: 1, Results: []imageLoadResult{{
		Node: ghost, Src: "ghost.png", Img: image.NewRGBA(image.Rect(0, 0, 8, 8)),
	}}}
	b.applyDeferredImageBatch(batch)
	if b.PendingImageRow != 0 {
		t.Fatalf("image without a fragment was deferred to row %d", b.PendingImageRow)
	}
}

func TestPendingImageLayoutWaitsForViewport(t *testing.T) {
	b := deferredImageFixture(t, `<html><body><p>top</p>`+
		`<div style="height:2000px"></div><img id="far" src="far.png" alt="far"></body></html>`)
	far := nodeByID(b.Page.Root, "far")
	row := nodeRow(b.Layout, far)
	b.applyDeferredImageBatch(imageBatch{Seq: 1, Results: []imageLoadResult{{
		Node: far, Src: "far.png", Img: image.NewRGBA(image.Rect(0, 0, 8, 8)),
	}}})
	if b.PendingImageRow == 0 {
		t.Fatal("below-fold image was not deferred")
	}
	before := b.Layout
	b.Scroll = 1
	b.applyPendingImageLayout()
	if b.Layout != before {
		t.Fatal("distant scroll rebuilt the page for a pending image")
	}
	if b.PendingImageRow == 0 {
		t.Fatal("distant scroll cleared the pending row")
	}
	// The image's own row is now inside the viewport.
	b.Scroll = row - 1
	b.applyPendingImageLayout()
	if b.PendingImageRow != 0 {
		t.Fatal("image entering the viewport stayed pending")
	}
	if b.Layout == before {
		t.Fatal("image entering the viewport did not rebuild")
	}
	if nodeRow(b.Layout, far) != row {
		t.Fatalf("image moved to row %d, want %d", nodeRow(b.Layout, far), row)
	}
}

func TestPendingImageLayoutRebuildsAtDocumentEnd(t *testing.T) {
	b := deferredImageFixture(t, `<html><body><p>top</p>`+
		`<div style="height:2000px"></div><img id="far" src="far.png" alt="far"></body></html>`)
	far := nodeByID(b.Page.Root, "far")
	b.applyDeferredImageBatch(imageBatch{Seq: 1, Results: []imageLoadResult{{
		Node: far, Src: "far.png", Img: image.NewRGBA(image.Rect(0, 0, 8, 8)),
	}}})
	before := b.Layout
	b.Scroll = b.Layout.Height
	b.applyPendingImageLayout()
	if b.PendingImageRow != 0 || b.Layout == before {
		t.Fatal("pending image at the end of the document was not laid out")
	}
}

func TestDrawAppliesPendingImageLayout(t *testing.T) {
	b := deferredImageFixture(t, `<html><body><p>top</p>`+
		`<div style="height:2000px"></div><img id="far" src="far.png" alt="far"></body></html>`)
	far := nodeByID(b.Page.Root, "far")
	row := nodeRow(b.Layout, far)
	b.applyDeferredImageBatch(imageBatch{Seq: 1, Results: []imageLoadResult{{
		Node: far, Src: "far.png", Img: image.NewRGBA(image.Rect(0, 0, 8, 8)),
	}}})
	before := b.Layout
	drawQuiet(b)
	if b.Layout != before {
		t.Fatal("a frame far above the pending image rebuilt the page")
	}
	b.Scroll = row - 1
	drawQuiet(b)
	if b.PendingImageRow != 0 || b.Layout == before {
		t.Fatal("the frame that reached the image did not lay it out")
	}
}

func TestBelowViewportImageBatchKeepsContentFrame(t *testing.T) {
	b := deferredImageFixture(t, `<html><body><p>top of the page</p>`+
		`<div style="height:2000px"></div><img id="far" src="far.png" alt="far"></body></html>`)
	far := nodeByID(b.Page.Root, "far")
	drawQuiet(b)
	before := append(b.frameBuf.Cells[:0:0], b.frameBuf.Cells...)
	b.applyDeferredImageBatch(imageBatch{Seq: 1, Results: []imageLoadResult{{
		Node: far, Src: "far.png", Img: image.NewRGBA(image.Rect(0, 0, 8, 8)),
	}}})
	drawQuiet(b)
	if !reflect.DeepEqual(b.frameBuf.Cells, before) {
		t.Fatal("a below-fold image changed the visible content frame")
	}
}

func TestStartDeferredImageLoadStreams(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	doc := ParseHTML(`<html><body><img id="x" src="` + dataURL + `"></body></html>`)
	u := mustParse(t, "https://example.com/")
	b := newTestSession(60, 20)
	b.loader.NoImages = false
	b.loader.DeferImageLoad = true
	b.Page = &Page{URL: u, Base: u, Root: doc}
	b.URL = u
	b.LoadSeq = 1
	b.rebuild()
	if !b.DeferredImages {
		t.Fatal("deferred image load did not start")
	}
	defer b.cancelDeferredImageLoad()
	deadline := time.After(2 * time.Second)
	for b.DeferredImages {
		select {
		case batch := <-b.ImageCh:
			if !b.applyDeferredImageBatch(batch) {
				t.Fatal("streamed batch did not request a repaint")
			}
		case <-deadline:
			t.Fatal("timed out waiting for deferred image results")
		}
	}
	img := doc.FindFirst("img")
	if img == nil || img.Img == nil {
		t.Fatal("streamed image was not installed")
	}
}
