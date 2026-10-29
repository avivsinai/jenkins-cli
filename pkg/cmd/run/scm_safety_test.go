package run

import "testing"

func TestAdversarialMissingSourceCheckoutDoesNotBorrowLibraryRemote(t *testing.T) {
	actions := []map[string]any{
		{"_class": "jenkins.scm.api.SCMRevisionAction", "revision": map[string]any{"hash": "source-head"}},
		{"_class": "hudson.plugins.git.util.BuildData", "lastBuiltRevision": map[string]any{"SHA1": "library-head", "branch": []any{map[string]any{"name": "main"}}}, "remoteUrls": []any{"https://example.com/library.git"}},
	}
	info := extractSCMInfo("app/main", actions, changeSet{})
	if info.Repo != "" {
		t.Fatalf("selected library remote %q alongside source commit %q", info.Repo, info.Commit)
	}
}

func TestAdversarialHistoricalMatchDoesNotBeatExactCurrentSource(t *testing.T) {
	actions := []map[string]any{
		{"_class": "jenkins.scm.api.SCMRevisionAction", "revision": map[string]any{"hash": "source-head"}},
		{"_class": "hudson.plugins.git.util.BuildData", "lastBuiltRevision": map[string]any{"SHA1": "library-head", "branch": []any{map[string]any{"name": "library"}}}, "remoteUrls": []any{"https://example.com/library.git"}, "buildsByBranchName": map[string]any{"old": map[string]any{"marked": map[string]any{"SHA1": "source-head"}}}},
		{"_class": "hudson.plugins.git.util.BuildData", "lastBuiltRevision": map[string]any{"SHA1": "source-head", "branch": []any{map[string]any{"name": "main"}}}, "remoteUrls": []any{"https://example.com/source.git"}},
	}
	info := extractSCMInfo("app/main", actions, changeSet{})
	if info.Repo != "https://example.com/source.git" {
		t.Fatalf("selected stale matching history over current checkout: %+v", info)
	}
}

func TestAdversarialSCMRevisionActionOnlyUsesMatchingActionClass(t *testing.T) {
	actions := []map[string]any{
		{"_class": "example.OtherPluginAction", "revision": map[string]any{"hash": "unrelated-revision"}},
		{"_class": "jenkins.scm.api.SCMRevisionAction", "revision": map[string]any{"hash": "source-head"}},
	}
	info := extractSCMInfo("app/main", actions, changeSet{})
	if info.Commit != "source-head" {
		t.Fatalf("ignored authoritative SCMRevisionAction in favor of unrelated action: %+v", info)
	}
}
