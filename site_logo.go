package site_icons

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	skipClassRegex = regexp.MustCompile(`(?i)menu|search`)
	logoRegex      = regexp.MustCompile(`(?i)logo([^s]|$)`)
)

// parseSiteLogo scans HTML for a site logo using weighted CSS selectors.
func parseSiteLogo(pageURL *url.URL, bodyStr string, isBlacklisted func(*url.URL) bool) (*Icon, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
	if err != nil {
		return nil, err
	}

	selectors := []string{
		"a[href='/'] img, a[href='/'] svg",
		"header img, header svg",
		"img[src*=logo]",
		"img[alt*=logo], svg[alt*=logo]",
		"*[class*=logo] img, *[class*=logo] svg",
		"*[id*=logo] img, *[id*=logo] svg",
		"img[class*=logo], svg[class*=logo]",
		"img[id*=logo], svg[id*=logo]",
	}

	type candidate struct {
		href   string
		isImg  bool
		weight int
	}

	var candidates []candidate
	seen := make(map[string]bool)

	for i, sel := range selectors {
		doc.Find(sel).Each(func(_ int, s *goquery.Selection) {
			elem := s.Get(0)
			if elem == nil {
				return
			}

			// Check if any ancestor has menu/search in class/id
			shouldSkip := false
			s.Parents().AddSelection(s).Each(func(_ int, ancestor *goquery.Selection) {
				if shouldSkip {
					return
				}
				node := ancestor.Get(0)
				if node == nil {
					return
				}
				for _, attr := range node.Attr {
					key := strings.ToLower(attr.Key)
					if key == "class" || key == "id" {
						if skipClassRegex.MatchString(strings.ToLower(attr.Val)) {
							shouldSkip = true
							return
						}
					}
				}
			})
			if shouldSkip {
				return
			}

			weight := 0

			// Check if in header
			inHeader := false
			s.Parents().Each(func(_ int, p *goquery.Selection) {
				if inHeader {
					return
				}
				node := p.Get(0)
				if node != nil && strings.EqualFold(node.Data, "header") {
					inHeader = true
				}
			})
			if inHeader {
				weight += 2
			}

			// First match gets +1
			if i == 0 {
				weight += 1
			}

			// Check if any ancestor has href="/"
			hasRootHref := false
			s.Parents().AddSelection(s).Each(func(_ int, el *goquery.Selection) {
				if hasRootHref {
					return
				}
				node := el.Get(0)
				if node == nil {
					return
				}
				for _, attr := range node.Attr {
					if strings.ToLower(attr.Key) == "href" && attr.Val == "/" {
						hasRootHref = true
						return
					}
				}
			})
			if hasRootHref {
				weight += 5
			}

			// Check for "logo" mentions in class/id/alt/src
			logoWeights := map[string]int{"class": 3, "id": 3, "alt": 2, "src": 1}

			checkMentions := func(sel *goquery.Selection) {
				node := sel.Get(0)
				if node == nil {
					return
				}
				for _, attr := range node.Attr {
					lowerKey := strings.ToLower(attr.Key)
					if w, ok := logoWeights[lowerKey]; ok {
						if logoRegex.MatchString(strings.ToLower(attr.Val)) {
							weight += w
						}
					}
				}
			}

			s.Parents().AddSelection(s).Each(func(_ int, el *goquery.Selection) {
				checkMentions(el)
			})

			// Domain name match in alt text
			domain := extractDomain(pageURL.Host)
			if domain != "" {
				s.Parents().AddSelection(s).Each(func(_ int, el *goquery.Selection) {
					node := el.Get(0)
					if node == nil {
						return
					}
					for _, attr := range node.Attr {
						if strings.ToLower(attr.Key) == "alt" {
							segments := strings.Split(strings.ToLower(domain), "-")
							for _, seg := range segments {
								if strings.Contains(strings.ToLower(attr.Val), seg) {
									weight += 10
									break
								}
							}
						}
					}
				})
			}

			// Get href/src
			var href string
			tagName := strings.ToLower(elem.Data)
			if tagName == "svg" {
				// Encode SVG element to data URI
				svgHTML, _ := s.Html()
				href = encodeSVG(svgHTML)
			} else {
				for _, attr := range elem.Attr {
					if strings.ToLower(attr.Key) == "src" {
						href = attr.Val
						break
					}
				}
			}

			if href == "" {
				return
			}

			// Check blacklist
			if isBlacklisted != nil {
				u, err := url.Parse(href)
				if err == nil {
					// Handle relative URLs
					if !u.IsAbs() {
						u = pageURL.ResolveReference(u)
					}
					if isBlacklisted(u) {
						return
					}
				}
			}

			if seen[href] {
				return
			}
			seen[href] = true

			candidates = append(candidates, candidate{href: href, isImg: tagName == "img", weight: weight})
		})
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Sort by weight descending
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].weight > candidates[i].weight {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Prefer <img> over SVG for same weight
	var bestCandidate *candidate
	prevWeight := -1
	for i := range candidates {
		c := &candidates[i]
		if prevWeight >= 0 && c.weight != prevWeight {
			break
		}
		prevWeight = c.weight
		if c.isImg {
			bestCandidate = c
			break
		}
	}

	if bestCandidate == nil {
		bestCandidate = &candidates[0]
	}

	// Resolve URL
	iconURL := bestCandidate.href
	if !strings.HasPrefix(iconURL, "data:") {
		u, err := url.Parse(iconURL)
		if err == nil && !u.IsAbs() {
			u = pageURL.ResolveReference(u)
			iconURL = u.String()
		}
	}

	return LoadIcon(iconURL, SiteLogo, nil, nil)
}

func extractDomain(host string) string {
	// Remove port if present
	h := host
	if idx := strings.Index(h, ":"); idx != -1 {
		h = h[:idx]
	}
	// Extract the main domain (second-level domain)
	parts := strings.Split(h, ".")
	if len(parts) >= 2 {
		// For domains like example.com, return "example"
		// For domains like www.example.co.uk, return the second-level
		if len(parts) > 2 {
			// Simple heuristic: return the part before the TLD
			return parts[len(parts)-2]
		}
		return parts[0]
	}
	return h
}
