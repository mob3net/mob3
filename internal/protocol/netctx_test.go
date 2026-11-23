package protocol

import (
	"context"
	"testing"
)

func TestRequestMetaPreservesEntryID(t *testing.T) {
	ctx := WithRequestEntry(context.Background(), 42)
	ctx = WithRequestMeta(ctx, 3, "doc")
	m := RequestMetaFrom(ctx)
	if m.EntryID != 42 || m.Gen != 3 || m.Kind != "doc" {
		t.Fatalf("meta = %+v, want entry 42, gen 3, doc", m)
	}
}
