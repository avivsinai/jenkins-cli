package run

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCollectRerunParametersSkipsNilValues(t *testing.T) {
	detail := runDetail{
		Parameters: []map[string]any{
			{"name": "STRING_VALUE", "value": "hello"},
			{"name": "BOOL_VALUE", "value": true},
			{"name": "NIL_VALUE", "value": nil},
		},
	}

	got := collectRerunParameters(detail)

	require.Equal(t, map[string]string{
		"STRING_VALUE": "hello",
		"BOOL_VALUE":   "true",
	}, got)
	require.NotContains(t, got, "NIL_VALUE")
}

func TestDisplayParameterValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "nil renders as empty string", value: nil, want: ""},
		{name: "string value unchanged", value: "hello", want: "hello"},
		{name: "empty string stays empty", value: "", want: ""},
		{name: "bool true", value: true, want: "true"},
		{name: "bool false", value: false, want: "false"},
		{name: "int64 zero", value: int64(0), want: "0"},
		{name: "int64 nonzero", value: int64(42), want: "42"},
		{name: "float64", value: 3.14, want: "3.14"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := displayParameterValue(tt.value)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCollectRerunParametersUsesActionParametersWhenNeeded(t *testing.T) {
	detail := runDetail{
		Actions: []map[string]any{
			{
				"parameters": []any{
					map[string]any{"name": "FROM_ACTION", "value": "build#42"},
					map[string]any{"name": "EMPTY_RUN_PARAM", "value": nil},
				},
			},
		},
	}

	got := collectRerunParameters(detail)

	require.Equal(t, map[string]string{
		"FROM_ACTION": "build#42",
	}, got)
	require.NotContains(t, got, "EMPTY_RUN_PARAM")
}

func TestCollectRerunParametersPreservesZeroValues(t *testing.T) {
	detail := runDetail{
		Parameters: []map[string]any{
			{"name": "EMPTY_STR", "value": ""},
			{"name": "ZERO_INT", "value": 0},
			{"name": "FALSE_BOOL", "value": false},
			{"name": "NIL_VAL", "value": nil},
		},
	}

	got := collectRerunParameters(detail)

	require.Equal(t, map[string]string{
		"EMPTY_STR":  "",
		"ZERO_INT":   "0",
		"FALSE_BOOL": "false",
	}, got)
	require.NotContains(t, got, "NIL_VAL")
}
