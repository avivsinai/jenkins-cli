package run

import (
	"testing"
)

func TestMatchJobGlob(t *testing.T) {
	cases := []struct {
		glob    string
		folder  string
		jobPath string
		expect  bool
	}{
		{"", "", "team/job", true},
		{"team/*", "", "team/job", true},
		{"deploy-*", "team", "team/deploy-prod", true},
		{"deploy-*", "team", "team/tools/sync", false},
		{"*/deploy-*", "team", "team/services/deploy-api", true},
	}

	for _, tc := range cases {
		if got := matchJobGlob(tc.glob, tc.folder, tc.jobPath); got != tc.expect {
			t.Fatalf("matchJobGlob(%q,%q,%q) = %v, want %v", tc.glob, tc.folder, tc.jobPath, got, tc.expect)
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
