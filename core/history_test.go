package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSessionScope(t *testing.T) {
	tests := []struct {
		name    string
		scope   Scope
		wantErr bool
	}{
		{"all is valid", ScopeAll, false},
		{"empty is valid", Scope(""), false},
		{"exam scope is valid", Scope("exam:2020A_FE-A"), false},
		{"part scope is valid", Scope("part:am"), false},
		{"topic scope is rejected", Scope("topic:Networks"), true},
		{"unknown kind is rejected", Scope("bogus:x"), true},
		{"malformed scope is rejected", Scope("malformed"), true},
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
		scope   Scope
		wantIDs []int64
	}{
		{"all returns everything unchanged", ScopeAll, []int64{1, 2, 3}},
		{"empty returns everything unchanged", Scope(""), []int64{1, 2, 3}},
		{"exam scope filters by exact exam id", Scope("exam:2020A_FE-A"), []int64{1, 3}},
		{"part scope filters by exam part", Scope("part:pm"), []int64{2}},
		{"exam scope with no matches returns empty", Scope("exam:nope"), nil},
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
