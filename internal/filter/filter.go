package filter

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Operator represents a supported comparison operator in filters.
type Operator string

const (
	OpEQ  Operator = "="
	OpNEQ Operator = "!="
	OpSUB Operator = "~"
	OpREG Operator = "~="
	OpPFX Operator = "^"
	OpSFX Operator = "$"
	OpGTE Operator = ">="
	OpLTE Operator = "<="
	OpGT  Operator = ">"
	OpLT  Operator = "<"
)

var orderedOperators = []Operator{
	OpGTE,
	OpLTE,
	OpNEQ,
	OpREG,
	OpSUB,
	OpEQ,
	OpPFX,
	OpSFX,
	OpGT,
	OpLT,
}

// Filter captures a single key/operator/value expression.
type Filter struct {
	Key      string
	Operator Operator
	Value    string
}

// Context is the typed value map used during evaluation.
type Context map[string]interface{}

// Parsing errors.
var (
	ErrInvalidFilter  = errors.New("invalid filter expression")
	ErrUnsupportedKey = errors.New("unsupported filter key")
)

var supportedKeys = []string{
	"result",
	"status",
	"branch",
	"commit",
	"cause.type",
	"cause.user",
	"queue.id",
	"started",
	"duration",
}

var secretKeywords = []string{"password", "secret", "token", "apikey", "api_key", "key", "pwd"}

// Parse converts raw flag values into Filter structures.
func Parse(raw []string) ([]Filter, error) {
	filters := make([]Filter, 0, len(raw))
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		var op Operator
		var key, value string

		for _, candidate := range orderedOperators {
			parts := strings.SplitN(entry, string(candidate), 2)
			if len(parts) != 2 {
				continue
			}

			key = strings.TrimSpace(parts[0])
			value = strings.TrimSpace(parts[1])
			op = candidate
			break
		}

		if key == "" || op == "" {
			return nil, fmt.Errorf("%w: %q", ErrInvalidFilter, entry)
		}

		if err := validateKey(key); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidFilter, err)
		}

		filters = append(filters, Filter{
			Key:      key,
			Operator: op,
			Value:    value,
		})
	}

	return filters, nil
}

// Evaluate returns true when all filters match the provided Context.
func Evaluate(ctx Context, filters []Filter, opts ...Option) bool {
	settings := applyOptions(opts...)

	for _, f := range filters {
		value, ok := ctx[f.Key]
		if !ok {
			return false
		}
		if !evaluateSingle(value, f, settings) {
			return false
		}
	}
	return true
}

func evaluateSingle(actual interface{}, f Filter, cfg settings) bool {
	switch typed := actual.(type) {
	case string:
		return evalString(typed, f, cfg)
	case time.Time:
		return evalTime(typed, f)
	case time.Duration:
		return evalDuration(typed, f)
	case int:
		return evalFloat(float64(typed), f)
	case int64:
		return evalFloat(float64(typed), f)
	case float64:
		return evalFloat(typed, f)
	case fmt.Stringer:
		return evalString(typed.String(), f, cfg)
	case []string:
		for _, entry := range typed {
			if evalString(entry, f, cfg) {
				return true
			}
		}
		return false
	case []any:
		for _, entry := range typed {
			if evalSliceEntry(entry, f, cfg) {
				return true
			}
		}
		return false
	case bool:
		return evalBool(typed, f)
	default:
		return false
	}
}

func evalSliceEntry(value any, f Filter, cfg settings) bool {
	switch v := value.(type) {
	case string:
		return evalString(v, f, cfg)
	case fmt.Stringer:
		return evalString(v.String(), f, cfg)
	default:
		return evalString(fmt.Sprint(v), f, cfg)
	}
}

func evalString(actual string, f Filter, cfg settings) bool {
	expected := f.Value

	switch f.Operator {
	case OpEQ:
		return strings.EqualFold(actual, expected)
	case OpNEQ:
		return !strings.EqualFold(actual, expected)
	case OpSUB:
		return strings.Contains(strings.ToLower(actual), strings.ToLower(expected))
	case OpPFX:
		return strings.HasPrefix(strings.ToLower(actual), strings.ToLower(expected))
	case OpSFX:
		return strings.HasSuffix(strings.ToLower(actual), strings.ToLower(expected))
	case OpREG:
		if !cfg.allowRegex {
			return strings.Contains(strings.ToLower(actual), strings.ToLower(expected))
		}
		re, err := regexp.Compile(expected)
		if err != nil {
			return false
		}
		return re.MatchString(actual)
	case OpGT, OpGTE, OpLT, OpLTE:
		num, err := strconv.ParseFloat(expected, 64)
		if err != nil {
			return false
		}
		var actualNum float64
		if v, err := strconv.ParseFloat(actual, 64); err == nil {
			actualNum = v
		} else {
			return false
		}
		return compareFloat(actualNum, num, f.Operator)
	default:
		return false
	}
}

func evalTime(actual time.Time, f Filter) bool {
	expected, err := parseTimeOrDuration(f.Value)
	if err != nil {
		return false
	}
	return compareTime(actual, expected, f.Operator)
}

func evalDuration(actual time.Duration, f Filter) bool {
	expected, err := ParseDuration(f.Value)
	if err != nil {
		return false
	}
	return compareFloat(float64(actual), float64(expected), f.Operator)
}

func evalFloat(actual float64, f Filter) bool {
	expected, err := strconv.ParseFloat(f.Value, 64)
	if err != nil {
		return false
	}
	return compareFloat(actual, expected, f.Operator)
}

func evalBool(actual bool, f Filter) bool {
	expected, err := strconv.ParseBool(strings.ToLower(f.Value))
	if err != nil {
		return false
	}

	switch f.Operator {
	case OpEQ:
		return actual == expected
	case OpNEQ:
		return actual != expected
	default:
		return false
	}
}

func compareFloat(actual, expected float64, op Operator) bool {
	switch op {
	case OpEQ:
		return actual == expected
	case OpNEQ:
		return actual != expected
	case OpGT:
		return actual > expected
	case OpGTE:
		return actual >= expected
	case OpLT:
		return actual < expected
	case OpLTE:
		return actual <= expected
	default:
		return false
	}
}

func compareTime(actual, expected time.Time, op Operator) bool {
	switch op {
	case OpEQ:
		return actual.Equal(expected)
	case OpNEQ:
		return !actual.Equal(expected)
	case OpGT:
		return actual.After(expected)
	case OpGTE:
		return actual.After(expected) || actual.Equal(expected)
	case OpLT:
		return actual.Before(expected)
	case OpLTE:
		return actual.Before(expected) || actual.Equal(expected)
	default:
		return false
	}
}

func parseTimeOrDuration(value string) (time.Time, error) {
	if d, err := ParseDuration(value); err == nil {
		return time.Now().Add(-d), nil
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, nil
	}
	return time.Time{}, fmt.Errorf("invalid time value %q", value)
}

// ParseDuration converts a string duration value into a time.Duration. It supports the Go duration syntax,
// day/week suffixes (e.g. "7d", "2w"), and raw millisecond integers.
func ParseDuration(value string) (time.Duration, error) { //nolint:cyclop
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("empty duration")
	}

	normalized := strings.ToLower(value)
	// Support suffixes d (days) and w (weeks) in addition to standard ParseDuration units.
	last := normalized[len(normalized)-1]
	switch last {
	case 'd':
		num, err := strconv.ParseFloat(normalized[:len(normalized)-1], 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(num * float64(24*time.Hour)), nil
	case 'w':
		num, err := strconv.ParseFloat(normalized[:len(normalized)-1], 64)
		if err != nil {
			return 0, err
		}
		return time.Duration(num * float64(7*24*time.Hour)), nil
	}

	if strings.ContainsAny(normalized, "hmsu") {
		return time.ParseDuration(normalized)
	}

	if millis, err := strconv.ParseFloat(normalized, 64); err == nil {
		return time.Duration(millis) * time.Millisecond, nil
	}

	return 0, fmt.Errorf("invalid duration %q", value)
}

func validateKey(key string) error {
	if strings.HasPrefix(key, "param.") ||
		strings.HasPrefix(key, "artifact.") ||
		strings.HasPrefix(key, "cause.") {
		return nil
	}

	for _, candidate := range supportedKeys {
		if candidate == key {
			return nil
		}
	}

	return fmt.Errorf("%w: %s", ErrUnsupportedKey, key)
}

// AllowedKeys returns the list of supported top-level keys.
func AllowedKeys() []string {
	base := make([]string, len(supportedKeys))
	copy(base, supportedKeys)
	base = append(base, "param.*", "artifact.*", "cause.*")
	return base
}

// Operators returns the list of supported operators.
func Operators() []string {
	result := make([]string, len(orderedOperators))
	for i, op := range orderedOperators {
		result[i] = string(op)
	}
	return result
}

// RequiresArtifacts reports if any filter references artifact fields.
func RequiresArtifacts(filters []Filter) bool {
	for _, f := range filters {
		if strings.HasPrefix(f.Key, "artifact.") {
			return true
		}
	}
	return false
}

// RequiresParameters reports if any filter references parameters.
func RequiresParameters(filters []Filter) bool {
	for _, f := range filters {
		if strings.HasPrefix(f.Key, "param.") {
			return true
		}
	}
	return false
}

// RequiresCauses reports if any filter references build causes.
func RequiresCauses(filters []Filter) bool {
	for _, f := range filters {
		if strings.HasPrefix(f.Key, "cause.") {
			return true
		}
	}
	return false
}

// IsLikelySecret indicates whether a parameter name probably holds a secret.
func IsLikelySecret(name string) bool {
	lower := strings.ToLower(name)
	for _, keyword := range secretKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// Option configures evaluation behavior.
type Option func(*settings)

type settings struct {
	allowRegex bool
}

func applyOptions(opts ...Option) settings {
	cfg := settings{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithRegexMatching enables regex evaluation for ~=.
func WithRegexMatching() Option {
	return func(s *settings) {
		s.allowRegex = true
	}
}
