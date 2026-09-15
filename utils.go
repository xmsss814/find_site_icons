package site_icons

import (
	"errors"
	"net"
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

// isDialError reports whether err chain contains a connection-establishment
// failure (DNS, TCP dial, TLS handshake) as opposed to an HTTP-level error
// (404 etc). Connection-level failures invalidate every other URL on the same
// host, letting callers skip the remaining candidates instead of timing out
// on each one.
func isDialError(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Op == "dial" || opErr.Op == "read" || opErr.Op == "write"
	}
	// http.Client errors (e.g. context deadline / timeout) wrap the cause.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
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
