package site_icons

import (
	"io"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

// parseHead parses HTML <head> from a streaming body, loading icons concurrently
// as they are discovered. Returns a channel that receives icons as they complete.
// The channel is closed when parsing finishes and all icon loads are done.
func parseHead(pageURL *url.URL, body io.Reader) <-chan *Icon {
	ch := make(chan *Icon, 32)

	go func() {
		defer close(ch)

		var wg sync.WaitGroup
		tokenizer := html.NewTokenizer(body)

		for {
			tokenType := tokenizer.Next()

			switch tokenType {
			case html.ErrorToken:
				wg.Wait()
				return

			case html.EndTagToken:
				name, _ := tokenizer.TagName()
				if strings.EqualFold(string(name), "head") {
					// Head section done. Detach the parser from the shared
					// body stream: closing the pipe reader makes the feed
					// goroutine's writes fail fast, and it keeps draining its
					// channel so the body pump (which also feeds the
					// site-logo side) never blocks.
					if pc, ok := body.(interface{ CloseWithError(error) error }); ok {
						pc.CloseWithError(io.EOF)
					}
					wg.Wait()
					return
				}

			case html.StartTagToken, html.SelfClosingTagToken:
				name, _ := tokenizer.TagName()
				if !strings.EqualFold(string(name), "link") {
					continue
				}

				attrs := readAttrs(tokenizer)
				href, rel, sizes := extractLinkAttrs(attrs)

				if href == "" {
					continue
				}

				if strings.Contains(rel, "manifest") {
					resolvedURL, err := pageURL.Parse(href)
					if err != nil {
						continue
					}
					manifestURL := resolvedURL.String()

					wg.Add(1)
					go func(mu string) {
						defer wg.Done()
						icons, err := loadManifest(mu)
						if err != nil {
							return
						}
						for _, icon := range icons {
							ch <- icon
						}
					}(manifestURL)
				} else if strings.Contains(rel, "icon") || strings.Contains(rel, "apple-touch-icon") {
					kind := SiteFavicon
					if strings.Contains(rel, "apple-touch-icon") {
						kind = AppIcon
					}

					resolvedURL, err := pageURL.Parse(href)
					if err != nil {
						continue
					}

					wg.Add(1)
					go func(iconURL string, k IconKind, sizesStr string) {
						defer wg.Done()
						var sizes *IconSizes
						if sizesStr != "" {
							sizes, _ = ParseIconSizes(sizesStr)
						}
						icon, err := LoadIcon(iconURL, k, sizes, nil)
						if err == nil {
							ch <- icon
						}
					}(resolvedURL.String(), kind, sizes)
				}
			}
		}
	}()

	return ch
}

// extractLinkAttrs extracts href, rel, and sizes from link tag attributes.
func extractLinkAttrs(attrs []attr) (href, rel, sizes string) {
	for _, a := range attrs {
		switch strings.ToLower(a.Key) {
		case "href":
			href = a.Val
		case "rel":
			rel = strings.ToLower(a.Val)
		case "sizes":
			sizes = a.Val
		}
	}
	return
}

// attr is a key-value pair for HTML attributes.
type attr struct {
	Key, Val string
}

// readAttrs reads all attributes from the current token.
func readAttrs(tokenizer *html.Tokenizer) []attr {
	var attrs []attr
	for {
		key, val, more := tokenizer.TagAttr()
		attrs = append(attrs, attr{Key: string(key), Val: string(val)})
		if !more {
			break
		}
	}
	return attrs
}
