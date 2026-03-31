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
