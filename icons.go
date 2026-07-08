package site_icons

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var httpClient = &http.Client{
	Transport: &userAgentTransport{
		transport: http.DefaultTransport,
	},
}

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

	// Start manifest and favicon requests immediately — they don't depend on HTML.
	go s.loadManifestGoroutine(manifestURLs, results)
	go s.loadFaviconGoroutine(faviconURLs, results)

	// Download HTML and stream to both parsers concurrently.
	resp, err := httpClient.Get(pageURL.String())
	if err != nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// HTML fetch failed — send empty results for head/site-logo.
		results <- loadedResult{kind: kindHeadTags}
		results <- loadedResult{kind: kindSiteLogo}
	} else {
		defer resp.Body.Close()
		finalURL := resp.Request.URL

		if s.IsBlacklisted(finalURL) {
			results <- loadedResult{kind: kindHeadTags}
			results <- loadedResult{kind: kindSiteLogo}
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
			go s.loadHeadGoroutine(finalURL, headCh, results)

			// Site logo parser: needs full body, buffers all chunks then parses.
			go s.loadSiteLogoGoroutine(finalURL, logoCh, results)
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

	return icons, nil
}

func (s *SiteIcons) loadManifestGoroutine(manifestURLs []string, results chan<- loadedResult) {
	for _, mu := range manifestURLs {
		icons, err := loadManifest(mu)
		if err == nil && len(icons) > 0 {
			results <- loadedResult{kind: kindManifest, icons: icons}
			return
		}
	}
	results <- loadedResult{kind: kindManifest}
}

func (s *SiteIcons) loadFaviconGoroutine(faviconURLs []string, results chan<- loadedResult) {
	for _, fu := range faviconURLs {
		icon, err := LoadIcon(fu, SiteFavicon, nil, nil)
		if err == nil {
			results <- loadedResult{kind: kindDefaultFavicon, single: icon}
			return
		}
	}
	results <- loadedResult{kind: kindDefaultFavicon}
}

func (s *SiteIcons) loadHeadGoroutine(pageURL *url.URL, headCh <-chan chunk, results chan<- loadedResult) {
	pr, pw := io.Pipe()

	// Feed chunks from channel into the pipe writer.
	go func() {
		defer pw.Close()
		for c := range headCh {
			if _, err := pw.Write(c.data); err != nil {
				return
			}
		}
	}()

	var icons []*Icon
	for icon := range parseHead(pageURL, pr) {
		icons = append(icons, icon)
	}

	if len(icons) > 0 {
		results <- loadedResult{kind: kindHeadTags, icons: icons}
	} else {
		results <- loadedResult{kind: kindHeadTags}
	}
}

func (s *SiteIcons) loadSiteLogoGoroutine(pageURL *url.URL, logoCh <-chan chunk, results chan<- loadedResult) {
	var buf bytes.Buffer
	for c := range logoCh {
		buf.Write(c.data)
	}

	icon, err := parseSiteLogo(pageURL, buf.String(), s.IsBlacklisted)
	if err == nil && icon != nil {
		results <- loadedResult{kind: kindSiteLogo, single: icon}
	} else {
		results <- loadedResult{kind: kindSiteLogo}
	}
}
