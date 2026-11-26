package protocol

import "errors"

// ErrStorageQuota reports a storage write that would exceed an area or context
// limit. The sentinel lives here so a process-backed storage provider can
// preserve errors.Is across the boundary.
var ErrStorageQuota = errors.New("storage quota exceeded")

// StorageKind selects localStorage or sessionStorage.
type StorageKind uint8

const (
	StorageLocal StorageKind = iota
	StorageSession
)

// Storage is one origin's storage area. BrowserContext implementations use the
// in-process StorageView; a process-backed engine uses a remote view.
type Storage interface {
	Get(key string) (string, bool)
	Set(key, value string) error
	Delete(key string)
	Clear()
	Keys() []string
	Len() int
	Snapshot() map[string]string
}

// StorageProvider resolves one tab/origin pair to a storage area. localStorage
// is shared across tabs for an origin; sessionStorage is scoped by tab.
type StorageProvider interface {
	View(kind StorageKind, tabID uint64, origin Origin) Storage
}
