package run

import "testing"

func TestExtractJobPathFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "simple job",
			url:      "http://jenkins/job/MyJob/",
			expected: "MyJob",
		},
		{
			name:     "nested job",
			url:      "http://jenkins/job/Folder/job/SubFolder/job/JobName/",
			expected: "Folder/SubFolder/JobName",
		},
		{
			name:     "URL encoded spaces",
			url:      "http://jenkins/job/My%20Folder/job/My%20Job/",
			expected: "My Folder/My Job",
		},
		{
			name:     "URL encoded slashes in branch name",
			url:      "http://jenkins/job/Repo/job/feature%2Fmy-branch/",
			expected: "Repo/feature/my-branch",
		},
		{
			name:     "mixed encoding",
			url:      "http://jenkins/job/Team%20A/job/Project/job/feature%2Fbranch%20name/",
			expected: "Team A/Project/feature/branch name",
		},
		{
			name:     "no job path",
			url:      "http://jenkins/",
			expected: "",
		},
		{
			name:     "trailing slash removed",
			url:      "http://jenkins/job/MyJob",
			expected: "MyJob",
		},
		{
			name:     "special characters",
			url:      "http://jenkins/job/Job%2B%26Name/",
			expected: "Job+&Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJobPathFromURL(tt.url)
			if result != tt.expected {
				t.Errorf("extractJobPathFromURL(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}
