package engine

import (
	"testing"
	"time"
)

func TestPageCacheBudgetAndDrop(t *testing.T) {
	c := NewPageCache(2 << 20)
	now := time.Now()
	c.Put(1, &CachedPage{Page: &Page{}, Bytes: 1 << 20, At: now})
	c.Put(2, &CachedPage{Page: &Page{}, Bytes: 1 << 20, At: now.Add(time.Second)})
	c.Put(3, &CachedPage{Page: &Page{}, Bytes: 1 << 20, At: now.Add(2 * time.Second)})
	if c.Len() != 2 || c.CachedBytes() > c.LimitBytes() {
		t.Fatalf("len=%d bytes=%d limit=%d", c.Len(), c.CachedBytes(), c.LimitBytes())
	}
	if c.Get(1) != nil {
		t.Fatal("oldest entry survived")
	}
	c.DropAll()
	if c.Len() != 0 {
		t.Fatal("drop left entries")
	}
}
