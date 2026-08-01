package llm_test

import (
	"testing"

	"github.com/shiv-eshwar/valve/pkg/llm"
)

func TestEstimateTokens(t *testing.T) {
	if llm.EstimateTokens("") != 0 {
		t.Fatal("empty")
	}
	// 4 runes => 1 token
	if n := llm.EstimateTokens("abcd"); n != 1 {
		t.Fatalf("got %d", n)
	}
	// 5 runes => ceil(5/4)=2
	if n := llm.EstimateTokens("abcde"); n != 2 {
		t.Fatalf("got %d", n)
	}
}

func TestEstimateChatRequest(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4o",
		"messages":[{"role":"user","content":"abcd"}],
		"max_tokens":100
	}`)
	est, err := llm.EstimateChatRequest(body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if est.InputTokens != 1 {
		t.Fatalf("input=%d", est.InputTokens)
	}
	if est.OutputReserve != 100 {
		t.Fatalf("out=%d", est.OutputReserve)
	}
	if est.TotalTokens != 101 {
		t.Fatalf("total=%d", est.TotalTokens)
	}
}

func TestEstimateChatRequestParts(t *testing.T) {
	body := []byte(`{
		"messages":[{"role":"user","content":[{"type":"text","text":"abcd"}]}]
	}`)
	est, err := llm.EstimateChatRequest(body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if est.InputTokens != 1 || est.TotalTokens != 1 {
		t.Fatalf("%+v", est)
	}
}

type fixedTok int

func (f fixedTok) Count(string) int { return int(f) }

func TestTokenizerHook(t *testing.T) {
	body := []byte(`{"messages":[{"content":"hello"}]}`)
	est, err := llm.EstimateChatRequest(body, fixedTok(42))
	if err != nil {
		t.Fatal(err)
	}
	if est.InputTokens != 42 {
		t.Fatalf("got %d", est.InputTokens)
	}
}
