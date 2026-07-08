package site_icons

import (
	"net/url"
	"regexp"
	"strings"
)

const dataURIPrefix = "data:image/svg+xml,"

// Percent-encoding set for data URIs (avoids encoding safe chars).
// Based on the Rust version's DATA_URI AsciiSet.
var dataURISafeChars = map[byte]bool{
	'-': true, '.': true, '_': true, '~': true,
	'!': true, '$': true, '&': true, '\'': true,
	'(': true, ')': true, '*': true, '+': true,
	',': true, ';': true, '=': true, ':': true,
	'@': true, '/': true,
}

func shouldEncode(b byte) bool {
	if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') {
		return false
	}
	return !dataURISafeChars[b]
}

func percentEncode(s string) string {
	var buf strings.Builder
	for i := 0; i < len(s); i++ {
		b := s[i]
		if shouldEncode(b) {
			buf.WriteString(url.QueryEscape(string(b)))
		} else {
			buf.WriteByte(b)
		}
	}
	return buf.String()
}

var svgTagRegex = regexp.MustCompile(`(?i)<svg`)
var xmlnsRegex = regexp.MustCompile(`(?i)xmlns=['"]?http://www\.w3\.org/2000/svg['"]?`)

// encodeSVG converts an SVG HTML string into a data: URI.
func encodeSVG(svg string) string {
	// Add namespace if missing
	if !xmlnsRegex.MatchString(svg) {
		svg = svgTagRegex.ReplaceAllString(svg, "<svg xmlns='http://www.w3.org/2000/svg'")
	}

	// Use single quotes instead of double quotes
	svg = strings.ReplaceAll(svg, "\"", "'")

	// Remove fill='none' attribute
	fillNoneRegex := regexp.MustCompile(`(?i)fill\s*=\s*['"]?none['"]?`)
	svg = fillNoneRegex.ReplaceAllString(svg, "")

	// Remove whitespace between tags
	wsBetweenTags := regexp.MustCompile(`>\s+<`)
	svg = wsBetweenTags.ReplaceAllString(svg, "><")

	// Collapse multiple whitespace
	multiWS := regexp.MustCompile(`\s{2,}`)
	svg = multiWS.ReplaceAllString(svg, " ")

	return dataURIPrefix + percentEncode(svg)
}
