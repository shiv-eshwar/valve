package llm

// Tokenizer counts tokens in text. When nil is passed to estimators, the
// char/4 heuristic is used instead.
type Tokenizer interface {
	Count(text string) int
}

// HeuristicTokenizer implements Tokenizer via EstimateTokens.
type HeuristicTokenizer struct{}

// Count implements Tokenizer.
func (HeuristicTokenizer) Count(text string) int {
	return EstimateTokens(text)
}
