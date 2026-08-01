package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// Usage is provider-normalized token usage.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

// Total returns input+output.
func (u Usage) Total() int64 { return u.InputTokens + u.OutputTokens }

// ParseUsageJSON extracts usage from an OpenAI or Anthropic-style JSON body.
func ParseUsageJSON(body []byte) (Usage, bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return Usage{}, false
	}
	raw, ok := root["usage"]
	if !ok {
		return Usage{}, false
	}
	var u map[string]int64
	if err := json.Unmarshal(raw, &u); err != nil {
		return Usage{}, false
	}
	in := firstInt(u, "prompt_tokens", "input_tokens")
	out := firstInt(u, "completion_tokens", "output_tokens")
	if in == 0 && out == 0 {
		if t, ok := u["total_tokens"]; ok && t > 0 {
			return Usage{InputTokens: t}, true
		}
		return Usage{}, false
	}
	return Usage{InputTokens: in, OutputTokens: out}, true
}

func firstInt(m map[string]int64, keys ...string) int64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return 0
}

// ParseUsageSSE scans an SSE stream and returns the last usage block found.
func ParseUsageSSE(r io.Reader) (Usage, bool) {
	sc := bufio.NewScanner(r)
	// Increase buffer for large chunks.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	var last Usage
	found := false
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" || data == "" {
			continue
		}
		if u, ok := parseUsageFromChunk([]byte(data)); ok {
			last = u
			found = true
		}
	}
	return last, found
}

func parseUsageFromChunk(data []byte) (Usage, bool) {
	// Full response-shaped chunk with usage.
	if u, ok := ParseUsageJSON(data); ok {
		return u, true
	}
	// Delta chunk may nest usage at top level still.
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return Usage{}, false
	}
	if raw, ok := root["usage"]; ok && !bytes.Equal(raw, []byte("null")) {
		var u map[string]int64
		if err := json.Unmarshal(raw, &u); err == nil {
			in := firstInt(u, "prompt_tokens", "input_tokens")
			out := firstInt(u, "completion_tokens", "output_tokens")
			if in != 0 || out != 0 {
				return Usage{InputTokens: in, OutputTokens: out}, true
			}
		}
	}
	return Usage{}, false
}

// TeeReadAll reads all bytes while returning them (helper for non-stream).
func TeeReadAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}
