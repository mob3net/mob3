package engine

import (
	"sort"
	"sync"

	"browser/internal/protocol"
)

const (
	// storagePerOriginLimit bounds one local or session storage area.
	storagePerOriginLimit = 5 << 20
	// storageTotalLimit bounds every area owned by one browser context.
	storageTotalLimit = 32 << 20
)

// ErrStorageQuota reports a write that would exceed a storage area or the
// browser-context total.
var ErrStorageQuota = protocol.ErrStorageQuota

// StorageKind selects localStorage or sessionStorage.
type StorageKind = protocol.StorageKind

const (
	StorageLocal   = protocol.StorageLocal
	StorageSession = protocol.StorageSession
)

type storageScope struct {
	tabID  uint64
	origin string
}

// StorageBroker owns the in-memory storage areas for one browser context.
// localStorage is keyed by origin and shared by every tab; sessionStorage is
// keyed by tab and origin. Opaque origins get a private view that is not shared
// with any other document.
type StorageBroker struct {
	mu         sync.Mutex
	perOrigin  int64
	totalLimit int64
	total      int64

	local        map[string]map[string]string
	localBytes   map[string]int64
	session      map[storageScope]map[string]string
	sessionBytes map[storageScope]int64
}

// NewStorageBroker creates the default in-memory storage pools.
func NewStorageBroker() *StorageBroker {
	return newStorageBroker(storagePerOriginLimit, storageTotalLimit)
}

// storageProvider adapts a StorageBroker to the serializable provider port.
type storageProvider struct{ broker *StorageBroker }

func (p storageProvider) View(kind protocol.StorageKind, tabID uint64, origin protocol.Origin) protocol.Storage {
	return p.broker.View(kind, tabID, origin)
}

func newStorageBroker(perOrigin, total int64) *StorageBroker {
	if perOrigin <= 0 {
		perOrigin = storagePerOriginLimit
	}
	if total <= 0 {
		total = storageTotalLimit
	}
	return &StorageBroker{
		perOrigin:    perOrigin,
		totalLimit:   total,
		local:        make(map[string]map[string]string),
		localBytes:   make(map[string]int64),
		session:      make(map[storageScope]map[string]string),
		sessionBytes: make(map[storageScope]int64),
	}
}

func newPrivateStorageView() *StorageView {
	return &StorageView{limit: storagePerOriginLimit, data: make(map[string]string)}
}

// View returns the storage area for one frame origin. An opaque origin gets a
// private, non-shared area.
func (b *StorageBroker) View(kind StorageKind, tabID uint64, origin protocol.Origin) *StorageView {
	if origin.IsOpaque() {
		limit := int64(storagePerOriginLimit)
		if b != nil {
			limit = b.perOrigin
		}
		return &StorageView{limit: limit, data: make(map[string]string)}
	}
	if b == nil {
		return &StorageView{limit: storagePerOriginLimit, data: make(map[string]string)}
	}
	return &StorageView{
		broker: b,
		kind:   kind,
		tabID:  tabID,
		origin: origin.String(),
		limit:  b.perOrigin,
	}
}

// StorageView is one localStorage or sessionStorage area as seen by a frame.
type StorageView struct {
	broker *StorageBroker
	kind   StorageKind
	tabID  uint64
	origin string
	limit  int64

	// data and privateBytes back an opaque-origin view. Tuple origins use the
	// broker maps instead.
	data         map[string]string
	privateBytes int64
}

func (v *StorageView) scope() storageScope {
	return storageScope{tabID: v.tabID, origin: v.origin}
}

// Get returns a stored value.
func (v *StorageView) Get(key string) (string, bool) {
	if v == nil {
		return "", false
	}
	if v.broker == nil {
		val, ok := v.data[key]
		return val, ok
	}
	v.broker.mu.Lock()
	defer v.broker.mu.Unlock()
	val, ok := v.broker.existingDataLocked(v.kind, v.scope())[key]
	return val, ok
}

// Set stores key. It returns ErrStorageQuota when the area or the context
// total would overflow.
func (v *StorageView) Set(key, value string) error {
	if v == nil {
		return ErrStorageQuota
	}
	if v.broker == nil {
		if v.data == nil {
			v.data = make(map[string]string)
		}
		old, exists := v.data[key]
		delta := int64(len(value)) - int64(len(old))
		if !exists {
			delta = int64(len(key) + len(value))
		}
		if v.privateBytes+delta > v.limit {
			return ErrStorageQuota
		}
		v.data[key] = value
		v.privateBytes += delta
		return nil
	}
	return v.broker.set(v.kind, v.scope(), key, value)
}

// Delete removes key.
func (v *StorageView) Delete(key string) {
	if v == nil {
		return
	}
	if v.broker == nil {
		old, ok := v.data[key]
		if !ok {
			return
		}
		delete(v.data, key)
		v.privateBytes -= int64(len(key) + len(old))
		return
	}
	v.broker.delete(v.kind, v.scope(), key)
}

// Clear empties the area.
func (v *StorageView) Clear() {
	if v == nil {
		return
	}
	if v.broker == nil {
		v.data = make(map[string]string)
		v.privateBytes = 0
		return
	}
	v.broker.clear(v.kind, v.scope())
}

// Keys returns the stored keys in sorted order.
func (v *StorageView) Keys() []string {
	if v == nil {
		return nil
	}
	if v.broker == nil {
		return sortedKeys(v.data)
	}
	v.broker.mu.Lock()
	defer v.broker.mu.Unlock()
	return sortedKeys(v.broker.existingDataLocked(v.kind, v.scope()))
}

// Len returns the number of stored keys.
func (v *StorageView) Len() int {
	if v == nil {
		return 0
	}
	if v.broker == nil {
		return len(v.data)
	}
	v.broker.mu.Lock()
	defer v.broker.mu.Unlock()
	return len(v.broker.existingDataLocked(v.kind, v.scope()))
}

// Snapshot returns a copy of the area for the console and tests.
func (v *StorageView) Snapshot() map[string]string {
	if v == nil {
		return nil
	}
	if v.broker == nil {
		out := make(map[string]string, len(v.data))
		for k, val := range v.data {
			out[k] = val
		}
		return out
	}
	v.broker.mu.Lock()
	defer v.broker.mu.Unlock()
	src := v.broker.existingDataLocked(v.kind, v.scope())
	out := make(map[string]string, len(src))
	for k, val := range src {
		out[k] = val
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (b *StorageBroker) existingDataLocked(kind StorageKind, scope storageScope) map[string]string {
	if kind == StorageSession {
		return b.session[scope]
	}
	return b.local[scope.origin]
}

func (b *StorageBroker) ensureDataLocked(kind StorageKind, scope storageScope) map[string]string {
	m := b.existingDataLocked(kind, scope)
	if m != nil {
		return m
	}
	m = make(map[string]string)
	if kind == StorageSession {
		b.session[scope] = m
	} else {
		b.local[scope.origin] = m
	}
	return m
}

func (b *StorageBroker) bytesLocked(kind StorageKind, scope storageScope) int64 {
	if kind == StorageSession {
		return b.sessionBytes[scope]
	}
	return b.localBytes[scope.origin]
}

func (b *StorageBroker) addBytesLocked(kind StorageKind, scope storageScope, delta int64) {
	if kind == StorageSession {
		b.sessionBytes[scope] += delta
	} else {
		b.localBytes[scope.origin] += delta
	}
	b.total += delta
}

func (b *StorageBroker) set(kind StorageKind, scope storageScope, key, value string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.existingDataLocked(kind, scope)
	old, exists := m[key]
	delta := int64(len(value)) - int64(len(old))
	if !exists {
		delta = int64(len(key) + len(value))
	}
	if b.bytesLocked(kind, scope)+delta > b.perOrigin || b.total+delta > b.totalLimit {
		return ErrStorageQuota
	}
	if m == nil {
		m = b.ensureDataLocked(kind, scope)
	}
	m[key] = value
	b.addBytesLocked(kind, scope, delta)
	return nil
}

func (b *StorageBroker) delete(kind StorageKind, scope storageScope, key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.existingDataLocked(kind, scope)
	old, ok := m[key]
	if !ok {
		return
	}
	delete(m, key)
	b.addBytesLocked(kind, scope, -int64(len(key)+len(old)))
}

func (b *StorageBroker) clear(kind StorageKind, scope storageScope) {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.existingDataLocked(kind, scope)
	if m == nil {
		return
	}
	for k, val := range m {
		b.addBytesLocked(kind, scope, -int64(len(k)+len(val)))
	}
	if kind == StorageSession {
		delete(b.session, scope)
	} else {
		delete(b.local, scope.origin)
	}
}

// storageView resolves the frame's origin against the tab's storage provider.
func (s *Session) storageView(kind StorageKind, f *Frame) protocol.Storage {
	if s.browser == nil {
		s.browser = NewBrowserContext()
	}
	origin := protocol.Origin{Opaque: true}
	if f != nil {
		origin = f.Origin()
	}
	provider := s.browser.storageProvider
	if provider == nil {
		provider = storageProvider{broker: s.browser.Storage()}
	}
	return provider.View(kind, s.tabID, origin)
}
