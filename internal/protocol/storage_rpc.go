package protocol

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// StorageOp identifies one shared-storage operation.
type StorageOp uint8

const (
	StorageOpGet StorageOp = iota + 1
	StorageOpSet
	StorageOpDelete
	StorageOpClear
	StorageOpKeys
	StorageOpLen
	StorageOpSnapshot
)

func (op StorageOp) valid() bool { return op >= StorageOpGet && op <= StorageOpSnapshot }

// StorageCall is one storage-broker request. Kind/TabID/Origin identify the
// view; the remaining fields are operation arguments.
type StorageCall struct {
	Op     StorageOp
	ID     uint64
	Kind   StorageKind
	TabID  uint64
	Origin Origin
	Key    string
	Value  string
}

// StorageResult is one storage-broker response.
type StorageResult struct {
	ID      uint64
	Value   string
	Found   bool
	Values  map[string]string
	Strings []string
	Int     int
	ErrKind HostErrorKind
	ErrText string
}

// Error reconstructs the storage error.
func (r StorageResult) Error() error { return hostError(r.ErrKind, r.ErrText) }

// AppendStorageCall encodes one storage call.
func AppendStorageCall(dst []byte, c StorageCall, lim Limits) ([]byte, error) {
	lim = lim.normalized()
	e := &encoder{b: dst, lim: lim}
	e.u8(uint8(c.Op))
	e.u64(c.ID)
	e.u8(uint8(c.Kind))
	e.u64(c.TabID)
	e.str(c.Origin.String())
	e.str(c.Key)
	e.str(c.Value)
	return e.b, e.err
}

// DecodeStorageCall decodes one storage call.
func DecodeStorageCall(src []byte, lim Limits) (StorageCall, error) {
	lim = lim.normalized()
	d := &decoder{b: src, lim: lim}
	c := StorageCall{
		Op:    StorageOp(d.u8()),
		ID:    d.u64(),
		Kind:  StorageKind(d.u8()),
		TabID: d.u64(),
	}
	if !c.Op.valid() || c.Kind > StorageSession {
		return StorageCall{}, fmt.Errorf("%w: invalid storage call", ErrInvalid)
	}
	var ok bool
	c.Origin, ok = ParseOrigin(d.str())
	if !ok {
		return StorageCall{}, fmt.Errorf("%w: invalid storage origin", ErrInvalid)
	}
	c.Key = d.str()
	c.Value = d.str()
	if d.err != nil {
		return StorageCall{}, d.err
	}
	if d.off != len(d.b) {
		return StorageCall{}, fmt.Errorf("%w: %d trailing storage-call bytes", ErrInvalid, len(d.b)-d.off)
	}
	return c, nil
}

// AppendStorageResult encodes one storage result.
func AppendStorageResult(dst []byte, r StorageResult, lim Limits) ([]byte, error) {
	lim = lim.normalized()
	e := &encoder{b: dst, lim: lim}
	e.u64(r.ID)
	e.str(r.Value)
	e.bool(r.Found)
	e.count(len(r.Values))
	for k, v := range r.Values {
		e.str(k)
		e.str(v)
	}
	e.count(len(r.Strings))
	for _, s := range r.Strings {
		e.str(s)
	}
	e.i32(r.Int)
	e.u8(uint8(r.ErrKind))
	e.str(r.ErrText)
	return e.b, e.err
}

// DecodeStorageResult decodes one storage result.
func DecodeStorageResult(src []byte, lim Limits) (StorageResult, error) {
	lim = lim.normalized()
	d := &decoder{b: src, lim: lim}
	r := StorageResult{
		ID:    d.u64(),
		Value: d.str(),
		Found: d.bool(),
	}
	if n := d.count(maxHostList); n > 0 {
		r.Values = make(map[string]string, n)
		for i := 0; i < n; i++ {
			r.Values[d.str()] = d.str()
		}
	}
	if n := d.count(maxHostList); n > 0 {
		r.Strings = make([]string, n)
		for i := range r.Strings {
			r.Strings[i] = d.str()
		}
	}
	r.Int = d.i32()
	r.ErrKind = HostErrorKind(d.u8())
	if r.ErrKind > HostErrStorageQuota {
		return StorageResult{}, fmt.Errorf("%w: storage error kind %d", ErrInvalid, r.ErrKind)
	}
	r.ErrText = d.str()
	if d.err != nil {
		return StorageResult{}, d.err
	}
	if d.off != len(d.b) {
		return StorageResult{}, fmt.Errorf("%w: %d trailing storage-result bytes", ErrInvalid, len(d.b)-d.off)
	}
	return r, nil
}

// StorageClient implements StorageProvider over a shared peer.
type StorageClient struct {
	peer *MuxPeer
	lim  Limits
	next atomic.Uint64

	mu   sync.Mutex
	pend map[uint64]chan StorageResult

	closed atomic.Bool
}

// NewStorageClientPeer mounts a storage client on a peer.
func NewStorageClientPeer(peer *MuxPeer) *StorageClient {
	c := &StorageClient{peer: peer, lim: peer.lim, pend: make(map[uint64]chan StorageResult)}
	_ = peer.Handle(MuxStorageResult, c.handleFrame)
	return c
}

// View resolves one storage area proxy.
func (c *StorageClient) View(kind StorageKind, tabID uint64, origin Origin) Storage {
	return &remoteStorageView{client: c, kind: kind, tabID: tabID, origin: origin}
}

func (c *StorageClient) call(ctx context.Context, call StorageCall) (StorageResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.closed.Load() {
		return StorageResult{}, ErrSessionClosed
	}
	id := c.next.Add(1)
	call.ID = id
	ch := make(chan StorageResult, 1)
	c.mu.Lock()
	c.pend[id] = ch
	c.mu.Unlock()
	body, err := AppendStorageCall(nil, call, c.lim)
	if err != nil {
		c.remove(id)
		return StorageResult{}, err
	}
	if err := c.peer.WriteFrame(MuxStorageCall, id, body); err != nil {
		c.remove(id)
		return StorageResult{}, err
	}
	select {
	case result := <-ch:
		return result, result.Error()
	case <-ctx.Done():
		c.remove(id)
		return StorageResult{}, ctx.Err()
	case <-c.peer.Done():
		c.remove(id)
		return StorageResult{}, ErrSessionClosed
	}
}

func (c *StorageClient) remove(id uint64) {
	c.mu.Lock()
	delete(c.pend, id)
	c.mu.Unlock()
}

func (c *StorageClient) handleFrame(f MuxFrame) error {
	result, err := DecodeStorageResult(f.Body, c.lim)
	if err != nil {
		return err
	}
	result.ID = f.ID
	c.mu.Lock()
	ch := c.pend[f.ID]
	delete(c.pend, f.ID)
	c.mu.Unlock()
	if ch != nil {
		select {
		case ch <- result:
		default:
		}
	}
	return nil
}

type remoteStorageView struct {
	client *StorageClient
	kind   StorageKind
	tabID  uint64
	origin Origin
}

func (v *remoteStorageView) call(op StorageOp, key, value string) (StorageResult, error) {
	return v.client.call(context.Background(), StorageCall{
		Op: op, Kind: v.kind, TabID: v.tabID, Origin: v.origin, Key: key, Value: value,
	})
}

func (v *remoteStorageView) Get(key string) (string, bool) {
	r, err := v.call(StorageOpGet, key, "")
	if err != nil {
		return "", false
	}
	return r.Value, r.Found
}

func (v *remoteStorageView) Set(key, value string) error {
	_, err := v.call(StorageOpSet, key, value)
	return err
}

func (v *remoteStorageView) Delete(key string) { _, _ = v.call(StorageOpDelete, key, "") }

func (v *remoteStorageView) Clear() { _, _ = v.call(StorageOpClear, "", "") }

func (v *remoteStorageView) Keys() []string {
	r, err := v.call(StorageOpKeys, "", "")
	if err != nil {
		return nil
	}
	return r.Strings
}

func (v *remoteStorageView) Len() int {
	r, err := v.call(StorageOpLen, "", "")
	if err != nil {
		return 0
	}
	return r.Int
}

func (v *remoteStorageView) Snapshot() map[string]string {
	r, err := v.call(StorageOpSnapshot, "", "")
	if err != nil {
		return nil
	}
	return r.Values
}

// StorageServer serves StorageProvider calls from a shared peer.
type StorageServer struct {
	provider StorageProvider
	peer     *MuxPeer
	lim      Limits
}

// NewStorageServerPeer mounts a storage server on a peer.
func NewStorageServerPeer(peer *MuxPeer, provider StorageProvider) *StorageServer {
	s := &StorageServer{provider: provider, peer: peer, lim: peer.lim}
	_ = peer.Handle(MuxStorageCall, s.handleFrame)
	return s
}

func (s *StorageServer) handleFrame(f MuxFrame) error {
	call, err := DecodeStorageCall(f.Body, s.lim)
	if err != nil {
		return err
	}
	result := s.dispatch(call)
	result.ID = f.ID
	body, err := AppendStorageResult(nil, result, s.lim)
	if err != nil {
		return err
	}
	return s.peer.WriteFrame(MuxStorageResult, result.ID, body)
}

func (s *StorageServer) dispatch(call StorageCall) StorageResult {
	var result StorageResult
	if s.provider == nil {
		result.ErrKind = HostErrOther
		result.ErrText = "storage unavailable"
		return result
	}
	view := s.provider.View(call.Kind, call.TabID, call.Origin)
	if view == nil {
		result.ErrKind = HostErrOther
		result.ErrText = "storage unavailable"
		return result
	}
	switch call.Op {
	case StorageOpGet:
		result.Value, result.Found = view.Get(call.Key)
	case StorageOpSet:
		if err := view.Set(call.Key, call.Value); err != nil {
			result.ErrKind = hostErrorKind(err)
			result.ErrText = err.Error()
		}
	case StorageOpDelete:
		view.Delete(call.Key)
	case StorageOpClear:
		view.Clear()
	case StorageOpKeys:
		result.Strings = view.Keys()
	case StorageOpLen:
		result.Int = view.Len()
	case StorageOpSnapshot:
		result.Values = view.Snapshot()
	}
	return result
}

var _ StorageProvider = (*StorageClient)(nil)
var _ Storage = (*remoteStorageView)(nil)
