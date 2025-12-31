package shared

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyJQ(t *testing.T) {
	tests := []struct {
		name       string
		data       interface{}
		expression string
		pretty     bool
		prettySet  bool
		want       string
		wantErr    bool
	}{
		{
			name:       "identity expression returns entire input",
			data:       map[string]interface{}{"result": "SUCCESS", "number": 42},
			expression: ".",
			want:       "{\n  \"number\": 42,\n  \"result\": \"SUCCESS\"\n}\n",
		},
		{
			name:       "string field without quotes",
			data:       map[string]interface{}{"result": "SUCCESS"},
			expression: ".result",
			want:       "SUCCESS\n",
		},
		{
			name:       "numeric field",
			data:       map[string]interface{}{"number": 123},
			expression: ".number",
			want:       "123\n",
		},
		{
			name:       "object construction",
			data:       map[string]interface{}{"result": "SUCCESS", "duration": 5000},
			expression: "{status: .result, time: .duration}",
			want:       "{\n  \"status\": \"SUCCESS\",\n  \"time\": 5000\n}\n",
		},
		{
			name:       "array iteration",
			data:       map[string]interface{}{"items": []interface{}{map[string]interface{}{"n": 1}, map[string]interface{}{"n": 2}}},
			expression: ".items[].n",
			want:       "1\n2\n",
		},
		{
			name:       "invalid expression",
			data:       map[string]interface{}{"x": 1},
			expression: ".[invalid",
			wantErr:    true,
		},
		{
			name:       "null result",
			data:       map[string]interface{}{"x": 1},
			expression: ".missing",
			want:       "null\n",
		},
		{
			name:       "boolean field",
			data:       map[string]interface{}{"active": true},
			expression: ".active",
			want:       "true\n",
		},
		{
			name:       "array access",
			data:       map[string]interface{}{"items": []interface{}{"a", "b", "c"}},
			expression: ".items[0]",
			want:       "a\n",
		},
		{
			name:       "nested object",
			data:       map[string]interface{}{"outer": map[string]interface{}{"inner": "value"}},
			expression: ".outer.inner",
			want:       "value\n",
		},
		{
			name:       "select filter",
			data:       map[string]interface{}{"items": []interface{}{map[string]interface{}{"n": 1}, map[string]interface{}{"n": 2}, map[string]interface{}{"n": 3}}},
			expression: ".items[] | select(.n > 1) | .n",
			want:       "2\n3\n",
		},
		{
			name:       "compact output when not pretty",
			data:       map[string]interface{}{"result": "SUCCESS", "number": 42},
			expression: ".",
			pretty:     false,
			prettySet:  true,
			want:       "{\"number\":42,\"result\":\"SUCCESS\"}\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			pretty := true
			if tt.prettySet {
				pretty = tt.pretty
			}
			err := ApplyJQ(tt.data, tt.expression, &buf, pretty)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, buf.String())
		})
	}
}
