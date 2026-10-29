package policy

// BuiltinCorpus returns the calls of the built-in decision corpus, for the
// external tests (default_mode_test.go).
func BuiltinCorpus() []ToolCall {
	out := make([]ToolCall, 0, len(builtinDecisionCases))
	for _, c := range builtinDecisionCases {
		out = append(out, c.call)
	}
	return out
}
