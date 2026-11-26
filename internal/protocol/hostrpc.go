package protocol

import (
	"errors"
	"fmt"
	"net/url"
	"time"
)

// HostOp identifies one host-broker method. The payload is a flat HostCall /
// HostResult pair; unused fields are encoded empty so the same codec serves
// every method without a per-method wire type.
type HostOp uint8

const (
	HostOpFetch HostOp = iota + 1
	HostOpJSFetch
	HostOpRecordBlocked
	HostOpApplyMixedContent
	HostOpIsLocalURL
	HostOpSetUserAgent
	HostOpUserAgent
	HostOpSetSearchURL
	HostOpSearchURL
	HostOpSetImageCaps
	HostOpImageCaps
	HostOpBeginPage
	HostOpCurrentPageGeneration
	HostOpStartRequest
	HostOpRecordCacheHit
	HostOpFinishRequest
	HostOpRequestSnapshot
	HostOpRequestCount
	HostOpCacheStats
	HostOpCacheLimitBytes
	HostOpSetCacheLimitBytes
	HostOpDocumentCookieValues
	HostOpSetDocumentCookieRaw
	HostOpCookieViews
	HostOpSetCertSource
	HostOpCertSource
	HostOpConfigTokenCurrent
	HostOpConfigTokenConsume
	// HostOpCancel cancels the call with the same ID. It has no result.
	HostOpCancel
)

func (op HostOp) valid() bool { return op >= HostOpFetch && op <= HostOpCancel }

// HostErrorKind is the serializable identity of a host error. The child
// reconstructs the sentinel so errors.Is keeps working across the boundary.
type HostErrorKind uint8

const (
	HostErrNone HostErrorKind = iota
	HostErrOther
	HostErrInvalid
	HostErrLimit
	HostErrLocalNetwork
	HostErrMixedContent
	HostErrResourceTooLarge
)

const maxHostList = 4096

// HostCall is one host-broker request. Fields not used by Op stay empty.
type HostCall struct {
	Op          HostOp
	ID          uint64
	Meta        RequestMeta
	URL         string
	Method      string
	Body        []byte
	ContentType string
	JS          FetchRequest
	Gen         int
	Kind        string
	RequestID   int
	Status      int
	Size        int
	Elapsed     time.Duration
	ErrKind     HostErrorKind
	ErrText     string
	Limit       int64
	UserAgent   string
	SearchURL   string
	Width       int
	Height      int
	CookieRaw   string
	Source      string
	Token       string
}

// HostResult is the answer to one HostCall. Fields not used by the call stay
// zero. ErrText and ErrKind carry a serializable error.
type HostResult struct {
	ID      uint64
	Fetch   HostFetch
	JS      FetchResponse
	URL     string
	Bool    bool
	Int     int
	Width   int
	Height  int
	Used    int64
	Limit   int64
	Entries int
	Records []RequestRecord
	Cookies []CookieView
	Text    string
	ErrKind HostErrorKind
	ErrText string
}

// Error reconstructs the host error carried by a result.
func (r HostResult) Error() error { return hostError(r.ErrKind, r.ErrText) }

// AppendHostCall encodes one host call.
func AppendHostCall(dst []byte, c HostCall, lim Limits) ([]byte, error) {
	lim = lim.normalized()
	e := &encoder{b: dst, lim: lim}
	e.u8(uint8(c.Op))
	e.u64(c.ID)
	e.requestMeta(c.Meta)
	e.str(c.URL)
	e.str(c.Method)
	e.blob(c.Body, lim.MaxMessageBytes)
	e.str(c.ContentType)
	e.fetchRequest(c.JS)
	e.i32(c.Gen)
	e.str(c.Kind)
	e.i32(c.RequestID)
	e.i32(c.Status)
	e.i32(c.Size)
	e.u64(uint64(c.Elapsed))
	e.u8(uint8(c.ErrKind))
	e.str(c.ErrText)
	e.u64(uint64(c.Limit))
	e.str(c.UserAgent)
	e.str(c.SearchURL)
	e.i32(c.Width)
	e.i32(c.Height)
	e.str(c.CookieRaw)
	e.str(c.Source)
	e.str(c.Token)
	return e.b, e.err
}

// DecodeHostCall decodes one host call.
func DecodeHostCall(src []byte, lim Limits) (HostCall, error) {
	lim = lim.normalized()
	d := &decoder{b: src, lim: lim}
	c := HostCall{Op: HostOp(d.u8())}
	if !c.Op.valid() {
		return HostCall{}, fmt.Errorf("%w: host op %d", ErrInvalid, c.Op)
	}
	c.ID = d.u64()
	c.Meta = d.requestMeta()
	c.URL = d.str()
	c.Method = d.str()
	c.Body = d.blob(lim.MaxMessageBytes)
	c.ContentType = d.str()
	c.JS = d.fetchRequest()
	c.Gen = d.i32()
	c.Kind = d.str()
	c.RequestID = d.i32()
	c.Status = d.i32()
	c.Size = d.i32()
	c.Elapsed = time.Duration(d.u64())
	c.ErrKind = HostErrorKind(d.u8())
	if c.ErrKind > HostErrResourceTooLarge {
		return HostCall{}, fmt.Errorf("%w: host error kind %d", ErrInvalid, c.ErrKind)
	}
	c.ErrText = d.str()
	c.Limit = int64(d.u64())
	c.UserAgent = d.str()
	c.SearchURL = d.str()
	c.Width = d.i32()
	c.Height = d.i32()
	c.CookieRaw = d.str()
	c.Source = d.str()
	c.Token = d.str()
	if d.err != nil {
		return HostCall{}, d.err
	}
	if d.off != len(d.b) {
		return HostCall{}, fmt.Errorf("%w: %d trailing host-call bytes", ErrInvalid, len(d.b)-d.off)
	}
	return c, nil
}

// AppendHostResult encodes one host result.
func AppendHostResult(dst []byte, r HostResult, lim Limits) ([]byte, error) {
	lim = lim.normalized()
	e := &encoder{b: dst, lim: lim}
	e.u64(r.ID)
	e.hostFetch(r.Fetch)
	e.fetchResponse(r.JS)
	e.str(r.URL)
	e.bool(r.Bool)
	e.i32(r.Int)
	e.i32(r.Width)
	e.i32(r.Height)
	e.u64(uint64(r.Used))
	e.u64(uint64(r.Limit))
	e.i32(r.Entries)
	e.requestRecords(r.Records)
	e.cookieViews(r.Cookies)
	e.str(r.Text)
	e.u8(uint8(r.ErrKind))
	e.str(r.ErrText)
	return e.b, e.err
}

// DecodeHostResult decodes one host result.
func DecodeHostResult(src []byte, lim Limits) (HostResult, error) {
	lim = lim.normalized()
	d := &decoder{b: src, lim: lim}
	r := HostResult{ID: d.u64()}
	r.Fetch = d.hostFetch()
	r.JS = d.fetchResponse()
	r.URL = d.str()
	r.Bool = d.bool()
	r.Int = d.i32()
	r.Width = d.i32()
	r.Height = d.i32()
	r.Used = int64(d.u64())
	r.Limit = int64(d.u64())
	r.Entries = d.i32()
	r.Records = d.requestRecords()
	r.Cookies = d.cookieViews()
	r.Text = d.str()
	r.ErrKind = HostErrorKind(d.u8())
	if r.ErrKind > HostErrResourceTooLarge {
		return HostResult{}, fmt.Errorf("%w: host error kind %d", ErrInvalid, r.ErrKind)
	}
	r.ErrText = d.str()
	if d.err != nil {
		return HostResult{}, d.err
	}
	if d.off != len(d.b) {
		return HostResult{}, fmt.Errorf("%w: %d trailing host-result bytes", ErrInvalid, len(d.b)-d.off)
	}
	return r, nil
}

func hostErrorKind(err error) HostErrorKind {
	switch {
	case err == nil:
		return HostErrNone
	case errors.Is(err, ErrLocalNetworkBlocked):
		return HostErrLocalNetwork
	case errors.Is(err, ErrMixedContentBlocked):
		return HostErrMixedContent
	case errors.Is(err, ErrResourceTooLarge):
		return HostErrResourceTooLarge
	case errors.Is(err, ErrInvalid):
		return HostErrInvalid
	case errors.Is(err, ErrLimit):
		return HostErrLimit
	default:
		return HostErrOther
	}
}

func hostError(kind HostErrorKind, text string) error {
	switch kind {
	case HostErrNone:
		return nil
	case HostErrInvalid:
		return ErrInvalid
	case HostErrLimit:
		return ErrLimit
	case HostErrLocalNetwork:
		return ErrLocalNetworkBlocked
	case HostErrMixedContent:
		return ErrMixedContentBlocked
	case HostErrResourceTooLarge:
		return ErrResourceTooLarge
	default:
		if text == "" {
			return errors.New("host error")
		}
		return errors.New(text)
	}
}

func (e *encoder) requestMeta(m RequestMeta) {
	e.i32(m.Gen)
	e.str(m.Kind)
	e.u64(m.EntryID)
	e.str(m.PageURL)
	e.str(m.TargetURL)
	e.bool(m.BlockLocal)
	e.bool(m.PageSecure)
	e.str(m.Origin)
}

func (d *decoder) requestMeta() RequestMeta {
	return RequestMeta{
		Gen:        d.i32(),
		Kind:       d.str(),
		EntryID:    d.u64(),
		PageURL:    d.str(),
		TargetURL:  d.str(),
		BlockLocal: d.bool(),
		PageSecure: d.bool(),
		Origin:     d.str(),
	}
}

func (e *encoder) headers(h []Header) {
	e.count(len(h))
	for _, one := range h {
		e.str(one.Name)
		e.str(one.Value)
	}
}

func (d *decoder) headers() []Header {
	n := d.count(maxHostList)
	if n == 0 {
		return nil
	}
	out := make([]Header, n)
	for i := range out {
		out[i] = Header{Name: d.str(), Value: d.str()}
	}
	return out
}

func (e *encoder) fetchRequest(r FetchRequest) {
	e.str(r.URL)
	e.str(r.Method)
	e.headers(r.Headers)
	e.blob(r.Body, e.lim.MaxMessageBytes)
	e.u8(uint8(r.Purpose))
	e.str(r.Origin)
	e.bool(r.Credentials)
	e.bool(r.NoCORS)
	e.str(r.Kind)
	e.bool(r.PageLocal)
}

func (d *decoder) fetchRequest() FetchRequest {
	return FetchRequest{
		URL:         d.str(),
		Method:      d.str(),
		Headers:     d.headers(),
		Body:        d.blob(d.lim.MaxMessageBytes),
		Purpose:     FetchPurpose(d.u8()),
		Origin:      d.str(),
		Credentials: d.bool(),
		NoCORS:      d.bool(),
		Kind:        d.str(),
		PageLocal:   d.bool(),
	}
}

func (e *encoder) hostFetch(r HostFetch) {
	e.blob(r.Data, e.lim.MaxMessageBytes)
	e.str(r.ContentType)
	e.str(urlString(r.FinalURL))
	e.i32(r.Status)
	e.bool(r.PageLocal)
}

func (d *decoder) hostFetch() HostFetch {
	return HostFetch{
		Data:        d.blob(d.lim.MaxMessageBytes),
		ContentType: d.str(),
		FinalURL:    parseURLString(d.str()),
		Status:      d.i32(),
		PageLocal:   d.bool(),
	}
}

func (e *encoder) fetchResponse(r FetchResponse) {
	e.i32(r.Status)
	e.headers(r.Headers)
	e.blob(r.Body, e.lim.MaxMessageBytes)
	e.str(r.FinalURL)
	e.str(r.ContentType)
}

func (d *decoder) fetchResponse() FetchResponse {
	return FetchResponse{
		Status:      d.i32(),
		Headers:     d.headers(),
		Body:        d.blob(d.lim.MaxMessageBytes),
		FinalURL:    d.str(),
		ContentType: d.str(),
	}
}

func (e *encoder) requestRecords(records []RequestRecord) {
	e.count(len(records))
	for _, r := range records {
		e.i32(r.ID)
		e.i32(r.Gen)
		e.str(r.Method)
		e.str(r.URL)
		e.str(r.Kind)
		e.i32(r.Status)
		e.i32(r.Size)
		e.u64(uint64(r.Elapsed))
		e.str(r.Err)
		e.bool(r.Done)
		e.bool(r.Cached)
	}
}

func (d *decoder) requestRecords() []RequestRecord {
	n := d.count(maxHostList)
	if n == 0 {
		return nil
	}
	out := make([]RequestRecord, n)
	for i := range out {
		out[i] = RequestRecord{
			ID:      d.i32(),
			Gen:     d.i32(),
			Method:  d.str(),
			URL:     d.str(),
			Kind:    d.str(),
			Status:  d.i32(),
			Size:    d.i32(),
			Elapsed: time.Duration(d.u64()),
			Err:     d.str(),
			Done:    d.bool(),
			Cached:  d.bool(),
		}
	}
	return out
}

func (e *encoder) cookieViews(views []CookieView) {
	e.count(len(views))
	for _, v := range views {
		e.str(v.Name)
		e.str(v.Value)
		e.str(v.Domain)
		e.str(v.Path)
		e.str(v.SameSite)
		e.bool(v.HttpOnly)
		e.bool(v.Secure)
		e.str(cookieExpiry(v.Expires))
	}
}

func (d *decoder) cookieViews() []CookieView {
	n := d.count(maxHostList)
	if n == 0 {
		return nil
	}
	out := make([]CookieView, n)
	for i := range out {
		out[i] = CookieView{
			Name:     d.str(),
			Value:    d.str(),
			Domain:   d.str(),
			Path:     d.str(),
			SameSite: d.str(),
			HttpOnly: d.bool(),
			Secure:   d.bool(),
			Expires:  parseCookieExpiry(d.str()),
		}
	}
	return out
}

func urlString(u *url.URL) string {
	if u == nil {
		return ""
	}
	return u.String()
}

func parseURLString(s string) *url.URL {
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil
	}
	return u
}

func cookieExpiry(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

func parseCookieExpiry(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
