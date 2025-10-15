package filter

import (
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	filters, err := Parse([]string{"result=SUCCESS", "param.CHART_NAME~nova", "duration<=90m"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(filters) != 3 {
		t.Fatalf("expected 3 filters, got %d", len(filters))
	}
	if filters[0].Key != "result" || filters[0].Operator != OpEQ || filters[0].Value != "SUCCESS" {
		t.Fatalf("unexpected first filter: %#v", filters[0])
	}
	if filters[1].Key != "param.CHART_NAME" || filters[1].Operator != OpSUB {
		t.Fatalf("unexpected second filter: %#v", filters[1])
	}
	if filters[2].Operator != OpLTE {
		t.Fatalf("expected <= operator, got %s", filters[2].Operator)
	}
}

func TestParseInvalidKey(t *testing.T) {
	if _, err := Parse([]string{"unknown==value"}); err == nil {
		t.Fatal("expected error for unsupported key")
	}
}

func TestEvaluateStringAndNumeric(t *testing.T) {
	ctx := Context{
		"result":   "SUCCESS",
		"status":   "finished",
		"duration": 75 * time.Minute,
	}

	filters, err := Parse([]string{"result=SUCCESS", "duration<=90m"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !Evaluate(ctx, filters) {
		t.Fatal("expected filters to pass")
	}
}

func TestEvaluateArrayMatch(t *testing.T) {
	ctx := Context{
		"artifact.name": []string{"build.tar.gz", "logs.txt"},
	}
	filters, err := Parse([]string{"artifact.name~logs"})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if !Evaluate(ctx, filters) {
		t.Fatal("expected substring match within slice")
	}
}

func TestEvaluateTime(t *testing.T) {
	now := time.Now()
	ctx := Context{
		"started": now.Add(-30 * time.Minute),
	}
	filters, err := Parse([]string{"started>=1h"})
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if !Evaluate(ctx, filters) {
		t.Fatal("expected started>=1h to match when run is newer than 1h ago")
	}
}

func TestParseDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"15m":  15 * time.Minute,
		"2h":   2 * time.Hour,
		"1.5d": time.Duration(36) * time.Hour,
		"168h": 168 * time.Hour,
	}

	for input, expected := range cases {
		got, err := ParseDuration(input)
		if err != nil {
			t.Fatalf("ParseDuration(%q) error: %v", input, err)
		}
		if got != expected {
			t.Fatalf("ParseDuration(%q) = %v, expected %v", input, got, expected)
		}
	}
}

func TestIsLikelySecret(t *testing.T) {
	if !IsLikelySecret("API_TOKEN") {
		t.Fatal("expected API_TOKEN to be detected as secret")
	}
	if IsLikelySecret("ENV") {
		t.Fatal("expected ENV not to be secret")
	}
}
