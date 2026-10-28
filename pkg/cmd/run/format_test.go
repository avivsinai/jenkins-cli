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

func TestExtractSCMInfoPrefersSCMRevisionActionOverLibraryBuildData(t *testing.T) {
	actions := []map[string]any{
		{"_class": "jenkins.scm.api.SCMRevisionAction", "revision": map[string]any{"hash": "2222222222222222222222222222222222222222"}},
		{
			"_class":            "hudson.plugins.git.util.BuildData",
			"lastBuiltRevision": map[string]any{"SHA1": "1111111111111111111111111111111111111111", "branch": []any{map[string]any{"name": "library-branch"}}},
			"remoteUrls":        []any{"https://example.com/org/pipeline-library.git"},
		},
		{
			"_class":            "hudson.plugins.git.util.BuildData",
			"lastBuiltRevision": map[string]any{"SHA1": "2222222222222222222222222222222222222222", "branch": []any{map[string]any{"name": "main"}}},
			"remoteUrls":        []any{"https://example.com/org/app.git"},
		},
	}

	info := extractSCMInfo("", actions, changeSet{})
	if info == nil {
		t.Fatal("expected SCM info")
	}
	if info.Commit != "2222222222222222222222222222222222222222" {
		t.Fatalf("commit = %q, want the job's source revision", info.Commit)
	}
	if info.Branch != "main" {
		t.Fatalf("branch = %q, want main", info.Branch)
	}
	if info.Repo != "https://example.com/org/app.git" {
		t.Fatalf("repo = %q, want the source remote", info.Repo)
	}
}

func TestExtractSCMInfoWithoutSCMRevisionActionKeepsFirstBuildData(t *testing.T) {
	actions := []map[string]any{
		{
			"lastBuiltRevision": map[string]any{"SHA1": "3333333333333333333333333333333333333333", "branch": []any{map[string]any{"name": "origin/main"}}},
			"remoteUrls":        []any{"https://example.com/org/app.git"},
		},
	}

	info := extractSCMInfo("", actions, changeSet{})
	if info == nil || info.Commit != "3333333333333333333333333333333333333333" || info.Branch != "origin/main" {
		t.Fatalf("unexpected SCM info: %+v", info)
	}
}

func TestExtractSCMInfoUsesPullRequestRevision(t *testing.T) {
	actions := []map[string]any{
		{"_class": "jenkins.scm.api.SCMRevisionAction", "revision": map[string]any{"pullHash": "4444444444444444444444444444444444444444"}},
		{
			"_class":            "hudson.plugins.git.util.BuildData",
			"lastBuiltRevision": map[string]any{"SHA1": "1111111111111111111111111111111111111111", "branch": []any{map[string]any{"name": "main"}}},
			"remoteUrls":        []any{"https://example.com/org/pipeline-library.git"},
		},
	}

	info := extractSCMInfo("app/PR-4", actions, changeSet{})
	if info == nil || info.Commit != "4444444444444444444444444444444444444444" {
		t.Fatalf("commit = %+v, want the pull request head", info)
	}
	if info.Branch != "" || info.Repo != "" {
		t.Fatalf("branch/repo = %q/%q, want none from the library checkout", info.Branch, info.Repo)
	}
}

// mergeStrategySourceBuildData is the job's BuildData for a pull request built
// with the merge strategy: lastBuiltRevision is the local merge commit, and the
// pull request head is the marked revision in buildsByBranchName.
func mergeStrategySourceBuildData() map[string]any {
	return map[string]any{
		"_class":            "hudson.plugins.git.util.BuildData",
		"lastBuiltRevision": map[string]any{"SHA1": "5555555555555555555555555555555555555555", "branch": []any{map[string]any{"name": "PR-12"}}},
		"buildsByBranchName": map[string]any{
			"PR-12": map[string]any{
				"marked":   map[string]any{"SHA1": "4444444444444444444444444444444444444444"},
				"revision": map[string]any{"SHA1": "5555555555555555555555555555555555555555"},
			},
		},
		"remoteUrls": []any{"https://example.com/org/app.git"},
	}
}

func TestExtractSCMInfoMergeStrategyPullRequest(t *testing.T) {
	actions := []map[string]any{
		{"_class": "jenkins.scm.api.SCMRevisionAction", "revision": map[string]any{"pullHash": "4444444444444444444444444444444444444444"}},
		mergeStrategySourceBuildData(),
	}

	info := extractSCMInfo("app/PR-12", actions, changeSet{})
	require.NotNil(t, info)
	require.Equal(t, "4444444444444444444444444444444444444444", info.Commit)
	require.Equal(t, "PR-12", info.Branch)
	require.Equal(t, "https://example.com/org/app.git", info.Repo)
}

func TestExtractSCMInfoMergeStrategyPullRequestWithLibraryCheckout(t *testing.T) {
	actions := []map[string]any{
		{"_class": "jenkins.scm.api.SCMRevisionAction", "revision": map[string]any{"pullHash": "4444444444444444444444444444444444444444"}},
		{
			"_class":            "hudson.plugins.git.util.BuildData",
			"lastBuiltRevision": map[string]any{"SHA1": "1111111111111111111111111111111111111111", "branch": []any{map[string]any{"name": "library-branch"}}},
			"buildsByBranchName": map[string]any{
				"library-branch": map[string]any{
					"marked":   map[string]any{"SHA1": "1111111111111111111111111111111111111111"},
					"revision": map[string]any{"SHA1": "1111111111111111111111111111111111111111"},
				},
			},
			"remoteUrls": []any{"https://example.com/org/pipeline-library.git"},
		},
		mergeStrategySourceBuildData(),
	}

	info := extractSCMInfo("app/PR-12", actions, changeSet{})
	require.NotNil(t, info)
	require.Equal(t, "4444444444444444444444444444444444444444", info.Commit)
	require.Equal(t, "PR-12", info.Branch)
	require.Equal(t, "https://example.com/org/app.git", info.Repo)
}

func TestExtractSCMInfoFallsBackToJobBranchBuildData(t *testing.T) {
	// Without buildsByBranchName, only the branch name links the merge
	// commit's BuildData to the job.
	source := mergeStrategySourceBuildData()
	delete(source, "buildsByBranchName")
	source["lastBuiltRevision"] = map[string]any{"SHA1": "5555555555555555555555555555555555555555", "branch": []any{map[string]any{"name": "feature/login"}}}
	actions := []map[string]any{
		{"_class": "jenkins.scm.api.SCMRevisionAction", "revision": map[string]any{"pullHash": "4444444444444444444444444444444444444444"}},
		{
			"_class":            "hudson.plugins.git.util.BuildData",
			"lastBuiltRevision": map[string]any{"SHA1": "1111111111111111111111111111111111111111", "branch": []any{map[string]any{"name": "library-branch"}}},
			"remoteUrls":        []any{"https://example.com/org/pipeline-library.git"},
		},
		source,
	}

	info := extractSCMInfo("app/feature%2Flogin", actions, changeSet{})
	require.NotNil(t, info)
	require.Equal(t, "4444444444444444444444444444444444444444", info.Commit)
	require.Equal(t, "feature/login", info.Branch)
	require.Equal(t, "https://example.com/org/app.git", info.Repo)
}

func TestBuildRunListTreeRequestsBuildsByBranchNameRevisions(t *testing.T) {
	tree := buildRunListTree(10, false, false, false)
	require.Contains(t, tree, "buildsByBranchName[marked[SHA1],revision[SHA1]]")
	require.Contains(t, tree, "revision[hash,pullHash]")
}
