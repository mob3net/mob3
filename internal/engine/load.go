package engine

import (
	"context"
	"net/url"

	"browser/internal/protocol"
)

// LoadFunc fetches one page request.
type LoadFunc func(context.Context, string, string, []byte, string) (*Page, error)

// LoadResult is one completed navigation request.
type LoadResult struct {
	Seq              int
	Page             *Page
	Err              error
	URL              *url.URL
	Method           string
	Body             []byte
	ContentType      string
	EntryID          uint64
	HistoryPush      bool
	RestoreScroll    int
	HasRestoreScroll bool
	DownloadName     string
}

const progressLingerTicks = 4

// NextHistoryID allocates an opaque ID for a new history entry.
func (s *Session) NextHistoryID() uint64 {
	id := s.NextEntryID
	s.NextEntryID++
	return id
}

// BeginLoad starts a background page load. The caller supplies the bound load
// function; results arrive on LoadCh and are applied by the caller.
func (s *Session) BeginLoad(load LoadFunc, u *url.URL, method, target string, body []byte, contentType string, entryID uint64, historyPush bool, restoreScroll int, hasRestoreScroll bool, downloadName string) {
	s.CancelLoadNow()
	s.LoadSeq++
	seq := s.LoadSeq
	ctx, cancel := context.WithCancel(context.Background())
	ctx = protocol.WithRequestEntry(ctx, entryID)
	s.CancelLoad = cancel
	s.Loading = true
	s.StartProgress()
	if s.LoadCh == nil {
		s.LoadCh = make(chan LoadResult, 1)
	}
	go func() {
		page, err := load(ctx, method, target, body, contentType)
		select {
		case s.LoadCh <- LoadResult{
			Seq: seq, Page: page, Err: err, URL: u,
			Method: method, Body: body, ContentType: contentType,
			EntryID: entryID, HistoryPush: historyPush,
			RestoreScroll: restoreScroll, HasRestoreScroll: hasRestoreScroll,
			DownloadName: downloadName,
		}:
		case <-ctx.Done():
		}
	}()
}

// CancelLoadNow aborts the in-flight page load.
func (s *Session) CancelLoadNow() {
	if s.CancelLoad != nil {
		s.CancelLoad()
		s.CancelLoad = nil
	}
	s.Loading = false
	s.ClearProgress()
}

func (s *Session) StartProgress() {
	s.Progress = 0.05
	s.ProgressHold = 0
}

func (s *Session) FinishProgress() {
	if s.Progress <= 0 {
		return
	}
	s.Progress = 1
	s.ProgressHold = progressLingerTicks
}

func (s *Session) ClearProgress() {
	s.Progress = 0
	s.ProgressHold = 0
}

func (s *Session) ProgressActive() bool { return s.Loading || s.ProgressHold > 0 }

func (s *Session) TickProgress() bool {
	if s.Loading {
		s.Progress += (0.9 - s.Progress) * 0.08
		if s.Progress > 0.9 {
			s.Progress = 0.9
		}
		return true
	}
	if s.ProgressHold > 0 {
		s.ProgressHold--
		if s.ProgressHold == 0 {
			s.Progress = 0
			return true
		}
	}
	return false
}
