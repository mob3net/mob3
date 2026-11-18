package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"io"
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

func TestFitImageCapsDimensions(t *testing.T) {
	l := NewLoader()
	l.maxImageW, l.maxImageH = 200, 150
	if b := l.fitImage(image.NewNRGBA(image.Rect(0, 0, 400, 300))).Bounds(); b.Dx() != 200 || b.Dy() != 150 {
		t.Fatalf("square cap = %dx%d, want 200x150", b.Dx(), b.Dy())
	}

	// The narrower side follows the aspect ratio instead of being clamped
	// independently.
	l.maxImageW, l.maxImageH = 100, 100
	if b := l.fitImage(image.NewNRGBA(image.Rect(0, 0, 400, 100))).Bounds(); b.Dx() != 100 || b.Dy() != 25 {
		t.Fatalf("wide cap = %dx%d, want 100x25", b.Dx(), b.Dy())
	}

	small := image.NewNRGBA(image.Rect(0, 0, 50, 40))
	if got := l.fitImage(small); got != small {
		t.Fatalf("image inside the cap was resampled: %v", got.Bounds())
	}

	l.maxImageW, l.maxImageH = 0, 0
	big := image.NewNRGBA(image.Rect(0, 0, 400, 300))
	if got := l.fitImage(big); got != big {
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
	loader := NewLoader()
	loader.noImages = false
	loader.maxImageW, loader.maxImageH = 100, 100
	if !loader.loadImages(context.Background(), root, u, false) {
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

func TestBrowserSetsImageCapFromViewport(t *testing.T) {
	b := NewBrowser(177, 42)
	if b.loader.maxImageW != 177*8 || b.loader.maxImageH != 42*16 {
		t.Fatalf("cap = %dx%d, want %dx%d", b.loader.maxImageW, b.loader.maxImageH, 177*8, 42*16)
	}
	b.width, b.height = 80, 24
	b.applyImageCap()
	if b.loader.maxImageW != 1024 || b.loader.maxImageH != 512 {
		t.Fatalf("small viewport cap = %dx%d, want the 1024x512 floor", b.loader.maxImageW, b.loader.maxImageH)
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
	if _, err := safeDecodeConfig([]byte("panicimg data")); err == nil {
		t.Fatal("config decoder panic was not converted")
	}
	if _, err := safeDecodeImage([]byte("panicimg data")); err == nil {
		t.Fatal("image decoder panic was not converted")
	}
}
