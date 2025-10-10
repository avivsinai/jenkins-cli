package jenkins

import (
	"net/url"
	"strings"
)

const jobSegment = "job"

// EncodeJobPath converts a human path like "team/app/main" into the Jenkins URL
// form "job/team/job/app/job/main".
func EncodeJobPath(human string) string {
	trimmed := strings.Trim(human, "/")
	if trimmed == "" {
		return ""
	}

	segments := strings.Split(trimmed, "/")
	var builder strings.Builder

	for _, segment := range segments {
		if segment == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteRune('/')
		}
		builder.WriteString(jobSegment)
		builder.WriteRune('/')
		builder.WriteString(url.PathEscape(segment))
	}

	return builder.String()
}
