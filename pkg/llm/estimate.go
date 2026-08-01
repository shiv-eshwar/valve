package llm

import (
	"encoding/json"
	"math"
	"unicode/utf8"
)

// EstimateTokens returns a fast heuristic count: max(1, ceil(runes/4)) for
// non-empty text, or 0 for empty. Documented error bound: typically within
// ~20–30% of tiktoken for English prose; use Tokenizer for exact counts.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	n := utf8.RuneCountInString(text)
	return int(math.Ceil(float64(n) / 4.0))
}

func countText(tok Tokenizer, text string) int {
	if tok != nil {
		return tok.Count(text)
	}
	return EstimateTokens(text)
}

// ChatEstimate is the breakdown of a chat/completions-style request.
type ChatEstimate struct {
	InputTokens   int64
	OutputReserve int64
	TotalTokens   int64 // Input + OutputReserve (use as Cost.Tokens)
}

// EstimateChatRequest parses a JSON body (OpenAI chat/completions or
// completions-like) and estimates TPM cost. Adds max_tokens /
// max_completion_tokens as output reserve when present.
func EstimateChatRequest(body []byte, tok Tokenizer) (ChatEstimate, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return ChatEstimate{}, err
	}

	var input int
	if msgs, ok := raw["messages"]; ok {
		var messages []struct {
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(msgs, &messages); err != nil {
			return ChatEstimate{}, err
		}
		for _, m := range messages {
			input += contentTokens(m.Content, tok)
		}
	}
	if prompt, ok := raw["prompt"]; ok {
		input += contentTokens(prompt, tok)
	}
	if input == 0 && len(body) > 0 {
		// Fallback: whole body as text (minus structural overhead ignorance).
		input = countText(tok, string(body))
	}

	var outReserve int64
	for _, key := range []string{"max_tokens", "max_completion_tokens"} {
		if v, ok := raw[key]; ok {
			var n int64
			if err := json.Unmarshal(v, &n); err == nil && n > outReserve {
				outReserve = n
			}
		}
	}

	total := int64(input) + outReserve
	if total < 1 {
		total = 1
	}
	return ChatEstimate{
		InputTokens:   int64(input),
		OutputReserve: outReserve,
		TotalTokens:   total,
	}, nil
}

func contentTokens(raw json.RawMessage, tok Tokenizer) int {
	if len(raw) == 0 {
		return 0
	}
	// string content
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return countText(tok, s)
	}
	// array of parts: [{"type":"text","text":"..."}]
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		n := 0
		for _, p := range parts {
			if t, ok := p["text"].(string); ok {
				n += countText(tok, t)
			}
		}
		return n
	}
	return countText(tok, string(raw))
}
