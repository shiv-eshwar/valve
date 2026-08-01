package llm_test

import (
	"errors"
	"testing"

	"github.com/shiv-eshwar/valve/pkg/llm"
)

func TestGate(t *testing.T) {
	g := llm.Gate{MaxInputTokens: 10, MaxRequestBytes: 100}
	if err := g.CheckBytes(50); err != nil {
		t.Fatal(err)
	}
	if err := g.CheckBytes(101); !errors.Is(err, llm.ErrTooLarge) {
		t.Fatalf("err=%v", err)
	}
	if err := g.CheckInput(11); !errors.Is(err, llm.ErrTooLarge) {
		t.Fatalf("err=%v", err)
	}
}
