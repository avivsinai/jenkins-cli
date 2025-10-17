package run

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/filter"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
	"github.com/avivsinai/jenkins-cli/pkg/cmd/shared"
	"github.com/avivsinai/jenkins-cli/pkg/cmdutil"
)

const (
	defaultSearchLimit   = 10
	defaultSearchMaxScan = 500
	maxJobDiscoveryDepth = 5
)

type runSearchOptions struct {
	Filters      []filter.Filter
	RawFilters   []string
	Since        *time.Time
	Limit        int
	MaxScan      int
	SelectFields []string
	AllowRegex   bool
	Folder       string
	JobGlob      string
}

type jobListEntry struct {
	Name  string `json:"name"`
	Class string `json:"_class"`
}

type jobListPayload struct {
	Jobs []jobListEntry `json:"jobs"`
}

func newRunSearchCmd(f *cmdutil.Factory) *cobra.Command {
	var (
		folder      string
		jobGlob     string
		filterArgs  []string
		sinceArg    string
		limit       int
		maxScan     int
		selectArg   string
		enableRegex bool
	)

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search runs across jobs",
		Example: `  # Find the latest production deploy across a folder
  jk run search --folder releases --filter param.CHART_NAME~nova-video-prod --limit 5 --json

  # Search jobs matching a glob and select additional fields
  jk run search --folder team --job-glob "*/deploy-*" --filter result=FAILURE --select parameters --since 30d`,
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

			if trimmed := strings.TrimSpace(jobGlob); trimmed != "" {
				if _, err := doublestar.Match(trimmed, "test/job"); err != nil {
					return fmt.Errorf("invalid job glob %q: %w", jobGlob, err)
				}
			}

			if limit <= 0 {
				limit = defaultSearchLimit
			}
			if maxScan <= 0 {
				maxScan = defaultSearchMaxScan
			}

			normalizedFolder := normalizeJobPath(folder)
			jobPaths, err := discoverJobs(cmd.Context(), client, normalizedFolder, jobGlob, maxJobDiscoveryDepth)
			if err != nil {
				return err
			}

			if len(jobPaths) == 0 {
				empty := runSearchOutput{SchemaVersion: "1.0", Items: []runSearchItem{}, Metadata: &runSearchMetadata{Folder: normalizedFolder, JobGlob: jobGlob, Filters: append([]string{}, filterArgs...), Since: sinceString(since), JobsScanned: 0, MaxScan: maxScan, Selection: append([]string{}, selectFields...)}}
				return shared.PrintOutput(cmd, empty, func() error {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No matching runs found")
					return nil
				})
			}

			opts := runSearchOptions{
				Filters:      parsedFilters,
				RawFilters:   append([]string{}, filterArgs...),
				Since:        since,
				Limit:        limit,
				MaxScan:      maxScan,
				SelectFields: selectFields,
				AllowRegex:   enableRegex,
				Folder:       normalizedFolder,
				JobGlob:      jobGlob,
			}

			output, err := executeRunSearch(cmd.Context(), client, jobPaths, opts)
			if err != nil {
				return err
			}

			return shared.PrintOutput(cmd, output, func() error {
				return renderRunSearchHuman(cmd, output)
			})
		},
	}

	cmd.Flags().StringVar(&folder, "folder", "", "Folder path to search in")
	cmd.Flags().StringVar(&jobGlob, "job-glob", "", "Job glob pattern (e.g., \"*/deploy-*\")")
	cmd.Flags().StringSliceVar(&filterArgs, "filter", nil, "Filter runs (repeatable): key[op]value")
	cmd.Flags().StringVar(&sinceArg, "since", "", "Only search runs since timestamp or duration (RFC3339, 72h, 7d)")
	cmd.Flags().IntVar(&limit, "limit", defaultSearchLimit, "Max results to return")
	cmd.Flags().IntVar(&maxScan, "max-scan", defaultSearchMaxScan, "Max builds to scan per job")
	cmd.Flags().StringVar(&selectArg, "select", "", "Select additional fields (comma-separated)")
	cmd.Flags().BoolVar(&enableRegex, "regex", false, "Enable regular expression matching for filters")

	return cmd
}

func executeRunSearch(ctx context.Context, client *jenkins.Client, jobPaths []string, opts runSearchOptions) (runSearchOutput, error) {
	items := make([]runSearchItem, 0, opts.Limit)
	for _, jobPath := range jobPaths {
		if ctx != nil && ctx.Err() != nil {
			return runSearchOutput{}, ctx.Err()
		}

		listOpts := runListOptions{
			Limit:        opts.MaxScan,
			Filters:      opts.Filters,
			Since:        opts.Since,
			SelectFields: opts.SelectFields,
			AllowRegex:   opts.AllowRegex,
		}

		out, err := executeRunList(ctx, client, jobPath, listOpts)
		if err != nil {
			return runSearchOutput{}, err
		}

		for _, item := range out.Items {
			items = append(items, buildRunSearchItem(jobPath, item))
		}
	}

	sortSearchItems(items)
	if opts.Limit > 0 && len(items) > opts.Limit {
		items = items[:opts.Limit]
	}

	metadata := &runSearchMetadata{
		Folder:      opts.Folder,
		JobGlob:     opts.JobGlob,
		Filters:     append([]string{}, opts.RawFilters...),
		Since:       sinceString(opts.Since),
		JobsScanned: len(jobPaths),
		MaxScan:     opts.MaxScan,
		Selection:   append([]string{}, opts.SelectFields...),
	}

	return runSearchOutput{SchemaVersion: "1.0", Items: items, Metadata: metadata}, nil
}

func discoverJobs(ctx context.Context, client *jenkins.Client, folderPath, jobGlob string, maxDepth int) ([]string, error) {
	visited := make(map[string]struct{})
	results := make([]string, 0)

	var walk func(path string, depth int) error

	walk = func(current string, depth int) error {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if depth > maxDepth {
			return nil
		}

		encoded := "/api/json"
		if current != "" {
			encoded = fmt.Sprintf("/%s/api/json", jenkins.EncodeJobPath(current))
		}

		var payload jobListPayload
		resp, err := client.Do(client.NewRequest().SetContext(ctx).SetQueryParam("tree", "jobs[name,_class]"), http.MethodGet, encoded, &payload)
		if err != nil {
			return err
		}

		status := resp.StatusCode()
		if status == http.StatusNotFound && current != "" {
			if matchJobGlob(jobGlob, folderPath, current) {
				if _, ok := visited[current]; !ok {
					visited[current] = struct{}{}
					results = append(results, current)
				}
			}
			return nil
		}
		if status >= 400 {
			return fmt.Errorf("list jobs for %s: %s", current, resp.Status())
		}

		for _, job := range payload.Jobs {
			childPath := joinJobPath(current, job.Name)

			// Check if this job matches the glob BEFORE deciding how to handle it
			matches := matchJobGlob(jobGlob, folderPath, childPath)

			// Handle multibranch projects specially
			if isMultibranchClass(job.Class) {
				if matches {
					// Matched multibranch: add ALL its branches (don't filter children)
					if err := walkAndAddAllBranches(ctx, client, childPath, &results, visited); err != nil {
						return err
					}
				} else {
					// Multibranch didn't match: recurse normally (children might match)
					if err := walk(childPath, depth+1); err != nil {
						return err
					}
				}
				continue
			}

			// Handle regular folders: recurse into them
			if isFolderClass(job.Class) {
				if err := walk(childPath, depth+1); err != nil {
					return err
				}
				continue
			}

			// Regular job: add if it matches
			if matches {
				if _, ok := visited[childPath]; !ok {
					visited[childPath] = struct{}{}
					results = append(results, childPath)
				}
			}
		}

		return nil
	}

	if err := walk(folderPath, 0); err != nil {
		return nil, err
	}

	sort.Strings(results)
	return results, nil
}

func joinJobPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return fmt.Sprintf("%s/%s", parent, child)
}

func walkAndAddAllBranches(ctx context.Context, client *jenkins.Client, multibranchPath string, results *[]string, visited map[string]struct{}) error {
	// Fetch branches of matched multibranch project
	encoded := fmt.Sprintf("/%s/api/json", jenkins.EncodeJobPath(multibranchPath))
	tree := "jobs[name,_class]"

	var payload jobListPayload
	resp, err := client.Do(
		client.NewRequest().
			SetContext(ctx).
			SetQueryParam("tree", tree),
		http.MethodGet,
		encoded,
		&payload,
	)
	if err != nil {
		return err
	}

	// Propagate HTTP errors (permission denied, server errors, etc.)
	if resp.StatusCode() >= 400 {
		return fmt.Errorf("list branches for %s: %s", multibranchPath, resp.Status())
	}

	// Add all branches without glob filtering (user matched parent project)
	for _, branch := range payload.Jobs {
		branchPath := joinJobPath(multibranchPath, branch.Name)
		// Only add actual branches (not nested folders)
		if !isFolderClass(branch.Class) && !isMultibranchClass(branch.Class) {
			if _, ok := visited[branchPath]; !ok {
				visited[branchPath] = struct{}{}
				*results = append(*results, branchPath)
			}
		}
	}

	return nil
}

func isMultibranchClass(className string) bool {
	return strings.Contains(strings.ToLower(className), "multibranch")
}

func isFolderClass(className string) bool {
	className = strings.ToLower(className)
	// Check for folder-like classes, but exclude multibranch (handled separately)
	return strings.Contains(className, "folder") && !strings.Contains(className, "multibranch")
}

func matchJobGlob(glob, folder, jobPath string) bool {
	if glob == "" {
		return true
	}

	// Strategy 1: Full path match (supports ** globstar)
	if ok, err := doublestar.Match(glob, jobPath); err == nil && ok {
		return true
	}

	// Strategy 2: Base name match
	base := path.Base(jobPath)
	if ok, err := doublestar.Match(glob, base); err == nil && ok {
		return true
	}

	// Strategy 3: Parent path component matching
	// If jobPath is "Tools/ada/master" and glob is "*ada*"
	// Check each path component: ["Tools", "ada", "master"]
	// This allows "*ada*" to match "Tools/ada/master" because "ada" component matches
	parts := strings.Split(jobPath, "/")
	for i := 0; i < len(parts)-1; i++ { // -1 to exclude base (already checked above)
		if ok, err := doublestar.Match(glob, parts[i]); err == nil && ok {
			return true
		}
	}

	// Strategy 4: Relative path match (when folder specified)
	if folder != "" && strings.HasPrefix(jobPath, folder+"/") {
		rel := strings.TrimPrefix(jobPath, folder+"/")
		if ok, err := doublestar.Match(glob, rel); err == nil && ok {
			return true
		}
	}

	return false
}

func sortSearchItems(items []runSearchItem) {
	sort.Slice(items, func(i, j int) bool {
		ti := parseSearchTime(items[i].StartTime)
		tj := parseSearchTime(items[j].StartTime)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		if items[i].JobPath == items[j].JobPath {
			return items[i].Number > items[j].Number
		}
		return items[i].JobPath < items[j].JobPath
	})
}

func parseSearchTime(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts
	}
	return time.Time{}
}

func sinceString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func renderRunSearchHuman(cmd *cobra.Command, output runSearchOutput) error {
	w := cmd.OutOrStdout()
	if len(output.Items) == 0 {
		_, _ = fmt.Fprintln(w, "No matching runs found")
		return nil
	}
	for _, item := range output.Items {
		result := strings.ToUpper(strings.TrimSpace(item.Result))
		if result == "" {
			result = strings.ToUpper(strings.TrimSpace(item.Status))
		}
		_, _ = fmt.Fprintf(w, "%s\t#%d\t%s\t%s\t%s\n", item.JobPath, item.Number, result, item.StartTime, shared.DurationString(item.DurationMs))
	}
	return nil
}
