package run

import (
	"reflect"
	"testing"
)

func TestMatchJobGlob(t *testing.T) {
	cases := []struct {
		glob    string
		folder  string
		jobPath string
		expect  bool
		desc    string
	}{
		{"", "", "team/job", true, "empty glob matches all"},
		{"team/*", "", "team/job", true, "full path match"},
		{"deploy-*", "team", "team/deploy-prod", true, "base name match with folder"},
		{"deploy-*", "team", "team/tools/sync", false, "no match"},
		{"*/deploy-*", "team", "team/services/deploy-api", true, "nested pattern match"},

		// Parent path component matching (new functionality)
		{"*ada*", "", "Tools/ada/master", true, "parent component ada matches *ada*"},
		{"*ada*", "", "Tools/ada/PR-22", true, "parent component ada matches *ada* in PR"},
		{"ada", "", "Tools/ada/master", true, "parent component ada matches exact"},
		{"*video*", "", "Media/video-service/develop", true, "parent video-service matches *video*"},
		{"*service*", "", "Team/api-service/feature/test", true, "parent api-service matches *service*"},

		// Should NOT match when component doesn't exist
		{"*ada*", "", "Tools/other/master", false, "no ada component in path"},
		{"*xyz*", "", "Tools/ada/master", false, "xyz not in any component"},

		// Globstar support
		{"**/ada", "", "Tools/ada", true, "globstar matches Tools/ada"},
		{"**/ada/*", "", "Tools/ada/master", true, "globstar matches nested"},

		// Relative path matching
		{"*ada*", "Tools", "Tools/ada/master", true, "relative match with folder"},
		{"ada/*", "Tools", "Tools/ada/master", true, "relative nested match"},
	}

	for _, tc := range cases {
		got := matchJobGlob(tc.glob, tc.folder, tc.jobPath)
		if got != tc.expect {
			t.Errorf("%s: matchJobGlob(%q, %q, %q) = %v, want %v",
				tc.desc, tc.glob, tc.folder, tc.jobPath, got, tc.expect)
		}
	}
}

func TestIsMultibranchClass(t *testing.T) {
	cases := []struct {
		className string
		expect    bool
		desc      string
	}{
		{"org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject", true, "workflow multibranch"},
		{"jenkins.branch.MultiBranchProject", true, "generic multibranch"},
		{"com.cloudbees.hudson.plugins.folder.Folder", false, "regular folder"},
		{"hudson.model.FreeStyleProject", false, "freestyle project"},
		{"org.jenkinsci.plugins.workflow.job.WorkflowJob", false, "pipeline job"},
		{"MULTIBRANCH", true, "case insensitive"},
	}

	for _, tc := range cases {
		got := isMultibranchClass(tc.className)
		if got != tc.expect {
			t.Errorf("%s: isMultibranchClass(%q) = %v, want %v",
				tc.desc, tc.className, got, tc.expect)
		}
	}
}

func TestIsFolderClass(t *testing.T) {
	cases := []struct {
		className string
		expect    bool
		desc      string
	}{
		{"com.cloudbees.hudson.plugins.folder.Folder", true, "regular folder"},
		{"com.cloudbees.hudson.plugins.folder.AbstractFolder", true, "abstract folder"},
		{"org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject", false, "multibranch excluded"},
		{"jenkins.branch.MultiBranchProject", false, "multibranch project excluded"},
		{"hudson.model.FreeStyleProject", false, "freestyle project"},
		{"org.jenkinsci.plugins.workflow.job.WorkflowJob", false, "workflow job"},
		{"FOLDER", true, "case insensitive folder"},
	}

	for _, tc := range cases {
		got := isFolderClass(tc.className)
		if got != tc.expect {
			t.Errorf("%s: isFolderClass(%q) = %v, want %v",
				tc.desc, tc.className, got, tc.expect)
		}
	}
}

func TestSortSearchItems(t *testing.T) {
	items := []runSearchItem{
		{JobPath: "b/job", Number: 1, StartTime: "2025-10-14T10:00:00Z"},
		{JobPath: "a/job", Number: 5, StartTime: "2025-10-15T08:00:00Z"},
		{JobPath: "a/job", Number: 2, StartTime: "2025-10-15T08:00:00Z"},
	}

	sortSearchItems(items)

	if items[0].JobPath != "a/job" || items[0].Number != 5 {
		t.Fatalf("expected newest item first, got %#v", items[0])
	}
	if items[1].JobPath != "a/job" || items[1].Number != 2 {
		t.Fatalf("expected secondary item from same job next, got %#v", items[1])
	}
	if items[2].JobPath != "b/job" {
		t.Fatalf("expected remaining item last, got %#v", items[2])
	}
}

func TestPerformFuzzySearchRanksByScore(t *testing.T) {
	allJobs := []string{
		"Team/ada-runner",
		"Tools/ada/feature/test",
		"Tools/ada/master",
		"Legacy/service",
	}

	got := performFuzzySearch("ada", allJobs, 3)
	want := []string{
		"Tools/ada/master",
		"Tools/ada/feature/test",
		"Team/ada-runner",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected fuzzy ranking order: got %v, want %v", got, want)
	}

	gotLimited := performFuzzySearch("ada", allJobs, 2)
	if len(gotLimited) != 2 {
		t.Fatalf("expected 2 results with maxResults limit, got %d", len(gotLimited))
	}
	if gotLimited[0] != "Tools/ada/master" {
		t.Fatalf("expected highest score to remain first with limit; got %q", gotLimited[0])
	}
}

func TestPerformFuzzySearchEmptyQuery(t *testing.T) {
	allJobs := []string{"Team/ada-runner", "Tools/ada/master"}
	if got := performFuzzySearch("", allJobs, 5); got != nil {
		t.Fatalf("expected nil results for empty query, got %v", got)
	}
}
