package shared

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/template"
	"time"

	"github.com/spf13/cobra"
)

// GetTemplate retrieves the --template flag value from the root command.
func GetTemplate(cmd *cobra.Command) string {
	v, _ := cmd.Root().PersistentFlags().GetString("template")
	return v
}

// WantsTemplate returns true if --template flag is set.
func WantsTemplate(cmd *cobra.Command) bool {
	return GetTemplate(cmd) != ""
}

// ApplyTemplate executes a Go template on data and writes results to w.
func ApplyTemplate(data interface{}, tmpl string, w io.Writer) error {
	// Convert to JSON-compatible map for consistent field access
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal data for template: %w", err)
	}

	var input interface{}
	if err := json.Unmarshal(jsonBytes, &input); err != nil {
		return fmt.Errorf("unmarshal for template: %w", err)
	}

	// Create template with helper functions
	t, err := template.New("output").Funcs(templateFuncs()).Parse(tmpl)
	if err != nil {
		return fmt.Errorf("invalid template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, input); err != nil {
		return fmt.Errorf("template execution: %w", err)
	}

	output := buf.String()
	// Ensure output ends with newline if it has content
	if len(output) > 0 && !strings.HasSuffix(output, "\n") {
		output += "\n"
	}

	_, err = io.WriteString(w, output)
	return err
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"json": func(v interface{}) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
		"join": func(sep string, items interface{}) string {
			switch v := items.(type) {
			case []interface{}:
				strs := make([]string, len(v))
				for i, item := range v {
					strs[i] = fmt.Sprint(item)
				}
				return strings.Join(strs, sep)
			case []string:
				return strings.Join(v, sep)
			default:
				return fmt.Sprint(items)
			}
		},
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"trim":  strings.TrimSpace,
		"timeago": func(ts string) string {
			t, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				return ts
			}
			d := time.Since(t)
			switch {
			case d < time.Minute:
				return "just now"
			case d < time.Hour:
				return fmt.Sprintf("%dm ago", int(d.Minutes()))
			case d < 24*time.Hour:
				return fmt.Sprintf("%dh ago", int(d.Hours()))
			default:
				return fmt.Sprintf("%dd ago", int(d.Hours()/24))
			}
		},
		"duration": func(ms interface{}) string {
			var millis int64
			switch v := ms.(type) {
			case float64:
				millis = int64(v)
			case int64:
				millis = v
			case int:
				millis = int64(v)
			default:
				return fmt.Sprint(ms)
			}
			return DurationString(millis)
		},
	}
}
