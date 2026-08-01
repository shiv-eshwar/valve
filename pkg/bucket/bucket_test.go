package bucket

import (
	"testing"
	"time"
)

func TestColdKeyStartsFull(t *testing.T) {
	s := State{Capacity: 60}
	s, res := TryConsume(s, 1_000_000, 1)
	if !res.Allowed {
		t.Fatal("expected allow on cold bucket")
	}
	if res.Remaining != 59 {
		t.Fatalf("remaining=%d want 59", res.Remaining)
	}
}

func TestDenyWhenEmpty(t *testing.T) {
	s := State{Tokens: 0, LastMs: 1_000_000, Capacity: 10}
	_, res := TryConsume(s, 1_000_000, 1)
	if res.Allowed {
		t.Fatal("expected deny")
	}
	if res.RetryAfter <= 0 {
		t.Fatal("expected positive retry_after")
	}
}

func TestExactEmptyAfterConsume(t *testing.T) {
	s := State{Tokens: 5, LastMs: 1_000_000, Capacity: 10}
	s, res := TryConsume(s, 1_000_000, 5)
	if !res.Allowed || res.Remaining != 0 {
		t.Fatalf("allowed=%v remaining=%d", res.Allowed, res.Remaining)
	}
	_, res = TryConsume(s, 1_000_000, 1)
	if res.Allowed {
		t.Fatal("expected deny after empty")
	}
}

func TestFractionalRefill(t *testing.T) {
	// capacity 60 => 1 token/sec
	s := State{Tokens: 0, LastMs: 0, Capacity: 60}
	s = Refill(s, 1_000_000) // cold -> full
	s.Tokens = 0
	s.LastMs = 1_000_000
	s = Refill(s, 1_000_000+2500) // 2.5s => 2.5 tokens
	if s.Tokens < 2.4 || s.Tokens > 2.6 {
		t.Fatalf("tokens=%v want ~2.5", s.Tokens)
	}
	_, res := TryConsume(s, 1_000_000+2500, 2)
	if !res.Allowed {
		t.Fatal("expected allow after refill")
	}
}

func TestCostGreaterThanCapacity(t *testing.T) {
	s := State{Capacity: 10}
	_, res := TryConsume(s, 1_000, 11)
	if res.Allowed {
		t.Fatal("expected deny when cost > capacity")
	}
}

func TestBurstThenRefill(t *testing.T) {
	s := State{Capacity: 10}
	now := int64(1_000_000)
	for i := 0; i < 10; i++ {
		var res Result
		s, res = TryConsume(s, now, 1)
		if !res.Allowed {
			t.Fatalf("burst item %d denied", i)
		}
	}
	_, res := TryConsume(s, now, 1)
	if res.Allowed {
		t.Fatal("11th should deny")
	}
	// wait 6s at 10/60 per sec => 1 token
	s, res = TryConsume(s, now+6000, 1)
	if !res.Allowed {
		t.Fatalf("expected allow after refill, retry=%v", res.RetryAfter)
	}
}

func TestCreditCapsAtCapacity(t *testing.T) {
	s := State{Tokens: 8, LastMs: 1000, Capacity: 10}
	s = Credit(s, 1000, 100)
	if RemainingInt(s.Tokens) != 10 {
		t.Fatalf("tokens=%v want 10", s.Tokens)
	}
}

func TestDebitFloorOvershoot(t *testing.T) {
	s := State{Tokens: 3, LastMs: 1000, Capacity: 10}
	s, debited, over := DebitFloor(s, 1000, 5)
	if debited != 3 || over != 2 {
		t.Fatalf("debited=%d over=%d", debited, over)
	}
	if RemainingInt(s.Tokens) != 0 {
		t.Fatalf("remaining=%d", RemainingInt(s.Tokens))
	}
}

func TestRetryAfterDuration(t *testing.T) {
	s := State{Tokens: 0, LastMs: 1000, Capacity: 60} // 1/sec
	_, res := TryConsume(s, 1000, 3)
	if res.RetryAfter != 3*time.Second {
		t.Fatalf("retry=%v want 3s", res.RetryAfter)
	}
}
