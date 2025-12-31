package shared

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyTemplate(t *testing.T) {
	tests := []struct {
		name     string
		data     interface{}
		template string
		want     string
		wantErr  string
	}{
		{
			name:     "simple field access",
			data:     map[string]string{"name": "test-job"},
			template: "Job: {{.name}}",
			want:     "Job: test-job\n",
		},
		{
			name:     "nested field access",
			data:     map[string]interface{}{"build": map[string]int{"number": 42}},
			template: "Build #{{.build.number}}",
			want:     "Build #42\n",
		},
		{
			name:     "json function",
			data:     map[string]interface{}{"items": []string{"a", "b", "c"}},
			template: `{{json .items}}`,
			want:     "[\"a\",\"b\",\"c\"]\n",
		},
		{
			name:     "join function",
			data:     map[string]interface{}{"tags": []interface{}{"v1", "v2", "v3"}},
			template: `{{join ", " .tags}}`,
			want:     "v1, v2, v3\n",
		},
		{
			name:     "upper function",
			data:     map[string]string{"status": "success"},
			template: `{{upper .status}}`,
			want:     "SUCCESS\n",
		},
		{
			name:     "lower function",
			data:     map[string]string{"status": "FAILURE"},
			template: `{{lower .status}}`,
			want:     "failure\n",
		},
		{
			name:     "trim function",
			data:     map[string]string{"text": "  spaces  "},
			template: `[{{trim .text}}]`,
			want:     "[spaces]\n",
		},
		{
			name:     "duration function with milliseconds",
			data:     map[string]int64{"duration_ms": 65000},
			template: `Duration: {{duration .duration_ms}}`,
			want:     "Duration: 1m5s\n",
		},
		{
			name:     "range over array",
			data:     map[string]interface{}{"builds": []interface{}{1, 2, 3}},
			template: `{{range .builds}}#{{.}} {{end}}`,
			want:     "#1 #2 #3 \n",
		},
		{
			name:     "conditional with if",
			data:     map[string]interface{}{"result": "SUCCESS"},
			template: `{{if eq .result "SUCCESS"}}passed{{else}}failed{{end}}`,
			want:     "passed\n",
		},
		{
			name:     "missing field returns empty",
			data:     map[string]string{"name": "test"},
			template: `[{{.missing}}]`,
			want:     "[<no value>]\n",
		},
		{
			name:     "invalid template syntax",
			data:     map[string]string{"name": "test"},
			template: `{{.name`,
			wantErr:  "invalid template",
		},
		{
			name:     "template execution error",
			data:     map[string]string{"name": "test"},
			template: `{{call .name}}`,
			wantErr:  "template execution",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := ApplyTemplate(tt.data, tt.template, &buf)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, buf.String())
		})
	}
}

func TestTemplateFuncsAvailable(t *testing.T) {
	// Verify all expected template functions are available
	funcs := []string{"json", "join", "upper", "lower", "trim", "timeago", "duration"}

	for _, fn := range funcs {
		t.Run(fn, func(t *testing.T) {
			fmap := templateFuncs()
			require.Contains(t, fmap, fn, "template function %q should be available", fn)
		})
	}
}
