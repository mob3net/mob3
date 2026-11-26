package host

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRequestLogLifecycleAndGenerations(t *testing.T) {
	l := NewRequestLog()
	gen := l.BeginPage()
	if gen != 1 || l.CurrentPageGeneration() != 1 {
		t.Fatalf("generation = %d current=%d", gen, l.CurrentPageGeneration())
	}
	id, ok := l.Start(gen, "GET", "https://example.test/a", "doc")
	if !ok || id != 1 {
		t.Fatalf("start = %d %v", id, ok)
	}
	l.Finish(id, gen, 200, 42, 5*time.Millisecond, nil)
	recs := l.Snapshot()
	if len(recs) != 1 || !recs[0].Done || recs[0].Status != 200 || recs[0].Size != 42 {
		t.Fatalf("records = %+v", recs)
	}

	if _, ok := l.Start(gen+1, "GET", "https://example.test/old", "css"); ok {
		t.Fatal("stale generation was accepted")
	}
	l.RecordCacheHit(gen, "GET", "https://example.test/c.css", "css", 10)
	if l.Count() != 2 {
		t.Fatalf("count = %d", l.Count())
	}
	recs = l.Snapshot()
	if !recs[1].Cached || recs[1].Status != 200 || recs[1].Size != 10 {
		t.Fatalf("cache-hit record = %+v", recs[1])
	}

	id, _ = l.Start(gen, "GET", "https://example.test/b", "img")
	l.Finish(id, gen, 500, 0, time.Second, errors.New("boom"))
	recs = l.Snapshot()
	if recs[2].Err != "boom" || !recs[2].Done {
		t.Fatalf("failed record = %+v", recs[2])
	}
	l.Finish(id, gen+1, 200, 99, 0, nil)
	if l.Snapshot()[2].Status != 500 {
		t.Fatal("stale finish changed the record")
	}

	next := l.BeginPage()
	if next != 2 || l.Count() != 0 {
		t.Fatalf("new page gen=%d count=%d", next, l.Count())
	}
}

func TestRequestLogTruncatesLongURLs(t *testing.T) {
	l := NewRequestLog()
	gen := l.BeginPage()
	long := "https://example.test/" + strings.Repeat("a", maxRequestLogURL+100)
	id, _ := l.Start(gen, "GET", long, "img")
	l.Finish(id, gen, 200, 1, 0, nil)
	got := l.Snapshot()[0].URL
	if len(got) > maxRequestLogURL || !strings.HasSuffix(got, "…") {
		t.Fatalf("logged URL = %d bytes, want at most %d with a marker", len(got), maxRequestLogURL)
	}
	l.RecordCacheHit(gen, "GET", long, "css", 1)
	got = l.Snapshot()[1].URL
	if len(got) > maxRequestLogURL {
		t.Fatalf("cache-hit URL = %d bytes", len(got))
	}
}

func TestRequestLogCapsAndCopies(t *testing.T) {
	l := NewRequestLog()
	gen := l.BeginPage()
	for i := 0; i < 515; i++ {
		if _, ok := l.Start(gen, "GET", "https://example.test/x", "img"); !ok {
			t.Fatal("start rejected")
		}
	}
	recs := l.Snapshot()
	if len(recs) != 512 || recs[0].ID != 4 {
		t.Fatalf("capped log = %d records, first ID %d", len(recs), recs[0].ID)
	}
	recs[0].URL = "mutated"
	if l.Snapshot()[0].URL == "mutated" {
		t.Fatal("Snapshot shared its slice with the log")
	}

	l.SetRecords([]Record{{ID: 9, URL: "x"}})
	if got := l.Snapshot(); len(got) != 1 || got[0].ID != 9 {
		t.Fatalf("SetRecords = %+v", got)
	}
}
