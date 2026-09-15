package site_icons

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// defaultTimeout bounds every request made by the scraper. A single target
// fan-out runs many requests (HTML + manifest + favicon + link icons); each
// request is capped so a hung host can't stall a whole target indefinitely.
const defaultTimeout = 12 * time.Second

// requestTimeout returns the per-request timeout, honoring the same knob the
// orchestrator uses (FIND_SITE_ICONS_TIMEOUT, in seconds) so operators can tune
// without recompiling.
func requestTimeout() time.Duration {
	if v := os.Getenv("FIND_SITE_ICONS_TIMEOUT"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return defaultTimeout
}

// newHTTPClient builds an http.Client whose Transport sets a Chrome-like
// User-Agent and whose Timeout bounds the full request lifecycle (connect +
// headers + body). The Timeout only applies to each individual request, not
// to a whole site, so per-target work is bounded by the slowest single request.
func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &userAgentTransport{
			transport: http.DefaultTransport,
		},
	}
}

var httpClient = newHTTPClient(requestTimeout())

// userAgentTransport adds a Chrome-like User-Agent to all requests.
type userAgentTransport struct {
	transport http.RoundTripper
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/88.0.4324.104 Safari/537.36")
	return t.transport.RoundTrip(req)
}

// SiteIcons is the main website icon scraper.
type SiteIcons struct {
	blacklist func(*url.URL) bool
}

// NewSiteIcons creates a new SiteIcons instance.
func NewSiteIcons() *SiteIcons {
	return &SiteIcons{}
}

// NewSiteIconsWithBlacklist creates a new SiteIcons with a URL blacklist filter.
func NewSiteIconsWithBlacklist(blacklist func(*url.URL) bool) *SiteIcons {
	return &SiteIcons{blacklist: blacklist}
}

// IsBlacklisted checks if a URL is in the blacklist.
func (s *SiteIcons) IsBlacklisted(u *url.URL) bool {
	if s.blacklist != nil {
		return s.blacklist(u)
	}
	return false
}

// loadedKind represents results from one icon source.
type loadedKind int

const (
	kindManifest loadedKind = iota
	kindHeadTags
	kindDefaultFavicon
	kindSiteLogo
)

type loadedResult struct {
	kind   loadedKind
	icons  []*Icon
	single *Icon
}

// chunk represents a piece of the HTML body stream.
type chunk struct {
	data []byte
}

// LoadWebsite scrapes a website for all available icons.
// Manifest and favicon requests start immediately (concurrent with HTML download).
// The HTML body is streamed to the head parser so icons begin loading during parsing.
func (s *SiteIcons) LoadWebsite(urlStr string, bestMatchesOnly bool) ([]*Icon, error) {
	pageURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	manifestURLs := uniqueStrings([]string{
		pushURL(pageURL, "manifest.json").String(),
		pushURL(pageURL, "manifest.webmanifest").String(),
		strings.TrimRight(pageURL.String(), "/") + "/manifest.json",
		strings.TrimRight(pageURL.String(), "/") + "/manifest.webmanifest",
	})

	faviconURLs := uniqueStrings([]string{
		pushURL(pageURL, "favicon.svg").String(),
		strings.TrimRight(pageURL.String(), "/") + "/favicon.svg",
		pushURL(pageURL, "favicon.ico").String(),
		strings.TrimRight(pageURL.String(), "/") + "/favicon.ico",
	})

	results := make(chan loadedResult, 4)
	// done aborts stragglers when we stop reading results early (--fast).
	done := make(chan struct{})
	var doneOnce sync.Once
	closeDone := func() { doneOnce.Do(func() { close(done) }) }
	defer closeDone()

	sendResult := func(r loadedResult) {
		select {
		case results <- r:
		case <-done:
		}
	}

	// Start manifest and favicon requests immediately — they don't depend on HTML.
	go s.loadManifestGoroutine(manifestURLs, sendResult)
	go s.loadFaviconGoroutine(faviconURLs, sendResult)

	// Download HTML and stream to both parsers concurrently.
	resp, err := httpClient.Get(pageURL.String())
	if err != nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// HTML fetch failed — send empty results for head/site-logo.
		sendResult(loadedResult{kind: kindHeadTags})
		sendResult(loadedResult{kind: kindSiteLogo})
	} else {
		defer resp.Body.Close()
		finalURL := resp.Request.URL

		if s.IsBlacklisted(finalURL) {
			sendResult(loadedResult{kind: kindHeadTags})
			sendResult(loadedResult{kind: kindSiteLogo})
		} else {
			// Fan out the body stream to two channels: one for head parser, one for site logo.
			headCh := make(chan chunk, 64)
			logoCh := make(chan chunk, 64)

			go func() {
				defer close(headCh)
				defer close(logoCh)
				buf := make([]byte, 8192)
				for {
					n, readErr := resp.Body.Read(buf)
					if n > 0 {
						data := make([]byte, n)
						copy(data, buf[:n])
						headCh <- chunk{data: data}
						logoCh <- chunk{data: data}
					}
					if readErr != nil {
						return
					}
				}
			}()

			// Head parser: receives body chunks via an io.Pipe, discovers and loads icons concurrently.
			go s.loadHeadGoroutine(finalURL, headCh, sendResult)

			// Site logo parser: needs full body, buffers all chunks then parses.
			go s.loadSiteLogoGoroutine(finalURL, logoCh, sendResult)
		}
	}

	// Collect results (exactly 4 results expected).
	var icons []*Icon
	foundBestMatch := false
	received := make(map[loadedKind]bool)

	for i := 0; i < 4; i++ {
		result := <-results
		received[result.kind] = true

		switch result.kind {
		case kindManifest:
			if len(result.icons) > 0 {
				icons = append(icons, result.icons...)
				foundBestMatch = true
			}

		case kindHeadTags:
			if len(result.icons) > 0 {
				icons = append(icons, result.icons...)
				foundBestMatch = true
			} else if received[kindDefaultFavicon] {
				for _, ic := range icons {
					if ic.Kind == SiteFavicon {
						foundBestMatch = true
						break
					}
				}
			}

		case kindDefaultFavicon:
			if result.single != nil {
				icons = append(icons, result.single)
				if received[kindHeadTags] {
					foundBestMatch = true
				}
			}

		case kindSiteLogo:
			if result.single != nil {
				icons = append(icons, result.single)
			}
		}

		SortIcons(icons)
		icons = DeduplicateIcons(icons)

		if bestMatchesOnly && foundBestMatch {
			break
		}
	}

	// With --fast we may stop reading before all 4 results arrive; close(done)
	// (via the deferred closeDone) unblocks any straggler send so its goroutine
	// can exit instead of leaking.
	return icons, nil
}

func (s *SiteIcons) loadManifestGoroutine(manifestURLs []string, send func(loadedResult)) {
	for _, mu := range manifestURLs {
		icons, err := loadManifest(mu)
		if err == nil && len(icons) > 0 {
			send(loadedResult{kind: kindManifest, icons: icons})
			return
		}
		// A connection-level failure (unroutable, refused, DNS) applies to
		// every remaining URL on the same host — don't burn a full timeout
		// per candidate path.
		if isDialError(err) {
			break
		}
	}
	send(loadedResult{kind: kindManifest})
}

func (s *SiteIcons) loadFaviconGoroutine(faviconURLs []string, send func(loadedResult)) {
	for _, fu := range faviconURLs {
		icon, err := LoadIcon(fu, SiteFavicon, nil, nil)
		if err == nil {
			send(loadedResult{kind: kindDefaultFavicon, single: icon})
			return
		}
		if isDialError(err) {
			break
		}
	}
	send(loadedResult{kind: kindDefaultFavicon})
}

func (s *SiteIcons) loadHeadGoroutine(pageURL *url.URL, headCh <-chan chunk, send func(loadedResult)) {
	pr, pw := io.Pipe()

	// Feed chunks from channel into the pipe writer. If the head parser
	// detached early (</head> reached), writes fail — but we must keep
	// draining headCh so the body pump (also feeding logoCh) never blocks;
	// otherwise the whole target's logo detection would stall behind it.
	go func() {
		defer pw.Close()
		for c := range headCh {
			if _, err := pw.Write(c.data); err != nil {
				for range headCh {
				}
				return
			}
		}
	}()

	var icons []*Icon
	for icon := range parseHead(pageURL, pr) {
		icons = append(icons, icon)
	}

	send(loadedResult{kind: kindHeadTags, icons: icons})
}

func (s *SiteIcons) loadSiteLogoGoroutine(pageURL *url.URL, logoCh <-chan chunk, send func(loadedResult)) {
	var buf bytes.Buffer
	buf.Grow(512 * 1024)
	for c := range logoCh {
		buf.Write(c.data)
	}

	icon, err := parseSiteLogo(pageURL, buf.String(), s.IsBlacklisted)
	if err == nil && icon != nil {
		send(loadedResult{kind: kindSiteLogo, single: icon})
	} else {
		send(loadedResult{kind: kindSiteLogo})
	}
}
