package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"

	"github.com/jim-ww/itpec-sensei/core"
)

// addScopeFlags registers --topic/--tags/--exam/--part on fs, combinable
// with AND (like practice's filters) — used by every progress-query command
// (summary, history, sessions), unlike reset's single scope string which is
// deliberately kept as-is (see core.ParseScope). topicSupported is false for
// "sessions", which rejects topic/tag scope (see core.validateSessionScope).
func addScopeFlags(fs *pflag.FlagSet, topic, examID, part *string, tags *[]string, topicSupported bool) {
	if topicSupported {
		fs.StringVar(topic, "topic", "", "filter to one topic (see \"itpec-sensei topics\" for valid names)")
		fs.StringSliceVar(tags, "tags", nil, "filter to questions carrying any of these tags (comma-separated, or repeat the flag; see \"itpec-sensei tags\" for valid names)")
	}
	fs.StringVarP(examID, "exam", "e", "", "filter to one exam id")
	fs.StringVar(part, "part", "", "am | pm — which exam session to scope to")
}

// scopeFromFlags validates --part and builds the combined core.ScopeFilter.
func scopeFromFlags(topic, examID, part string, tags []string) (core.ScopeFilter, error) {
	p := strings.ToLower(part)
	switch p {
	case "am", "pm", "":
	default:
		return core.ScopeFilter{}, fmt.Errorf("invalid --part %q, expected am or pm", part)
	}
	return core.ScopeFilter{Topic: topic, Tags: tags, ExamID: examID, Part: p}, nil
}

// scopeLabel renders a ScopeFilter for display, e.g. in "(scope=..., ...)" headers.
func scopeLabel(f core.ScopeFilter) string {
	if f.IsEmpty() {
		return "all"
	}
	var parts []string
	if f.Topic != "" {
		parts = append(parts, "topic:"+f.Topic)
	}
	for _, t := range f.Tags {
		parts = append(parts, "tag:"+t)
	}
	if f.ExamID != "" {
		parts = append(parts, "exam:"+f.ExamID)
	}
	if f.Part != "" {
		parts = append(parts, "part:"+f.Part)
	}
	return strings.Join(parts, ",")
}
