package shared

import "time"

func DurationString(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	d := time.Duration(ms) * time.Millisecond
	return d.String()
}
