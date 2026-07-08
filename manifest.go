package site_icons

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sync"
)

// ManifestIcon represents an icon entry in a web app manifest.
type ManifestIcon struct {
	Src   string `json:"src"`
	Sizes string `json:"sizes,omitempty"`
}

// Manifest represents a web app manifest.
type Manifest struct {
	Icons []ManifestIcon `json:"icons"`
}

// loadManifest fetches and parses a web app manifest, loading all icons.
func loadManifest(manifestURL string) ([]*Icon, error) {
	resp, err := httpClient.Get(manifestURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest %s: %w", manifestURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("manifest fetch failed: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest body: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	baseURL, err := url.Parse(manifestURL)
	if err != nil {
		return nil, fmt.Errorf("invalid manifest URL: %w", err)
	}

	var wg sync.WaitGroup
	results := make(chan *Icon, len(manifest.Icons))

	for _, mIcon := range manifest.Icons {
		wg.Add(1)
		go func(mi ManifestIcon) {
			defer wg.Done()

			srcURL, err := baseURL.Parse(mi.Src)
			if err != nil {
				return
			}

			var sizes *IconSizes
			if mi.Sizes != "" {
				sizes, _ = ParseIconSizes(mi.Sizes)
			}

			icon, err := LoadIcon(srcURL.String(), AppIcon, sizes, nil)
			if err != nil {
				return
			}
			results <- icon
		}(mIcon)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var icons []*Icon
	for icon := range results {
		icons = append(icons, icon)
	}

	return icons, nil
}
