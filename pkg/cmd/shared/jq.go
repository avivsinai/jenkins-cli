package shared

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/itchyny/gojq"
	"github.com/spf13/cobra"
)

// GetJQExpression retrieves the --jq flag value from the root command.
func GetJQExpression(cmd *cobra.Command) string {
	v, _ := cmd.Root().PersistentFlags().GetString("jq")
	return v
}

// WantsJQ returns true if --jq flag is set with a non-empty expression.
func WantsJQ(cmd *cobra.Command) bool {
	return GetJQExpression(cmd) != ""
}

// ApplyJQ executes a jq expression on data and writes results to w.
// Strings are output without quotes (gh-ux pattern).
func ApplyJQ(data interface{}, expression string, w io.Writer) error {
	query, err := gojq.Parse(expression)
	if err != nil {
		return fmt.Errorf("invalid jq expression: %w", err)
	}

	// Marshal then unmarshal to normalize Go structs into JSON-compatible maps.
	// This is intentional: gojq expects map[string]interface{} (not typed structs),
	// and this round-trip ensures consistent behavior regardless of input type
	// (struct, map, slice, etc.) by converting everything to the JSON data model.
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal data for jq: %w", err)
	}

	var input interface{}
	if err := json.Unmarshal(jsonBytes, &input); err != nil {
		return fmt.Errorf("unmarshal for jq: %w", err)
	}

	iter := query.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			return fmt.Errorf("jq evaluation: %w", err)
		}

		// gh-ux pattern: strings without quotes
		if s, isString := v.(string); isString {
			fmt.Fprintln(w, s)
			continue
		}

		output, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Errorf("format jq result: %w", err)
		}
		fmt.Fprintln(w, string(output))
	}
	return nil
}
