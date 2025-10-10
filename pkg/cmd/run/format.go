package run

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/your-org/jenkins-cli/pkg/cmd/shared"
)

type runListOutput struct {
	Items      []runListItem `json:"items"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type runListItem struct {
	ID         string `json:"id"`
	Number     int64  `json:"number"`
	Status     string `json:"status"`
	Result     string `json:"result,omitempty"`
	DurationMs int64  `json:"durationMs"`
	StartTime  string `json:"startTime,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Commit     string `json:"commit,omitempty"`
}

type runTriggerOutput struct {
	JobPath       string `json:"jobPath"`
	Message       string `json:"message"`
	QueueLocation string `json:"queueLocation,omitempty"`
}

type runDetailOutput struct {
	ID                  string          `json:"id"`
	Number              int64           `json:"number"`
	JobPath             string          `json:"jobPath"`
	URL                 string          `json:"url"`
	Status              string          `json:"status"`
	Result              string          `json:"result,omitempty"`
	StartTime           string          `json:"startTime,omitempty"`
	DurationMs          int64           `json:"durationMs"`
	EstimatedDurationMs int64           `json:"estimatedDurationMs,omitempty"`
	Parameters          []runParameter  `json:"parameters,omitempty"`
	SCM                 *runSCMInfo     `json:"scm,omitempty"`
	Causes              []runCause      `json:"causes,omitempty"`
	Stages              []runStage      `json:"stages,omitempty"`
	Artifacts           []artifactItem  `json:"artifacts,omitempty"`
	Tests               *runTestSummary `json:"tests,omitempty"`
	Queue               *runQueueInfo   `json:"queue,omitempty"`
	Node                *runNodeInfo    `json:"node,omitempty"`
	Description         string          `json:"description,omitempty"`
	DisplayName         string          `json:"displayName,omitempty"`
}

type runParameter struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type runSCMInfo struct {
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
	Repo   string `json:"repo,omitempty"`
	Author string `json:"author,omitempty"`
}

type runCause struct {
	Type        string `json:"type,omitempty"`
	UserID      string `json:"userId,omitempty"`
	UserName    string `json:"userName,omitempty"`
	Description string `json:"description,omitempty"`
}

type runStage struct {
	Name            string `json:"name"`
	Status          string `json:"status,omitempty"`
	Result          string `json:"result,omitempty"`
	DurationMs      int64  `json:"durationMs"`
	StartTime       string `json:"startTime,omitempty"`
	PauseDurationMs int64  `json:"pauseDurationMs,omitempty"`
}

type runTestSummary struct {
	Total   int `json:"total"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type runQueueInfo struct {
	ID       int64  `json:"id"`
	QueuedAt string `json:"queuedAt,omitempty"`
}

type runNodeInfo struct {
	DisplayName string `json:"displayName,omitempty"`
	Executor    int    `json:"executor,omitempty"`
}

type runCursorPayload struct {
	JobPath string `json:"jobPath,omitempty"`
	Number  int64  `json:"number"`
}

func buildRunListOutput(jobPath, cursor string, builds []runSummary, limit int) (runListOutput, error) {
	normalized := normalizeJobPath(jobPath)
	sorted := make([]runSummary, len(builds))
	copy(sorted, builds)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Number > sorted[j].Number
	})

	var cutoff int64
	if cursor != "" {
		payload, err := decodeRunCursor(cursor)
		if err != nil {
			return runListOutput{}, err
		}
		if payload.JobPath != "" && payload.JobPath != normalized {
			return runListOutput{}, fmt.Errorf("cursor job path %q does not match %q", payload.JobPath, normalized)
		}
		cutoff = payload.Number
	}

	filtered := make([]runSummary, 0, len(sorted))
	for _, build := range sorted {
		if cutoff > 0 && build.Number >= cutoff {
			continue
		}
		filtered = append(filtered, build)
	}

	count := limit
	if count > len(filtered) {
		count = len(filtered)
	}

	items := make([]runListItem, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, buildRunListItem(normalized, filtered[i]))
	}

	output := runListOutput{Items: items}
	if len(filtered) > count && count > 0 {
		output.NextCursor = encodeRunCursor(normalized, items[len(items)-1].Number)
	}
	return output, nil
}

func buildRunListItem(jobPath string, summary runSummary) runListItem {
	status := statusFromFlags(summary.Building)
	result := resultForList(summary.Result, summary.Building)
	scm := extractSCMInfo(summary.Actions, summary.ChangeSet)

	item := runListItem{
		ID:         fmt.Sprintf("%s/%d", jobPath, summary.Number),
		Number:     summary.Number,
		Status:     status,
		Result:     result,
		DurationMs: summary.Duration,
		StartTime:  formatTimestamp(summary.Timestamp),
	}
	if scm != nil {
		item.Branch = scm.Branch
		item.Commit = scm.Commit
	}
	return item
}

func buildRunDetailOutput(jobPath string, detail runDetail, testReport *shared.TestReport) runDetailOutput {
	normalized := normalizeJobPath(jobPath)
	status := statusFromFlags(detail.Building)
	result := resultForList(detail.Result, detail.Building)

	parameters := extractParameters(detail)
	scm := extractSCMInfo(detail.Actions, detail.ChangeSet)
	causes := extractCauses(detail.Actions)
	stages := extractStages(detail.Stages)
	tests := extractTestSummary(testReport)

	var queueInfo *runQueueInfo
	if detail.QueueID > 0 {
		queueInfo = &runQueueInfo{ID: detail.QueueID}
	}

	var nodeInfo *runNodeInfo
	if detail.BuiltOn != "" || (detail.Executor != nil && detail.Executor.Number > 0) {
		nodeInfo = &runNodeInfo{DisplayName: detail.BuiltOn}
		if detail.Executor != nil {
			nodeInfo.Executor = detail.Executor.Number
		}
	}

	output := runDetailOutput{
		ID:                  fmt.Sprintf("%s/%d", normalized, detail.Number),
		Number:              detail.Number,
		JobPath:             normalized,
		URL:                 detail.URL,
		Status:              status,
		Result:              result,
		StartTime:           formatTimestamp(detail.Timestamp),
		DurationMs:          detail.Duration,
		EstimatedDurationMs: detail.EstimatedDuration,
		Parameters:          parameters,
		SCM:                 scm,
		Causes:              causes,
		Stages:              stages,
		Artifacts:           detail.Artifacts,
		Tests:               tests,
		Queue:               queueInfo,
		Node:                nodeInfo,
		Description:         strings.TrimSpace(detail.Description),
		DisplayName:         strings.TrimSpace(detail.FullDisplayName),
	}

	return output
}

func collectRerunParameters(detail runDetail) map[string]string {
	params := extractParameters(detail)
	if len(params) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(params))
	for _, param := range params {
		out[param.Name] = fmt.Sprint(param.Value)
	}
	return out
}

func extractParameters(detail runDetail) []runParameter {
	var params []runParameter
	seen := make(map[string]struct{})

	appendParam := func(name string, value any) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		params = append(params, runParameter{Name: name, Value: normalizeParameterValue(value)})
		seen[name] = struct{}{}
	}

	for _, entry := range detail.Parameters {
		name, _ := entry["name"].(string)
		value := entry["value"]
		appendParam(name, value)
	}

	for _, action := range detail.Actions {
		rawParams, ok := action["parameters"].([]any)
		if !ok {
			continue
		}
		for _, raw := range rawParams {
			if paramMap, ok := raw.(map[string]any); ok {
				name, _ := paramMap["name"].(string)
				appendParam(name, paramMap["value"])
			}
		}
	}

	sort.Slice(params, func(i, j int) bool {
		return params[i].Name < params[j].Name
	})
	return params
}

func normalizeParameterValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return v
	case bool:
		return v
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case float32:
		f := float64(v)
		if f == math.Trunc(f) {
			return int64(f)
		}
		return f
	case float64:
		if v == math.Trunc(v) {
			return int64(v)
		}
		return v
	default:
		return fmt.Sprint(value)
	}
}

func extractSCMInfo(actions []map[string]any, change changeSet) *runSCMInfo {
	info := &runSCMInfo{}

	for _, action := range actions {
		if lastBuilt, ok := action["lastBuiltRevision"].(map[string]any); ok {
			if sha, ok := lastBuilt["SHA1"].(string); ok && info.Commit == "" {
				info.Commit = sha
			}
			if branches, ok := lastBuilt["branch"].([]any); ok {
				for _, branchAny := range branches {
					if bMap, ok := branchAny.(map[string]any); ok {
						if name, ok := bMap["name"].(string); ok && info.Branch == "" {
							info.Branch = name
						}
					}
				}
			}
		}

		if branchMap, ok := action["buildsByBranchName"].(map[string]any); ok {
			for name, raw := range branchMap {
				if info.Branch == "" {
					info.Branch = name
				}
				if entry, ok := raw.(map[string]any); ok {
					if rev, ok := entry["revision"].(string); ok && info.Commit == "" {
						info.Commit = rev
					}
				}
			}
		}

		if remotes, ok := action["remoteUrls"].([]any); ok {
			for _, remote := range remotes {
				if s, ok := remote.(string); ok && info.Repo == "" {
					info.Repo = s
					break
				}
			}
		}

		if remote, ok := action["remoteUrl"].(string); ok && info.Repo == "" {
			info.Repo = remote
		}
	}

	for _, item := range change.Items {
		if info.Commit == "" && item.CommitID != "" {
			info.Commit = item.CommitID
		}
		if info.Author == "" {
			switch {
			case item.AuthorEmail != "":
				info.Author = item.AuthorEmail
			case item.Author.FullName != "":
				info.Author = item.Author.FullName
			}
		}
		if info.Commit != "" && info.Author != "" {
			break
		}
	}

	if info.Branch == "" && info.Commit == "" && info.Repo == "" && info.Author == "" {
		return nil
	}

	return info
}

func extractCauses(actions []map[string]any) []runCause {
	var causes []runCause
	seen := make(map[string]struct{})

	for _, action := range actions {
		rawCauses, ok := action["causes"].([]any)
		if !ok {
			continue
		}
		for _, raw := range rawCauses {
			causeMap, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			description := getString(causeMap["shortDescription"])
			className := getString(causeMap["_class"])
			cause := runCause{
				Type:        classifyCause(className, description),
				UserID:      getString(causeMap["userId"]),
				UserName:    getString(causeMap["userName"]),
				Description: description,
			}
			key := fmt.Sprintf("%s|%s|%s|%s", cause.Type, cause.UserID, cause.UserName, cause.Description)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			causes = append(causes, cause)
		}
	}

	return causes
}

func extractStages(rawStages []map[string]any) []runStage {
	if len(rawStages) == 0 {
		return nil
	}

	stages := make([]runStage, 0, len(rawStages))
	for _, raw := range rawStages {
		name := getString(raw["name"])
		if name == "" {
			continue
		}

		stage := runStage{
			Name:            name,
			Status:          strings.ToLower(getString(raw["status"])),
			Result:          strings.ToUpper(getString(raw["result"])),
			DurationMs:      toInt64(raw["durationMillis"], raw["durationMs"], raw["duration"]),
			PauseDurationMs: toInt64(raw["pauseDurationMillis"], raw["pauseDurationMs"]),
			StartTime:       formatTimestampAny(raw["startTimeMillis"], raw["startTime"]),
		}

		if stage.Status == "" && stage.Result != "" {
			stage.Status = statusFromResult(stage.Result)
		}

		stages = append(stages, stage)
	}

	return stages
}

func extractTestSummary(report *shared.TestReport) *runTestSummary {
	if report == nil {
		return nil
	}
	return &runTestSummary{
		Total:   report.TotalCount,
		Failed:  report.FailCount,
		Skipped: report.SkipCount,
	}
}

func statusFromFlags(building bool) string {
	if building {
		return "running"
	}
	return "completed"
}

func resultForList(result string, building bool) string {
	if building {
		return ""
	}
	result = strings.ToUpper(strings.TrimSpace(result))
	if result == "" {
		return "SUCCESS"
	}
	return result
}

func statusFromResult(result string) string {
	switch strings.ToUpper(result) {
	case "SUCCESS", "UNSTABLE", "FAILURE", "ABORTED", "NOT_BUILT":
		return "completed"
	default:
		return ""
	}
}

func formatTimestamp(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.UnixMilli(ts).UTC().Format(time.RFC3339)
}

func formatTimestampAny(values ...any) string {
	for _, v := range values {
		switch typed := v.(type) {
		case int64:
			if typed > 0 {
				return formatTimestamp(typed)
			}
		case int:
			if typed > 0 {
				return formatTimestamp(int64(typed))
			}
		case float64:
			if typed > 0 {
				return formatTimestamp(int64(typed))
			}
		case string:
			if strings.TrimSpace(typed) != "" {
				return typed
			}
		}
	}
	return ""
}

func getString(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func classifyCause(className, description string) string {
	className = strings.ToLower(className)
	switch {
	case strings.Contains(className, "useridcause"):
		return "user"
	case strings.Contains(className, "scmtrigger"):
		return "scm"
	case strings.Contains(className, "timertrigger"):
		return "timer"
	case strings.Contains(className, "upstream"):
		return "upstream"
	}

	desc := strings.ToLower(description)
	switch {
	case strings.Contains(desc, "user"):
		return "user"
	case strings.Contains(desc, "scm"):
		return "scm"
	case strings.Contains(desc, "timer"):
		return "timer"
	case strings.Contains(desc, "upstream"):
		return "upstream"
	default:
		return "other"
	}
}

func toInt64(values ...any) int64 {
	for _, value := range values {
		switch v := value.(type) {
		case int64:
			if v != 0 {
				return v
			}
		case int:
			if v != 0 {
				return int64(v)
			}
		case float64:
			if v != 0 {
				return int64(v)
			}
		case float32:
			if v != 0 {
				return int64(v)
			}
		}
	}
	return 0
}

func normalizeJobPath(jobPath string) string {
	return strings.Trim(strings.TrimSpace(jobPath), "/")
}

func encodeRunCursor(jobPath string, number int64) string {
	payload := runCursorPayload{
		JobPath: jobPath,
		Number:  number,
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func decodeRunCursor(cursor string) (runCursorPayload, error) {
	var payload runCursorPayload
	if cursor == "" {
		return payload, nil
	}
	bytes, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return payload, fmt.Errorf("decode cursor: %w", err)
	}
	if err := json.Unmarshal(bytes, &payload); err != nil {
		return payload, fmt.Errorf("decode cursor: %w", err)
	}
	return payload, nil
}
