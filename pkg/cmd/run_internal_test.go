package cmd

import "testing"

func TestDurationString(t *testing.T) {
	cases := []struct {
		name   string
		ms     int64
		expect string
	}{
		{"zero", 0, "0s"},
		{"seconds", 1000, "1s"},
		{"minute", 60000, "1m0s"},
	}

	for _, tc := range cases {
		if got := durationString(tc.ms); got != tc.expect {
			t.Fatalf("%s: expected %s got %s", tc.name, tc.expect, got)
		}
	}
}

func TestExitCodeForResult(t *testing.T) {
	cases := map[string]int{
		"SUCCESS":   0,
		"UNSTABLE":  10,
		"FAILURE":   11,
		"ABORTED":   12,
		"NOT_BUILT": 13,
		"UNKNOWN":   0,
	}

	for input, expect := range cases {
		if got := exitCodeForResult(input); got != expect {
			t.Fatalf("%s: expected %d got %d", input, expect, got)
		}
	}
}
