package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScopeQuestionIDs(t *testing.T) {
	bank := newTestBank(t)
	c := &Core{Bank: bank}

	tests := []struct {
		name    string
		scope   Scope
		want    []string // nil means "expect nil (no filter)"
		wantErr bool
	}{
		{"all scope means no filter", ScopeAll, nil, false},
		{"empty scope means no filter", Scope(""), nil, false},
		{"topic scope matches only that topic", Scope("topic:Networks"), []string{"2020A_FE-A#1"}, false},
		{"exam scope matches only that exam", Scope("exam:2020A_FE-B"), []string{"2020A_FE-B#1"}, false},
		{"part scope am matches am exam questions", Scope("part:am"), []string{"2020A_FE-A#1", "2020A_FE-A#2"}, false},
		{"part scope pm matches pm exam questions", Scope("part:pm"), []string{"2020A_FE-B#1"}, false},
		{"part scope is case insensitive", Scope("part:AM"), []string{"2020A_FE-A#1", "2020A_FE-A#2"}, false},
		{"unknown topic yields empty non-nil set", Scope("topic:NoSuchTopic"), []string{}, false},
		{"invalid part value errors", Scope("part:xx"), nil, true},
		{"invalid scope kind errors", Scope("bogus:x"), nil, true},
		{"malformed scope (no colon) errors", Scope("malformed"), nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ids, err := c.scopeQuestionIDs(tt.scope)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.want == nil {
				assert.Nil(t, ids)
				return
			}
			require.NotNil(t, ids)
			got := make([]string, 0, len(ids))
			for id := range ids {
				got = append(got, id)
			}
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestQuestionIDList(t *testing.T) {
	t.Run("nil set stays nil", func(t *testing.T) {
		assert.Nil(t, questionIDList(nil))
	})
	t.Run("empty non-nil set becomes empty non-nil slice", func(t *testing.T) {
		got := questionIDList(map[string]struct{}{})
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})
	t.Run("populated set becomes a slice with all members", func(t *testing.T) {
		got := questionIDList(map[string]struct{}{"a": {}, "b": {}})
		assert.ElementsMatch(t, []string{"a", "b"}, got)
	})
}
