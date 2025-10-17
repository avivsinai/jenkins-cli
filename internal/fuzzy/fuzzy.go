package fuzzy

import (
	"sort"
	"strings"
)

// Match represents a fuzzy match result with score
type Match struct {
	Value string
	Score int
}

// Search performs fuzzy matching on a list of items
// Returns matches sorted by score (best first)
func Search(query string, items []string, maxResults int) []Match {
	if query == "" {
		return nil
	}

	query = strings.ToLower(query)
	matches := make([]Match, 0, len(items))

	for _, item := range items {
		score := calculateScore(query, item)
		if score > 0 {
			matches = append(matches, Match{
				Value: item,
				Score: score,
			})
		}
	}

	// Sort by score (descending)
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		// Tie-breaker: prefer shorter strings
		return len(matches[i].Value) < len(matches[j].Value)
	})

	if maxResults > 0 && len(matches) > maxResults {
		matches = matches[:maxResults]
	}

	return matches
}

// calculateScore returns a score for how well the query matches the target
// Higher scores indicate better matches
func calculateScore(query, target string) int {
	// Normalize both strings to lowercase for case-insensitive matching
	queryLower := strings.ToLower(query)
	targetLower := strings.ToLower(target)
	score := 0

	// Exact match (highest score)
	if queryLower == targetLower {
		return 1000
	}

	// Exact substring match
	if strings.Contains(targetLower, queryLower) {
		// Prefer matches at the start
		if strings.HasPrefix(targetLower, queryLower) {
			score += 500
		} else {
			score += 300
		}
	}

	// Component matching (for paths like "Tools/ada/master")
	queryParts := strings.Split(queryLower, "/")
	targetParts := strings.Split(targetLower, "/")

	componentMatched := false
	for _, qPart := range queryParts {
		qPart = strings.TrimSpace(qPart)
		if qPart == "" {
			continue
		}

		for _, tPart := range targetParts {
			if qPart == tPart {
				score += 100 // Exact component match
				componentMatched = true
			} else if strings.Contains(tPart, qPart) {
				score += 50 // Partial component match
				componentMatched = true
			}
		}
	}

	// Word-based matching
	queryWords := strings.Fields(strings.ReplaceAll(queryLower, "/", " "))
	targetWords := strings.Fields(strings.ReplaceAll(targetLower, "/", " "))

	wordMatched := false
	for _, qWord := range queryWords {
		for _, tWord := range targetWords {
			if qWord == tWord {
				score += 80
				wordMatched = true
			} else if strings.HasPrefix(tWord, qWord) {
				score += 40
				wordMatched = true
			} else if strings.Contains(tWord, qWord) {
				score += 20
				wordMatched = true
			}
		}
	}

	// Character similarity bonus only if there was a component or word match
	// This prevents unrelated jobs from getting positive scores
	if (componentMatched || wordMatched) && len(queryLower) > 3 {
		commonChars := countCommonChars(queryLower, targetLower)
		score += commonChars * 2
	}

	// Bonus for main branches (master, main, develop) to prefer them over PR branches
	// Only apply if there was some actual match
	if score > 0 && (strings.HasSuffix(targetLower, "/master") || strings.HasSuffix(targetLower, "/main") || strings.HasSuffix(targetLower, "/develop")) {
		score += 50
	}

	return score
}

// countCommonChars counts how many characters from query appear in target
func countCommonChars(query, target string) int {
	charCount := make(map[rune]int)
	for _, ch := range target {
		charCount[ch]++
	}

	common := 0
	for _, ch := range query {
		if charCount[ch] > 0 {
			common++
			charCount[ch]--
		}
	}
	return common
}

// ExtractValues extracts string values from matches
func ExtractValues(matches []Match) []string {
	values := make([]string, len(matches))
	for i, m := range matches {
		values[i] = m.Value
	}
	return values
}
