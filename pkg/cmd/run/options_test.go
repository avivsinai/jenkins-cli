package run

import (
	"testing"
	"time"
)

func TestParseSelectFields(t *testing.T) {
	fields, err := parseSelectFields("Number, Result, parameters, number")
	if err != nil {
		t.Fatalf("parseSelectFields error: %v", err)
	}
	expected := []string{"number", "parameters", "result"}
	if len(fields) != len(expected) {
		t.Fatalf("expected %d fields, got %d", len(expected), len(fields))
	}
	for i, field := range expected {
		if fields[i] != field {
			t.Fatalf("expected field %s at index %d, got %s", field, i, fields[i])
		}
	}
}

func TestParseSelectFieldsInvalid(t *testing.T) {
	if _, err := parseSelectFields("unknown"); err == nil {
		t.Fatal("expected error for unsupported select field")
	}
}

func TestNormalizeAggregation(t *testing.T) {
	if agg, err := normalizeAggregation(""); err != nil || agg != "count" {
		t.Fatalf("expected default count, got %s (err=%v)", agg, err)
	}
	if agg, err := normalizeAggregation("LAST"); err != nil || agg != "last" {
		t.Fatalf("expected last, got %s (err=%v)", agg, err)
	}
	if _, err := normalizeAggregation("sum"); err == nil {
		t.Fatal("expected error for unsupported aggregation")
	}
}

func TestParseSince(t *testing.T) {
	ts, err := parseSince("2025-10-01T00:00:00Z")
	if err != nil {
		t.Fatalf("expected RFC3339 parse success, got %v", err)
	}
	if ts.Format(time.RFC3339) != "2025-10-01T00:00:00Z" {
		t.Fatalf("unexpected timestamp %s", ts)
	}

	value, err := parseSince("1h")
	if err != nil {
		t.Fatalf("parseSince duration error: %v", err)
	}
	diff := time.Since(value)
	if diff < 30*time.Minute || diff > 90*time.Minute {
		t.Fatalf("expected diff to be near 1h, got %s", diff)
	}
}
