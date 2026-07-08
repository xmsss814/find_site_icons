package site_icons

import (
	"net/url"
	"strings"
)

// pushURL appends a path segment to a URL.
func pushURL(u *url.URL, segment string) *url.URL {
	result := *u
	result.Path = strings.TrimRight(result.Path, "/") + "/" + segment
	result.RawPath = ""
	return &result
}

// uniqueStrings deduplicates a slice of strings.
func uniqueStrings(items []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}
