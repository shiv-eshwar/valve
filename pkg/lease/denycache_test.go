package lease

import (
	"testing"
	"time"

	"github.com/shiv-eshwar/valve/pkg/api"
)

func TestDenyCacheSetGetClear(t *testing.T) {
	c := NewDenyCache()
	key := api.Key{Subject: "s", Model: "m"}
	if _, _, ok := c.Get(key); ok {
		t.Fatal("expected empty")
	}
	c.Set(key, api.LimitTypeTokens, 50*time.Millisecond)
	lt, retry, ok := c.Get(key)
	if !ok || lt != api.LimitTypeTokens || retry <= 0 {
		t.Fatalf("lt=%v retry=%v ok=%v", lt, retry, ok)
	}
	c.Clear(key)
	if _, _, ok := c.Get(key); ok {
		t.Fatal("expected cleared")
	}
}
