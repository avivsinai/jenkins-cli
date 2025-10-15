package run

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/internal/filter"
	"github.com/your-org/jenkins-cli/internal/jenkins"
	jklog "github.com/your-org/jenkins-cli/internal/log"
	"github.com/your-org/jenkins-cli/pkg/cmd/shared"
	"github.com/your-org/jenkins-cli/pkg/cmdutil"
)

type runListResponse struct {
	Builds []runSummary `json:"builds"`
}

type runSummary struct {
	Number            int64            `json:"number"`
	Result            string           `json:"result"`
	Building          bool             `json:"building"`
	Timestamp         int64            `json:"timestamp"`
	Duration          int64            `json:"duration"`
	EstimatedDuration int64            `json:"estimatedDuration"`
	URL               string           `json:"url"`
	QueueID           int64            `json:"queueId"`
	Actions           []map[string]any `json:"actions"`
	ChangeSet         changeSet        `json:"changeSet"`
	Artifacts         []artifactItem   `json:"artifacts"`
}

type runDetail struct {
	Number            int64             `json:"number"`
	Result            string            `json:"result"`
	Building          bool              `json:"building"`
	Timestamp         int64             `json:"timestamp"`
	Duration          int64             `json:"duration"`
	EstimatedDuration int64             `json:"estimatedDuration"`
	URL               string            `json:"url"`
	Actions           []map[string]any  `json:"actions"`
	Parameters        []map[string]any  `json:"parameters"`
	Stages            []map[string]any  `json:"stages"`
	ChangeSet         changeSet         `json:"changeSet"`
	Artifacts         []artifactItem    `json:"artifacts"`
	QueueID           int64             `json:"queueId"`
	BuiltOn           string            `json:"builtOn"`
	Executor          *executorMetadata `json:"executor"`
	FullDisplayName   string            `json:"fullDisplayName"`
	Description       string            `json:"description"`
}

type artifactItem struct {
	FileName     string `json:"fileName"`
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size"`
}

type changeSet struct {
	Items []changeSetItem `json:"items"`
}

type changeSetItem struct {
	AuthorEmail string          `json:"authorEmail"`
	CommitID    string          `json:"commitId"`
	Msg         string          `json:"msg"`
	Author      changeSetAuthor `json:"author"`
}

type changeSetAuthor struct {
	FullName string `json:"fullName"`
}

type executorMetadata struct {
	Number int `json:"number"`
}

type queueItemStatus struct {
	ID           int64            `json:"id"`
	Why          string           `json:"why"`
	Cancelled    bool             `json:"cancelled"`
	InQueueSince int64            `json:"inQueueSince"`
	Executable   *queueExecutable `json:"executable"`
}

type queueExecutable struct {
	Number int64 `json:"number"`
}

type runListOptions struct {
	Limit        int
	Cursor       string
	Filters      []filter.Filter
	Since        *time.Time
	SelectFields []string
	GroupBy      string
	Aggregation  string
	WithMeta     bool
	AllowRegex   bool
}

type runInspection struct {
	Summary    runSummary
	Context    filter.Context
	Parameters map[string]string
	Causes     []runCauseInfo
	Artifacts  []artifactItem
}

type runCauseInfo struct {
	Type     string
	UserID   string
	UserName string
}

type runGroupAccumulator struct {
	Value          string
	Count          int
	Last           *runInspection
	First          *runInspection
	LastTimestamp  int64
	FirstTimestamp int64
}

const runListHeadroom = 50

type selectionRequirement struct {
	requiresParameters bool
	requiresArtifacts  bool
	requiresCauses     bool
}

var selectFieldRegistry = map[string]selectionRequirement{
	"number":              {},
	"status":              {},
	"result":              {},
	"starttime":           {},
	"durationms":          {},
	"branch":              {},
	"commit":              {},
	"url":                 {},
	"queueid":             {},
	"parameters":          {requiresParameters: true},
	"artifacts":           {requiresArtifacts: true},
	"causes":              {requiresCauses: true},
	"estimateddurationms": {},
}

type metadataCollector struct {
	enabled    bool
	parameters map[string]*parameterStat
	totalRuns  int
}

type parameterStat struct {
	Count   int
	Secret  bool
	Samples map[string]struct{}
}

func newMetadataCollector(enabled bool) *metadataCollector {
	return &metadataCollector{
		enabled:    enabled,
		parameters: make(map[string]*parameterStat),
	}
}

func (m *metadataCollector) observe(run *runInspection) {
	if !m.enabled || run == nil {
		return
	}

	m.totalRuns++
	for name, value := range run.Parameters {
		stat, ok := m.parameters[name]
		if !ok {
			stat = &parameterStat{
				Secret:  filter.IsLikelySecret(name),
				Samples: make(map[string]struct{}),
			}
			m.parameters[name] = stat
		}
		stat.Count++
		if stat.Secret {
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		if len(stat.Samples) < 5 {
			stat.Samples[value] = struct{}{}
		}
	}
}

func selectionRequiresParameters(fields []string) bool {
	for _, field := range fields {
		if spec, ok := selectFieldRegistry[field]; ok && spec.requiresParameters {
			return true
		}
	}
	return false
}

func selectionRequiresArtifacts(fields []string) bool {
	for _, field := range fields {
		if spec, ok := selectFieldRegistry[field]; ok && spec.requiresArtifacts {
			return true
		}
	}
	return false
}

func selectionRequiresCauses(fields []string) bool {
	for _, field := range fields {
		if spec, ok := selectFieldRegistry[field]; ok && spec.requiresCauses {
			return true
		}
	}
	return false
}

func parseSince(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("since value cannot be empty")
	}

	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, nil
	}

	dur, err := filter.ParseDuration(value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid since value %q: %w", value, err)
	}
	return time.Now().Add(-dur), nil
}

func parseSelectFields(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	parts := strings.Split(value, ",")
	seen := make(map[string]struct{})
	fields := make([]string, 0, len(parts))
	for _, part := range parts {
		field := strings.ToLower(strings.TrimSpace(part))
		if field == "" {
			continue
		}
		if _, ok := selectFieldRegistry[field]; !ok {
			return nil, fmt.Errorf("unsupported select field %q", part)
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields, nil
}

func normalizeAggregation(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "count", nil
	}
	switch trimmed {
	case "count", "first", "last":
		return trimmed, nil
	default:
		return "", fmt.Errorf("unsupported aggregation %q", value)
	}
}

func NewCmdRun(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Interact with job runs",
	}

	cmd.AddCommand(
		newRunStartCmd(f),
		newRunListCmd(f),
		newRunSearchCmd(f),
		newRunParamsCmd(f),
		newRunViewCmd(f),
		newRunCancelCmd(f),
		newRunRerunCmd(f),
	)

	return cmd
}

func newRunStartCmd(f *cmdutil.Factory) *cobra.Command {
	var params []string
	var follow bool
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "start <jobPath>",
		Short: "Trigger a job run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			paramMap := make(map[string]string, len(params))
			for _, p := range params {
				parts := strings.SplitN(p, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid parameter %q", p)
				}
				paramMap[strings.TrimSpace(parts[0])] = parts[1]
			}

			resp, err := triggerBuild(client, args[0], paramMap)
			if err != nil {
				return err
			}

			if !shared.WantsJSON(cmd) && !shared.WantsYAML(cmd) {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Triggered run for %s\n", args[0])
			}

			if !follow {
				if shared.WantsJSON(cmd) || shared.WantsYAML(cmd) {
					payload := runTriggerOutput{
						JobPath:       args[0],
						Message:       "run requested",
						QueueLocation: queueLocationFromResponse(resp),
					}
					return shared.PrintOutput(cmd, payload, func() error {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Triggered run for %s\n", args[0])
						return nil
					})
				}
				return nil
			}

			return followTriggeredRun(cmd, client, args[0], resp, interval)
		},
	}

	cmd.Flags().StringSliceVarP(&params, "param", "p", nil, "Build parameter key=value")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow the run progress until completion")
	cmd.Flags().DurationVar(&interval, "interval", 500*time.Millisecond, "Polling interval when following runs")
	return cmd
}

func newRunListCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		limit       int
		cursor      string
		filterArgs  []string
		sinceArg    string
		selectArg   string
		groupBy     string
		aggregation string
		withMeta    bool
		enableRegex bool
	)

	cmd := &cobra.Command{
		Use:   "ls <jobPath>",
		Short: "List recent runs",
		Example: `  # List recent runs for a job
	jk run ls Helm.Chart.Deploy

	# Filter by parameter values
	jk run ls Helm.Chart.Deploy --filter param.CHART_NAME~nova --filter result=SUCCESS --since 7d

	# Group by chart name and return the last run per chart
	jk run ls Helm.Chart.Deploy --group-by param.CHART_NAME --agg last --json

	# Select specific fields for agent consumption
	jk run ls Helm.Chart.Deploy --select parameters --limit 5 --json --with-meta`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			parsedFilters, err := filter.Parse(filterArgs)
			if err != nil {
				return err
			}

			var since *time.Time
			if strings.TrimSpace(sinceArg) != "" {
				sinceValue, err := parseSince(sinceArg)
				if err != nil {
					return err
				}
				since = &sinceValue
			}

			selectFields, err := parseSelectFields(selectArg)
			if err != nil {
				return err
			}

			agg, err := normalizeAggregation(aggregation)
			if err != nil {
				return err
			}
			if groupBy == "" && agg != "" && agg != "count" {
				return errors.New("aggregation flag requires --group-by")
			}

			opts := runListOptions{
				Limit:        limit,
				Cursor:       cursor,
				Filters:      parsedFilters,
				Since:        since,
				SelectFields: selectFields,
				GroupBy:      groupBy,
				Aggregation:  agg,
				WithMeta:     withMeta,
				AllowRegex:   enableRegex,
			}

			output, err := executeRunList(cmd.Context(), client, args[0], opts)
			if err != nil {
				return err
			}

			return shared.PrintOutput(cmd, output, func() error {
				return renderRunListHuman(cmd, output, opts)
			})
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Number of runs to list")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Cursor for pagination (use value from previous output)")
	cmd.Flags().StringSliceVar(&filterArgs, "filter", nil, "Filter runs (repeatable): key[op]value")
	cmd.Flags().StringVar(&sinceArg, "since", "", "Filter runs since timestamp or duration (RFC3339, 72h, 7d)")
	cmd.Flags().StringVar(&selectArg, "select", "", "Select additional fields (comma-separated)")
	cmd.Flags().StringVar(&groupBy, "group-by", "", "Group results by field (e.g., param.CHART_NAME)")
	cmd.Flags().StringVar(&aggregation, "agg", "count", "Aggregation function for grouped results: count, first, last")
	cmd.Flags().BoolVar(&withMeta, "with-meta", false, "Include metadata in JSON output")
	cmd.Flags().BoolVar(&enableRegex, "regex", false, "Enable regular expression matching for filters")

	return cmd
}

func executeRunList(ctx context.Context, client *jenkins.Client, jobPath string, opts runListOptions) (runListOutput, error) {
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if opts.Aggregation == "" {
		opts.Aggregation = "count"
	}

	requireArtifacts := filter.RequiresArtifacts(opts.Filters) || selectionRequiresArtifacts(opts.SelectFields) || strings.HasPrefix(opts.GroupBy, "artifact.")
	requireParams := filter.RequiresParameters(opts.Filters) || selectionRequiresParameters(opts.SelectFields) || strings.HasPrefix(opts.GroupBy, "param.") || opts.WithMeta
	requireCauses := filter.RequiresCauses(opts.Filters) || selectionRequiresCauses(opts.SelectFields) || strings.HasPrefix(opts.GroupBy, "cause.")

	fetchLimit := opts.Limit + runListHeadroom
	if fetchLimit < opts.Limit {
		fetchLimit = opts.Limit
	}

	path := fmt.Sprintf("/%s/api/json", jenkins.EncodeJobPath(jobPath))
	query := buildRunListTree(fetchLimit, requireArtifacts, requireParams, requireCauses)
	req := client.NewRequest().SetQueryParam("tree", query)
	if ctx != nil {
		req.SetContext(ctx)
	}

	var resp runListResponse
	if _, err := client.Do(req, http.MethodGet, path, &resp); err != nil {
		return runListOutput{}, err
	}

	out, _, err := processRunList(jobPath, opts, resp.Builds, requireArtifacts, requireParams, requireCauses)
	return out, err
}

func buildRunListTree(fetchLimit int, includeArtifacts, includeParameters, includeCauses bool) string {
	actionsFields := []string{
		"lastBuiltRevision[SHA1,branch[name]]",
		"buildsByBranchName[*]",
		"remoteUrls",
	}
	if includeParameters {
		actionsFields = append(actionsFields, "parameters[name,value]")
	}
	if includeCauses {
		actionsFields = append(actionsFields, "causes[shortDescription,userId,userName,_class]")
	}

	fields := []string{
		"number",
		"url",
		"result",
		"building",
		"timestamp",
		"duration",
		"estimatedDuration",
		"queueId",
		fmt.Sprintf("actions[%s]", strings.Join(actionsFields, ",")),
		"changeSet[items[authorEmail,author[fullName],commitId,msg]]",
	}
	if includeArtifacts {
		fields = append(fields, "artifacts[fileName,relativePath,size]")
	}

	return fmt.Sprintf("builds[%s]{,%d}", strings.Join(fields, ","), fetchLimit)
}

func processRunList(jobPath string, opts runListOptions, builds []runSummary, needArtifacts, needParams, needCauses bool) (runListOutput, []*runInspection, error) {
	normalized := normalizeJobPath(jobPath)
	sorted := make([]runSummary, len(builds))
	copy(sorted, builds)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Number > sorted[j].Number
	})

	var cutoff int64
	if strings.TrimSpace(opts.Cursor) != "" {
		payload, err := decodeRunCursor(opts.Cursor)
		if err != nil {
			return runListOutput{}, nil, err
		}
		if payload.JobPath != "" && payload.JobPath != normalized {
			return runListOutput{}, nil, fmt.Errorf("cursor job path %q does not match %q", payload.JobPath, normalized)
		}
		cutoff = payload.Number
	}

	var sinceMs int64
	if opts.Since != nil {
		sinceMs = opts.Since.UnixMilli()
	}

	evalOpts := []filter.Option{}
	if opts.AllowRegex {
		evalOpts = append(evalOpts, filter.WithRegexMatching())
	}

	collector := newMetadataCollector(opts.WithMeta)
	matched := make([]*runInspection, 0, minInt(opts.Limit, len(sorted)))
	groups := make(map[string]*runGroupAccumulator)
	moreMatches := false

	for _, summary := range sorted {
		if cutoff > 0 && summary.Number >= cutoff {
			continue
		}
		if sinceMs > 0 && summary.Timestamp < sinceMs {
			break
		}

		inspection := inspectRun(summary, needParams, needCauses, needArtifacts)
		if inspection == nil {
			continue
		}

		if len(opts.Filters) > 0 && !filter.Evaluate(inspection.Context, opts.Filters, evalOpts...) {
			continue
		}

		collector.observe(inspection)

		if opts.GroupBy != "" {
			groupValue := resolveGroupValue(inspection, opts.GroupBy)
			acc, ok := groups[groupValue]
			if !ok {
				acc = &runGroupAccumulator{Value: groupValue}
				groups[groupValue] = acc
			}
			acc.Count++
			if acc.Last == nil || summary.Timestamp > acc.LastTimestamp {
				acc.Last = inspection
				acc.LastTimestamp = summary.Timestamp
			}
			if acc.First == nil || summary.Timestamp < acc.FirstTimestamp {
				acc.First = inspection
				acc.FirstTimestamp = summary.Timestamp
			}
		}

		if len(matched) < opts.Limit {
			matched = append(matched, inspection)
		} else {
			moreMatches = true
		}
	}

	nextCursor := ""
	if moreMatches && len(matched) > 0 {
		nextCursor = encodeRunCursor(normalized, matched[len(matched)-1].Summary.Number)
	}

	return assembleRunListOutput(jobPath, opts, matched, groups, collector, nextCursor), matched, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func inspectRun(summary runSummary, needParams, needCauses, needArtifacts bool) *runInspection {
	ctx := filter.Context{
		"result":            strings.ToUpper(strings.TrimSpace(summary.Result)),
		"status":            statusFromFlags(summary.Building),
		"queue.id":          summary.QueueID,
		"building":          summary.Building,
		"started":           time.UnixMilli(summary.Timestamp),
		"duration":          time.Duration(summary.Duration) * time.Millisecond,
		"estimatedDuration": time.Duration(summary.EstimatedDuration) * time.Millisecond,
	}

	if ctx["result"] == "" {
		ctx["result"] = ctx["status"]
	}

	parameters := make(map[string]string)
	if needParams {
		parameters = extractParametersFromSummary(summary)
		for name, value := range parameters {
			ctx["param."+name] = value
		}
	}

	var causes []runCauseInfo
	if needCauses {
		causes = extractCausesFromSummary(summary)
		var causeUsers []string
		var causeTypes []string
		for _, cause := range causes {
			if cause.UserName != "" {
				causeUsers = append(causeUsers, cause.UserName)
			} else if cause.UserID != "" {
				causeUsers = append(causeUsers, cause.UserID)
			}
			if cause.Type != "" {
				causeTypes = append(causeTypes, cause.Type)
			}
		}
		if len(causeUsers) > 0 {
			ctx["cause.user"] = causeUsers
		}
		if len(causeTypes) > 0 {
			ctx["cause.type"] = causeTypes
		}
	}

	if needArtifacts {
		var names []string
		var paths []string
		for _, artifact := range summary.Artifacts {
			if artifact.FileName != "" {
				names = append(names, artifact.FileName)
			}
			if artifact.RelativePath != "" {
				paths = append(paths, artifact.RelativePath)
			}
		}
		if len(names) > 0 {
			ctx["artifact.name"] = names
		}
		if len(paths) > 0 {
			ctx["artifact.path"] = paths
		}
	}

	if scm := extractSCMInfo(summary.Actions, summary.ChangeSet); scm != nil {
		if scm.Branch != "" {
			ctx["branch"] = scm.Branch
		}
		if scm.Commit != "" {
			ctx["commit"] = scm.Commit
		}
	}

	return &runInspection{
		Summary:    summary,
		Context:    ctx,
		Parameters: parameters,
		Causes:     causes,
		Artifacts:  summary.Artifacts,
	}
}

func extractParametersFromSummary(summary runSummary) map[string]string {
	params := make(map[string]string)
	for _, action := range summary.Actions {
		raw, ok := action["parameters"].([]any)
		if !ok {
			continue
		}
		for _, entry := range raw {
			if paramMap, ok := entry.(map[string]any); ok {
				name, _ := paramMap["name"].(string)
				if strings.TrimSpace(name) == "" {
					continue
				}
				value := fmt.Sprint(paramMap["value"])
				params[strings.TrimSpace(name)] = value
			}
		}
	}
	return params
}

func extractCausesFromSummary(summary runSummary) []runCauseInfo {
	var causes []runCauseInfo
	for _, action := range summary.Actions {
		raw, ok := action["causes"].([]any)
		if !ok {
			continue
		}
		for _, entry := range raw {
			if causeMap, ok := entry.(map[string]any); ok {
				cause := runCauseInfo{}
				if className, ok := causeMap["_class"].(string); ok && className != "" {
					cause.Type = className
				} else if desc, ok := causeMap["shortDescription"].(string); ok {
					cause.Type = desc
				}
				if userID, ok := causeMap["userId"].(string); ok {
					cause.UserID = userID
				}
				if userName, ok := causeMap["userName"].(string); ok {
					cause.UserName = userName
				}
				causes = append(causes, cause)
			}
		}
	}
	return causes
}

func resolveGroupValue(run *runInspection, key string) string {
	if run == nil {
		return ""
	}
	if value, ok := run.Context[key]; ok {
		return contextValueToString(value)
	}
	if strings.HasPrefix(key, "param.") {
		name := strings.TrimPrefix(key, "param.")
		if val, ok := run.Parameters[name]; ok {
			return val
		}
	}
	return ""
}

func contextValueToString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []string:
		if len(v) > 0 {
			return v[0]
		}
	case []any:
		for _, entry := range v {
			if s := contextValueToString(entry); s != "" {
				return s
			}
		}
	case time.Time:
		return v.Format(time.RFC3339)
	case time.Duration:
		return v.String()
	case fmt.Stringer:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
	return ""
}

func availableSelectFields() []string {
	fields := make([]string, 0, len(selectFieldRegistry))
	for field := range selectFieldRegistry {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func (m *metadataCollector) metadata(jobPath string, opts runListOptions) *runListMetadata {
	meta := &runListMetadata{
		Filters: &filterMetadata{
			Available: filter.AllowedKeys(),
			Operators: filter.Operators(),
		},
		Fields:    availableSelectFields(),
		Selection: append([]string{}, opts.SelectFields...),
	}
	if opts.Since != nil {
		meta.Since = opts.Since.Format(time.RFC3339)
	}
	if opts.GroupBy != "" {
		meta.GroupBy = opts.GroupBy
		meta.Aggregation = opts.Aggregation
	}

	if !m.enabled || m.totalRuns == 0 {
		meta.Suggestions = buildMetadataSuggestions(jobPath, opts)
		return meta
	}

	params := make([]runParameterInfo, 0, len(m.parameters))
	for name, stat := range m.parameters {
		info := runParameterInfo{
			Name:     name,
			IsSecret: stat.Secret,
		}
		if m.totalRuns > 0 {
			info.Frequency = float64(stat.Count) / float64(m.totalRuns)
		}
		if !stat.Secret && len(stat.Samples) > 0 {
			samples := make([]string, 0, len(stat.Samples))
			for sample := range stat.Samples {
				samples = append(samples, sample)
			}
			sort.Strings(samples)
			if len(samples) > 5 {
				samples = samples[:5]
			}
			info.SampleValues = samples
		}
		params = append(params, info)
	}
	sort.Slice(params, func(i, j int) bool {
		return strings.ToLower(params[i].Name) < strings.ToLower(params[j].Name)
	})

	meta.Parameters = params
	meta.Suggestions = buildMetadataSuggestions(jobPath, opts)
	return meta
}

func buildMetadataSuggestions(jobPath string, opts runListOptions) []string {
	normalized := normalizeJobPath(jobPath)
	suggestions := make([]string, 0, 3)

	if len(opts.Filters) == 0 {
		suggestions = append(suggestions, fmt.Sprintf("jk run ls %s --filter result=SUCCESS --limit 5", normalized))
	}
	if opts.GroupBy == "" {
		suggestions = append(suggestions, fmt.Sprintf("jk run ls %s --group-by result --agg last", normalized))
	}
	if !selectionRequiresParameters(opts.SelectFields) {
		suggestions = append(suggestions, fmt.Sprintf("jk run ls %s --filter param.NAME~=value", normalized))
	}

	if len(suggestions) > 3 {
		return suggestions[:3]
	}
	return suggestions
}

func renderRunListHuman(cmd *cobra.Command, output runListOutput, opts runListOptions) error {
	w := cmd.OutOrStdout()

	if len(output.Items) == 0 && len(output.Groups) == 0 {
		_, _ = fmt.Fprintln(w, "No runs found")
		return nil
	}

	if opts.GroupBy != "" && len(output.Groups) > 0 {
		_, _ = fmt.Fprintf(w, "Grouped by %s (agg=%s)\n", opts.GroupBy, strings.ToLower(opts.Aggregation))
		for _, group := range output.Groups {
			label := group.Value
			if strings.TrimSpace(label) == "" {
				label = "(none)"
			}
			switch opts.Aggregation {
			case "count":
				if group.Last != nil {
					_, _ = fmt.Fprintf(w, "%s\t%d\t#%d\t%s\t%s\n", label, group.Count, group.Last.Number, strings.ToUpper(group.Last.Result), group.Last.StartTime)
				} else {
					_, _ = fmt.Fprintf(w, "%s\t%d\n", label, group.Count)
				}
			case "last":
				if group.Last != nil {
					_, _ = fmt.Fprintf(w, "%s\t#%d\t%s\t%s\n", label, group.Last.Number, strings.ToUpper(group.Last.Result), group.Last.StartTime)
				} else {
					_, _ = fmt.Fprintf(w, "%s\t(no data)\n", label)
				}
			case "first":
				if group.First != nil {
					_, _ = fmt.Fprintf(w, "%s\t#%d\t%s\t%s\n", label, group.First.Number, strings.ToUpper(group.First.Result), group.First.StartTime)
				} else {
					_, _ = fmt.Fprintf(w, "%s\t(no data)\n", label)
				}
			default:
				if group.Last != nil {
					_, _ = fmt.Fprintf(w, "%s\t#%d\t%s\t%s\n", label, group.Last.Number, strings.ToUpper(group.Last.Result), group.Last.StartTime)
				} else {
					_, _ = fmt.Fprintf(w, "%s\t(no data)\n", label)
				}
			}
		}
	} else {
		for _, item := range output.Items {
			_, _ = fmt.Fprintf(
				w,
				"#%d\t%s\t%s\t%s\n",
				item.Number,
				strings.ToUpper(item.Result),
				item.StartTime,
				shared.DurationString(item.DurationMs),
			)
		}
	}

	if output.NextCursor != "" {
		_, _ = fmt.Fprintf(w, "Next cursor: %s\n", output.NextCursor)
	}
	return nil
}

func newRunViewCmd(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <jobPath> <buildNumber>",
		Short: "View run details",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			num, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid build number: %w", err)
			}

			path := fmt.Sprintf("/%s/%d/api/json", jenkins.EncodeJobPath(args[0]), num)
			var detail runDetail
			_, err = client.Do(client.NewRequest(), http.MethodGet, path, &detail)
			if err != nil {
				return err
			}

			testReport, err := shared.FetchTestReport(client, args[0], num)
			if err != nil {
				jklog.L().Debug().Err(err).Msg("fetch test report failed")
			}

			output := buildRunDetailOutput(args[0], detail, testReport)

			return shared.PrintOutput(cmd, output, func() error {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Run #%d (%s)\n", output.Number, output.Status)
				if output.Result != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Result: %s\n", output.Result)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "URL: %s\n", output.URL)
				if output.StartTime != "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Started: %s\n", output.StartTime)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Duration: %s\n", shared.DurationString(output.DurationMs))
				if output.SCM != nil && (output.SCM.Branch != "" || output.SCM.Commit != "" || output.SCM.Repo != "") {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "SCM: branch=%s commit=%s repo=%s\n", output.SCM.Branch, output.SCM.Commit, output.SCM.Repo)
				}
				if len(output.Parameters) > 0 {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Parameters:")
					for _, p := range output.Parameters {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s=%v\n", p.Name, p.Value)
					}
				}
				if output.Tests != nil {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Tests: total=%d failed=%d skipped=%d\n", output.Tests.Total, output.Tests.Failed, output.Tests.Skipped)
				}
				return nil
			})
		},
	}

	return cmd
}

func newRunCancelCmd(f *cmdutil.Factory) *cobra.Command {
	var mode string

	cmd := &cobra.Command{
		Use:   "cancel <jobPath> <buildNumber>",
		Short: "Cancel a running job",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			num, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid build number: %w", err)
			}

			action, err := resolveCancelAction(mode)
			if err != nil {
				return err
			}

			path := fmt.Sprintf("/%s/%d/%s", jenkins.EncodeJobPath(args[0]), num, action)
			resp, err := client.Do(client.NewRequest(), http.MethodPost, path, nil)
			if err != nil {
				return err
			}
			if resp.StatusCode() >= 300 {
				return fmt.Errorf("cancel failed: %s", resp.Status())
			}

			if shared.WantsJSON(cmd) || shared.WantsYAML(cmd) {
				payload := map[string]any{
					"jobPath": args[0],
					"build":   num,
					"action":  action,
					"status":  "requested",
				}
				return shared.PrintOutput(cmd, payload, func() error {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cancellation requested for %s #%d (%s)\n", args[0], num, action)
					return nil
				})
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cancellation requested for %s #%d (%s)\n", args[0], num, action)
			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "stop", "Termination mode: stop, term, or kill")
	return cmd
}

func newRunRerunCmd(f *cmdutil.Factory) *cobra.Command {
	var follow bool
	var interval time.Duration

	cmd := &cobra.Command{
		Use:   "rerun <jobPath> <buildNumber>",
		Short: "Rerun a job using the previous parameters",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := shared.JenkinsClient(cmd, f)
			if err != nil {
				return err
			}

			num, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid build number: %w", err)
			}

			detail, err := fetchRunDetail(client, args[0], num)
			if err != nil {
				return err
			}

			params := collectRerunParameters(*detail)
			resp, err := triggerBuild(client, args[0], params)
			if err != nil {
				return err
			}

			if !shared.WantsJSON(cmd) && !shared.WantsYAML(cmd) {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Triggered rerun for %s #%d\n", args[0], num)
			}

			if !follow {
				if shared.WantsJSON(cmd) || shared.WantsYAML(cmd) {
					payload := runTriggerOutput{
						JobPath:       args[0],
						Message:       "rerun requested",
						QueueLocation: queueLocationFromResponse(resp),
					}
					return shared.PrintOutput(cmd, payload, func() error {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Triggered rerun for %s #%d\n", args[0], num)
						return nil
					})
				}
				return nil
			}

			return followTriggeredRun(cmd, client, args[0], resp, interval)
		},
	}

	cmd.Flags().BoolVar(&follow, "follow", false, "Follow the rerun progress until completion")
	cmd.Flags().DurationVar(&interval, "interval", 500*time.Millisecond, "Polling interval when following runs")
	return cmd
}

func triggerBuild(client *jenkins.Client, jobPath string, params map[string]string) (*resty.Response, error) {
	if client == nil {
		return nil, errors.New("jenkins client is required")
	}

	encoded := jenkins.EncodeJobPath(jobPath)
	if encoded == "" {
		return nil, errors.New("job path is required")
	}

	methodPath := fmt.Sprintf("/%s/build", encoded)
	req := client.NewRequest()
	if len(params) > 0 {
		req.SetFormData(params)
		methodPath = fmt.Sprintf("/%s/buildWithParameters", encoded)
	}

	resp, err := client.Do(req, http.MethodPost, methodPath, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() >= 300 {
		return nil, fmt.Errorf("trigger build failed: %s", resp.Status())
	}
	return resp, nil
}

func followTriggeredRun(cmd *cobra.Command, client *jenkins.Client, jobPath string, resp *resty.Response, interval time.Duration) error {
	queueLocation := queueLocationFromResponse(resp)
	buildNumber, err := waitForBuildNumber(client, queueLocation, 5*time.Minute)
	if err != nil {
		return err
	}

	streamLogs := !shared.WantsJSON(cmd) && !shared.WantsYAML(cmd)
	result, err := monitorRun(cmd, client, jobPath, buildNumber, interval, streamLogs)
	if err != nil {
		return err
	}

	if shared.WantsJSON(cmd) || shared.WantsYAML(cmd) {
		detail, err := fetchRunDetail(client, jobPath, buildNumber)
		if err != nil {
			return err
		}
		testReport, err := shared.FetchTestReport(client, jobPath, buildNumber)
		if err != nil {
			jklog.L().Debug().Err(err).Msg("fetch test report failed")
		}
		output := buildRunDetailOutput(jobPath, *detail, testReport)
		if err := shared.PrintOutput(cmd, output, func() error {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Run #%d completed with status %s\n", output.Number, output.Result)
			return nil
		}); err != nil {
			return err
		}
	}

	code := exitCodeForResult(result)
	if code == 0 {
		return nil
	}
	return shared.NewExitError(code, "")
}

func queueLocationFromResponse(resp *resty.Response) string {
	if resp == nil {
		return ""
	}
	location := resp.Header().Get("Location")
	if location == "" {
		location = resp.Header().Get("X-Queue-Item")
	}
	return location
}

func fetchRunDetail(client *jenkins.Client, jobPath string, buildNumber int64) (*runDetail, error) {
	var detail runDetail
	path := fmt.Sprintf("/%s/%d/api/json", jenkins.EncodeJobPath(jobPath), buildNumber)
	_, err := client.Do(client.NewRequest(), http.MethodGet, path, &detail)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

func resolveCancelAction(mode string) (string, error) {
	if mode == "" {
		return "stop", nil
	}
	switch strings.ToLower(mode) {
	case "stop":
		return "stop", nil
	case "term", "terminate":
		return "term", nil
	case "kill":
		return "kill", nil
	default:
		return "", fmt.Errorf("unsupported cancel mode %q", mode)
	}
}

func waitForBuildNumber(client *jenkins.Client, queueLocation string, timeout time.Duration) (int64, error) {
	if queueLocation == "" {
		return 0, errors.New("follow requested but queue location unavailable")
	}

	queueAPI := strings.TrimSpace(queueLocation)
	if !strings.Contains(queueAPI, "/api/json") {
		queueAPI = strings.TrimSuffix(queueAPI, "/") + "/api/json"
	}

	deadline := time.Now().Add(timeout)
	for {
		var status queueItemStatus
		_, err := client.Do(client.NewRequest(), http.MethodGet, queueAPI, &status)
		if err != nil {
			return 0, err
		}

		if status.Cancelled {
			if status.Why != "" {
				return 0, fmt.Errorf("queue item cancelled: %s", status.Why)
			}
			return 0, errors.New("queue item cancelled")
		}

		if status.Executable != nil && status.Executable.Number > 0 {
			return status.Executable.Number, nil
		}

		if time.Now().After(deadline) {
			return 0, errors.New("timed out waiting for run to start")
		}

		time.Sleep(1 * time.Second)
	}
}

func monitorRun(cmd *cobra.Command, client *jenkins.Client, jobPath string, buildNumber int64, interval time.Duration, streamLogs bool) (string, error) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		logCtx   context.Context
		cancel   context.CancelFunc
		logErrCh chan error
	)
	if streamLogs {
		logCtx, cancel = context.WithCancel(ctx)
		defer cancel()
		logErrCh = make(chan error, 1)
		go func() {
			err := shared.StreamProgressiveLog(logCtx, client, jobPath, int(buildNumber), interval, cmd.OutOrStdout())
			logErrCh <- err
		}()
	}

	statusPath := fmt.Sprintf("/%s/%d/api/json", jenkins.EncodeJobPath(jobPath), buildNumber)
	lastStatus := time.Time{}
	for {
		var detail runDetail
		_, err := client.Do(client.NewRequest(), http.MethodGet, statusPath, &detail)
		if err != nil {
			if cancel != nil {
				cancel()
			}
			if logErrCh != nil {
				<-logErrCh
			}
			return "", err
		}

		if !detail.Building {
			if cancel != nil {
				cancel()
			}
			if logErrCh != nil {
				if err := <-logErrCh; err != nil {
					return "", err
				}
			}
			result := strings.ToUpper(detail.Result)
			if result == "" {
				result = "SUCCESS"
			}
			if streamLogs {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nRun #%d completed with status %s\n", detail.Number, result)
			}
			return result, nil
		}

		if streamLogs && time.Since(lastStatus) >= 5*time.Second {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Run #%d still running...\n", detail.Number)
			lastStatus = time.Now()
		}
		time.Sleep(2 * time.Second)
	}
}

func exitCodeForResult(result string) int {
	switch strings.ToUpper(result) {
	case "SUCCESS":
		return 0
	case "UNSTABLE":
		return 10
	case "FAILURE":
		return 11
	case "ABORTED":
		return 12
	case "NOT_BUILT":
		return 13
	default:
		return 0
	}
}
