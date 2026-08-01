package llm_test

import (
	"strings"
	"testing"

	"github.com/shiv-eshwar/valve/pkg/llm"
)

func TestParseUsageOpenAI(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	u, ok := llm.ParseUsageJSON(body)
	if !ok || u.InputTokens != 10 || u.OutputTokens != 5 || u.Total() != 15 {
		t.Fatalf("%+v ok=%v", u, ok)
	}
}

func TestParseUsageAnthropic(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":3,"output_tokens":7}}`)
	u, ok := llm.ParseUsageJSON(body)
	if !ok || u.InputTokens != 3 || u.OutputTokens != 7 {
		t.Fatalf("%+v", u)
	}
}

func TestParseUsageSSE(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"1","choices":[{"delta":{"content":"hi"}}]}`,
		``,
		`data: {"usage":{"prompt_tokens":2,"completion_tokens":4}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	u, ok := llm.ParseUsageSSE(strings.NewReader(sse))
	if !ok || u.Total() != 6 {
		t.Fatalf("%+v ok=%v", u, ok)
	}
}

func TestRecordSettle(t *testing.T) {
	var m llm.Metrics
	m.Record(100, 80)
	m.Record(50, 60)
	m.Record(10, 10)
	s := m.Snapshot()
	if s.OverEstimate != 1 || s.UnderEstimate != 1 || s.Exact != 1 {
		t.Fatalf("%+v", s)
	}
	if s.EstimateTotal != 160 || s.ActualTotal != 150 {
		t.Fatalf("%+v", s)
	}
}
