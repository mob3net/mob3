package host

import (
	"browser/internal/protocol"
	"sync"
	"time"
	"unicode/utf8"
)

// maxRequestLogURL bounds the URL stored for one request record. The request
// list is a display aid, so a page cannot retain unbounded URL strings.
const maxRequestLogURL = 2 << 10

// logURL returns a bounded, rune-safe form of a request URL.
func logURL(raw string) string {
	if len(raw) <= maxRequestLogURL {
		return raw
	}
	cut := maxRequestLogURL - len("…")
	for cut > 0 && !utf8.RuneStart(raw[cut]) {
		cut--
	}
	return raw[:cut] + "…"
}

// Record is one network request shown in the console request pane.
type Record = protocol.RequestRecord

// RequestLog is the in-memory request list for one page generation.
type RequestLog struct {
	mu      sync.Mutex
	gen     int
	next    int
	records []Record
}

// SetRecords replaces the log, for tests and debug views.
func (l *RequestLog) SetRecords(r []Record) {
	l.mu.Lock()
	l.records = r
	l.mu.Unlock()
}

// beginPage clears the request list and starts a new page generation.
func (l *RequestLog) BeginPage() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gen++
	l.records = nil
	return l.gen
}

func (l *RequestLog) CurrentPageGeneration() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.gen
}

func (l *RequestLog) Start(gen int, method, rawURL, kind string) (int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if gen != l.gen {
		return 0, false
	}
	l.next++
	id := l.next
	l.records = append(l.records, Record{
		ID: id, Gen: gen, Method: method, URL: logURL(rawURL), Kind: kind,
	})
	if len(l.records) > 512 {
		l.records = append([]Record(nil), l.records[len(l.records)-512:]...)
	}
	return id, true
}

func (l *RequestLog) RecordCacheHit(gen int, method, rawURL, kind string, size int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if gen != l.gen {
		return
	}
	l.next++
	l.records = append(l.records, Record{
		ID: l.next, Gen: gen, Method: method, URL: logURL(rawURL), Kind: kind,
		Status: 200, Size: size, Done: true, Cached: true,
	})
	if len(l.records) > 512 {
		l.records = append([]Record(nil), l.records[len(l.records)-512:]...)
	}
}

func (l *RequestLog) Finish(id, gen int, status, size int, elapsed time.Duration, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if gen != l.gen {
		return
	}
	for i := range l.records {
		if l.records[i].ID == id {
			l.records[i].Status = status
			l.records[i].Size = size
			l.records[i].Elapsed = elapsed
			l.records[i].Done = true
			if err != nil {
				l.records[i].Err = err.Error()
			}
			return
		}
	}
}

func (l *RequestLog) Snapshot() []Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Record, len(l.records))
	copy(out, l.records)
	return out
}

func (l *RequestLog) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.records)
}

// NewRequestLog creates an empty request log.
func NewRequestLog() *RequestLog { return &RequestLog{} }
