package protocol

import (
	"context"
	"net"
	"reflect"
	"testing"
)

type testStorage struct {
	data map[string]string
}

func (s *testStorage) Get(key string) (string, bool) {
	v, ok := s.data[key]
	return v, ok
}
func (s *testStorage) Set(key, value string) error {
	s.data[key] = value
	return nil
}
func (s *testStorage) Delete(key string) { delete(s.data, key) }
func (s *testStorage) Clear()            { s.data = make(map[string]string) }
func (s *testStorage) Keys() []string {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys
}
func (s *testStorage) Len() int                    { return len(s.data) }
func (s *testStorage) Snapshot() map[string]string { return s.data }

type testStorageProvider struct{ view *testStorage }

func (p *testStorageProvider) View(StorageKind, uint64, Origin) Storage { return p.view }

func TestStorageCallRoundTrip(t *testing.T) {
	in := StorageCall{
		Op: StorageOpSet, ID: 9, Kind: StorageSession, TabID: 7,
		Origin: Origin{Scheme: "https", Host: "example.test"},
		Key:    "k", Value: "v",
	}
	body, err := AppendStorageCall(nil, in, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeStorageCall(body, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("storage call:\n in: %#v\nout: %#v", in, out)
	}
	result := StorageResult{
		ID: 9, Value: "v", Found: true,
		Values: map[string]string{"a": "b"}, Strings: []string{"a"}, Int: 2,
	}
	body, err = AppendStorageResult(nil, result, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeStorageResult(body, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result, got) {
		t.Fatalf("storage result:\n in: %#v\nout: %#v", result, got)
	}
}

func TestStorageClientServer(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	parentPeer := NewMuxPeerReadWriter(server, DefaultLimits)
	childPeer := NewMuxPeerReadWriter(client, DefaultLimits)
	provider := &testStorageProvider{view: &testStorage{data: make(map[string]string)}}
	NewStorageServerPeer(parentPeer, provider)
	storageClient := NewStorageClientPeer(childPeer)
	go func() { _ = parentPeer.Run(ctx) }()
	go func() { _ = childPeer.Run(ctx) }()

	view := storageClient.View(StorageLocal, 7, Origin{Scheme: "https", Host: "example.test"})
	if err := view.Set("a", "b"); err != nil {
		t.Fatal(err)
	}
	if v, ok := view.Get("a"); !ok || v != "b" {
		t.Fatalf("Get = %q, %v", v, ok)
	}
	if got := view.Len(); got != 1 {
		t.Fatalf("Len = %d", got)
	}
	if got := view.Keys(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("Keys = %v", got)
	}
	if got := view.Snapshot(); got["a"] != "b" {
		t.Fatalf("Snapshot = %v", got)
	}
	view.Delete("a")
	if _, ok := view.Get("a"); ok {
		t.Fatal("Delete did not remove the key")
	}
	if err := view.Set("b", "c"); err != nil {
		t.Fatal(err)
	}
	view.Clear()
	if got := view.Len(); got != 0 {
		t.Fatalf("Len after Clear = %d", got)
	}
}
