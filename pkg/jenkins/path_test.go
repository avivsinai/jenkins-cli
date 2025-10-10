package jenkins

import "testing"

func TestEncodeJobPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"empty", "", ""},
		{"single", "example", "job/example"},
		{"nested", "team/app/build", "job/team/job/app/job/build"},
		{"spaces", "folder name/job", "job/folder%20name/job/job"},
	}

	for _, tt := range tests {
		got := EncodeJobPath(tt.input)
		if got != tt.expect {
			t.Fatalf("%s: expected %s got %s", tt.name, tt.expect, got)
		}
	}
}
