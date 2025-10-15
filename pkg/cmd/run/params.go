package run

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/internal/filter"
	"github.com/your-org/jenkins-cli/internal/jenkins"
	"github.com/your-org/jenkins-cli/pkg/cmd/shared"
	"github.com/your-org/jenkins-cli/pkg/cmdutil"
)

const (
	paramsSourceAuto   = "auto"
	paramsSourceConfig = "config"
	paramsSourceRuns   = "runs"
)

func newRunParamsCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		source    string
		limitRuns int
	)

	cmd := &cobra.Command{
		Use:   "params <jobPath>",
		Short: "Discover job parameter definitions",
		Example: `  # Infer parameters from job configuration
	jk run params Helm.Chart.Deploy --source config --json

	# Auto-discover parameters using the last 20 runs
	jk run params Helm.Chart.Deploy --source runs --limit-runs 20

	# Combine with jk run ls for an agent workflow
	jk run ls Helm.Chart.Deploy --select parameters --filter param.CHART_NAME~nova --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			src := strings.TrimSpace(strings.ToLower(source))
			if src == "" {
				src = paramsSourceAuto
			}
			switch src {
			case paramsSourceAuto, paramsSourceConfig, paramsSourceRuns:
			default:
				return fmt.Errorf("unsupported source %q (expected auto, config, runs)", source)
			}

			if limitRuns <= 0 {
				limitRuns = 50
			}

			jobPath := args[0]
			var (
				params     []runParameterInfo
				usedSource string
			)

			switch src {
			case paramsSourceConfig:
				params, err = fetchParamsFromConfig(ctx, client, jobPath)
				usedSource = paramsSourceConfig
			case paramsSourceRuns:
				params, err = fetchParamsFromRuns(ctx, client, jobPath, limitRuns)
				usedSource = paramsSourceRuns
			case paramsSourceAuto:
				params, err = fetchParamsFromConfig(ctx, client, jobPath)
				usedSource = paramsSourceConfig
				if err != nil || len(params) == 0 {
					paramsRuns, runsErr := fetchParamsFromRuns(ctx, client, jobPath, limitRuns)
					if runsErr == nil {
						params = paramsRuns
						usedSource = paramsSourceRuns
						err = nil
					} else if err == nil {
						err = runsErr
					}
				}
			}

			if err != nil {
				return err
			}

			sort.Slice(params, func(i, j int) bool {
				return strings.ToLower(params[i].Name) < strings.ToLower(params[j].Name)
			})

			output := runParamsOutput{
				JobPath:    normalizeJobPath(jobPath),
				Source:     usedSource,
				Parameters: params,
			}

			return shared.PrintOutput(cmd, output, func() error {
				return renderRunParamsHuman(cmd, output)
			})
		},
	}

	cmd.Flags().StringVar(&source, "source", paramsSourceAuto, "Parameter source: auto, config, or runs")
	cmd.Flags().IntVar(&limitRuns, "limit-runs", 50, "Number of recent runs to scan when inferring parameters")

	return cmd
}

func fetchParamsFromConfig(ctx context.Context, client *jenkins.Client, jobPath string) ([]runParameterInfo, error) {
	path := fmt.Sprintf("/%s/config.xml", jenkins.EncodeJobPath(jobPath))
	req := client.NewRequest().SetHeader("Accept", "application/xml")
	req.SetContext(ctx)

	resp, err := client.Do(req, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode() >= 400 {
		return nil, fmt.Errorf("fetch job config failed: %s", resp.Status())
	}

	data := resp.Body()
	if len(data) == 0 {
		return nil, nil
	}

	params, err := parseParametersFromConfig(data)
	if err != nil {
		return nil, err
	}
	return params, nil
}

func fetchParamsFromRuns(ctx context.Context, client *jenkins.Client, jobPath string, limit int) ([]runParameterInfo, error) {
	opts := runListOptions{
		Limit:    limit,
		WithMeta: true,
	}

	output, err := executeRunList(ctx, client, jobPath, opts)
	if err != nil {
		return nil, err
	}

	if output.Metadata == nil {
		return nil, nil
	}

	params := append([]runParameterInfo(nil), output.Metadata.Parameters...)
	for i := range params {
		if params[i].Frequency == 0 {
			params[i].Frequency = 1
		}
	}
	return params, nil
}

func renderRunParamsHuman(cmd *cobra.Command, output runParamsOutput) error {
	w := cmd.OutOrStdout()

	if len(output.Parameters) == 0 {
		_, _ = fmt.Fprintf(w, "No parameters found for %s (source: %s)\n", output.JobPath, output.Source)
		return nil
	}

	_, _ = fmt.Fprintf(w, "Parameters for %s (source: %s):\n\n", output.JobPath, output.Source)
	for _, param := range output.Parameters {
		typeLabel := param.Type
		if strings.TrimSpace(typeLabel) == "" {
			typeLabel = "string"
		}
		freq := "optional"
		if param.Frequency >= 0.999 {
			freq = "required"
		}
		_, _ = fmt.Fprintf(w, "  %s (%s, %s)\n", param.Name, typeLabel, freq)
		if param.Default != "" && !param.IsSecret {
			_, _ = fmt.Fprintf(w, "    Default: %s\n", param.Default)
		}
		if param.IsSecret {
			_, _ = fmt.Fprintln(w, "    Marked as secret (values not displayed)")
		}
		if len(param.SampleValues) > 0 && !param.IsSecret {
			_, _ = fmt.Fprintf(w, "    Sample values: %s\n", strings.Join(param.SampleValues, ", "))
		}
		if param.Frequency > 0 && param.Frequency < 0.999 {
			_, _ = fmt.Fprintf(w, "    Seen in %.0f%% of recent runs\n", param.Frequency*100)
		}
	}

	return nil
}

func parseParametersFromConfig(data []byte) ([]runParameterInfo, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))

	var (
		stack          []xml.StartElement
		params         []runParameterInfo
		current        *runParameterInfo
		paramDefsDepth = -1
		paramDepth     = -1
		inChoices      bool
	)

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch tok := token.(type) {
		case xml.StartElement:
			stack = append(stack, tok)
			depth := len(stack)
			if tok.Name.Local == "parameterDefinitions" {
				paramDefsDepth = depth
				continue
			}
			if paramDefsDepth > 0 && depth == paramDefsDepth+1 {
				typ, secret := parameterTypeFromElement(tok.Name.Local)
				current = &runParameterInfo{Type: typ, IsSecret: secret}
				paramDepth = depth
				continue
			}
			if current != nil && depth == paramDepth+1 && tok.Name.Local == "choices" {
				inChoices = true
			}
		case xml.CharData:
			if current == nil {
				continue
			}
			text := strings.TrimSpace(string(tok))
			if text == "" {
				continue
			}
			depth := len(stack)
			if depth == 0 {
				continue
			}
			element := stack[depth-1].Name.Local
			switch element {
			case "name":
				current.Name = text
				if filter.IsLikelySecret(text) {
					current.IsSecret = true
				}
			case "defaultValue":
				if !current.IsSecret {
					current.Default = text
				}
			case "description":
				// description omitted from output for now
			case "string":
				if inChoices && !current.IsSecret {
					current.SampleValues = appendSampleValue(current.SampleValues, text, 5)
				}
			}
		case xml.EndElement:
			depth := len(stack)
			if current != nil && depth == paramDepth && stack[depth-1].Name.Local == tok.Name.Local {
				if current.Name != "" {
					if current.Frequency == 0 {
						current.Frequency = 1
					}
					if current.IsSecret {
						current.Default = ""
						current.SampleValues = nil
					}
					params = append(params, *current)
				}
				current = nil
				paramDepth = -1
				inChoices = false
			}
			if depth > 0 {
				stack = stack[:depth-1]
			}
			if tok.Name.Local == "choices" {
				inChoices = false
			}
			if tok.Name.Local == "parameterDefinitions" {
				paramDefsDepth = -1
			}
		}
	}

	sort.Slice(params, func(i, j int) bool {
		return strings.ToLower(params[i].Name) < strings.ToLower(params[j].Name)
	})
	return params, nil
}

func parameterTypeFromElement(name string) (string, bool) {
	raw := name
	if idx := strings.LastIndex(raw, "."); idx >= 0 {
		raw = raw[idx+1:]
	}
	lower := strings.ToLower(raw)
	secret := false
	switch lower {
	case "stringparameterdefinition":
		return "string", false
	case "booleanparameterdefinition":
		return "boolean", false
	case "choiceparameterdefinition":
		return "choice", false
	case "textparameterdefinition":
		return "text", false
	case "fileparameterdefinition":
		return "file", false
	case "passwordparameterdefinition":
		return "password", true
	case "credentialsparameterdefinition":
		return "credentials", true
	default:
		cleaned := strings.TrimSuffix(raw, "ParameterDefinition")
		cleaned = strings.TrimSuffix(cleaned, "Definition")
		if cleaned == "" {
			cleaned = raw
		}
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
			secret = true
		}
		return strings.ToLower(cleaned), secret
	}
}

func appendSampleValue(values []string, value string, limit int) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	if len(values) >= limit {
		return values
	}
	return append(values, value)
}
