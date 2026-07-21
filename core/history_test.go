package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSessionScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   ScopeFilter
		wantErr bool
	}{
		{"empty is valid", ScopeFilter{}, false},
		{"exam scope is valid", ScopeFilter{ExamID: "2020A_FE-A"}, false},
		{"part scope is valid", ScopeFilter{Part: "am"}, false},
		{"topic scope is rejected", ScopeFilter{Topic: "Networks"}, true},
		{"tag scope is rejected", ScopeFilter{Tags: []string{"cache-memory"}}, true},
		{"invalid part is rejected", ScopeFilter{Part: "xx"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSessionScope(tt.scope)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFilterSessionsByScope(t *testing.T) {
	records := []SessionRecord{
		{ID: 1, ExamID: "2020A_FE-A"},
		{ID: 2, ExamID: "2020A_FE-B"},
		{ID: 3, ExamID: "2020A_FE-A"},
	}

	tests := []struct {
		name    string
		scope   ScopeFilter
		wantIDs []int64
	}{
		{"empty returns everything unchanged", ScopeFilter{}, []int64{1, 2, 3}},
		{"exam scope filters by exact exam id", ScopeFilter{ExamID: "2020A_FE-A"}, []int64{1, 3}},
		{"part scope filters by exam part", ScopeFilter{Part: "pm"}, []int64{2}},
		{"exam scope with no matches returns empty", ScopeFilter{ExamID: "nope"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterSessionsByScope(records, tt.scope)
			var gotIDs []int64
			for _, r := range got {
				gotIDs = append(gotIDs, r.ID)
			}
			assert.Equal(t, tt.wantIDs, gotIDs)
		})
	}
}

func TestParseScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   Scope
		want    ScopeFilter
		wantErr bool
	}{
		{"all means empty filter", ScopeAll, ScopeFilter{}, false},
		{"empty means empty filter", Scope(""), ScopeFilter{}, false},
		{"topic", Scope("topic:Networks"), ScopeFilter{Topic: "Networks"}, false},
		{"tag", Scope("tag:cache-memory"), ScopeFilter{Tags: []string{"cache-memory"}}, false},
		{"exam", Scope("exam:2020A_FE-A"), ScopeFilter{ExamID: "2020A_FE-A"}, false},
		{"part", Scope("part:am"), ScopeFilter{Part: "am"}, false},
		{"part is lowercased", Scope("part:AM"), ScopeFilter{Part: "am"}, false},
		{"invalid part errors", Scope("part:xx"), ScopeFilter{}, true},
		{"unknown kind errors", Scope("bogus:x"), ScopeFilter{}, true},
		{"malformed (no colon) errors", Scope("malformed"), ScopeFilter{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseScope(tt.scope)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
