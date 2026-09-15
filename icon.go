package site_icons

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// IconKind represents the category of an icon.
type IconKind int

const (
	AppIcon     IconKind = 0
	SiteFavicon IconKind = 1
	SiteLogo    IconKind = 2
)

var iconKindStrings = map[IconKind]string{
	AppIcon:     "app_icon",
	SiteFavicon: "site_favicon",
	SiteLogo:    "site_logo",
}

var iconKindFromString = map[string]IconKind{
	"app_icon":     AppIcon,
	"site_favicon": SiteFavicon,
	"site_logo":    SiteLogo,
}

// String returns the string representation of IconKind.
func (k IconKind) String() string {
	if s, ok := iconKindStrings[k]; ok {
		return s
	}
	return "unknown"
}

// MarshalJSON serializes IconKind as a string.
func (k IconKind) MarshalJSON() ([]byte, error) {
	return json.Marshal(k.String())
}

// UnmarshalJSON deserializes IconKind from a string.
func (k *IconKind) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if v, ok := iconKindFromString[s]; ok {
		*k = v
		return nil
	}
	return fmt.Errorf("unknown icon kind: %s", s)
}

// iconKindOrder returns the priority order for sorting (lower = higher priority).
func iconInfoTypeOrder(t string) int {
	switch t {
	case "svg":
		return 0
	case "png":
		return 1
	case "gif":
		return 2
	case "jpeg":
		return 3
	case "ico":
		return 4
	default:
		return 5
	}
}

// IconInfo holds the type, size, and base64-encoded data of an icon.
type IconInfo struct {
	Type  string     `json:"type"`
	Size  *IconSize  `json:"size,omitempty"`
	Sizes *IconSizes `json:"sizes,omitempty"`
	Data  string     `json:"data"`
}

// SizeOrLargest returns the single size or the largest from sizes.
func (info *IconInfo) SizeOrLargest() *IconSize {
	if info.Size != nil {
		return info.Size
	}
	if info.Sizes != nil {
		s := info.Sizes.Largest()
		return &s
	}
	return nil
}

// MimeType returns the MIME type for the icon.
func (info *IconInfo) MimeType() string {
	switch info.Type {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	case "ico":
		return "image/x-icon"
	case "gif":
		return "image/gif"
	case "svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}

// Icon represents a website icon with its metadata.
type Icon struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Kind    IconKind          `json:"kind"`
	Info    *IconInfo         `json:"-"`
}

// MarshalJSON flattens IconInfo fields into the Icon level (matching Rust serde(flatten)).
func (i *Icon) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{
		"url":     i.URL,
		"headers": i.Headers,
		"kind":    i.Kind,
		"type":    i.Info.Type,
		"data":    i.Info.Data,
	}
	if i.Info.Size != nil {
		m["size"] = i.Info.Size
	}
	if i.Info.Sizes != nil {
		m["sizes"] = i.Info.Sizes
	}
	return json.Marshal(m)
}

// UnmarshalJSON populates Icon from flattened JSON.
func (i *Icon) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	json.Unmarshal(raw["url"], &i.URL)
	json.Unmarshal(raw["headers"], &i.Headers)
	json.Unmarshal(raw["kind"], &i.Kind)

	info := &IconInfo{}
	json.Unmarshal(raw["type"], &info.Type)
	json.Unmarshal(raw["data"], &info.Data)

	if sizeData, ok := raw["size"]; ok {
		var size IconSize
		if err := json.Unmarshal(sizeData, &size); err == nil {
			info.Size = &size
		}
	}
	if sizesData, ok := raw["sizes"]; ok {
		var sizes IconSizes
		if err := json.Unmarshal(sizesData, &sizes); err == nil {
			info.Sizes = &sizes
		}
	}

	i.Info = info
	return nil
}

// LoadIcon fetches an icon from a URL, determines its type and dimensions,
// and returns an Icon with base64-encoded data.
func LoadIcon(iconURL string, kind IconKind, sizes *IconSizes, headers map[string]string) (*Icon, error) {
	if headers == nil {
		headers = make(map[string]string)
	}

	info, err := loadIconInfo(iconURL, headers, sizes)
	if err != nil {
		return nil, err
	}

	return &Icon{
		URL:     iconURL,
		Headers: headers,
		Kind:    kind,
		Info:    info,
	}, nil
}

// loadIconInfo fetches icon data from a URL and decodes its info.
func loadIconInfo(iconURL string, headers map[string]string, sizes *IconSizes) (*IconInfo, error) {
	parsedURL, err := url.Parse(iconURL)
	if err != nil {
		return nil, fmt.Errorf("invalid icon URL: %w", err)
	}

	var data []byte
	var contentType string

	if parsedURL.Scheme == "data" {
		// Handle data URI
		data, contentType, err = parseDataURI(iconURL)
		if err != nil {
			return nil, err
		}
	} else {
		// Fetch from URL
		req, err := http.NewRequest("GET", iconURL, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch icon: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("failed to fetch icon: HTTP %d", resp.StatusCode)
		}

		contentType = resp.Header.Get("Content-Type")

		data, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read icon body: %w", err)
		}
	}

	// Base64 encode the data
	b64data := base64.StdEncoding.EncodeToString(data)

	// Determine type from content type if sizes are provided and we can use them
	kind := mimeToKind(contentType)

	// If sizes are provided from HTML attributes, use them directly
	if sizes != nil && kind != "" {
		switch kind {
		case "png":
			return &IconInfo{Type: "png", Size: ptrIconSize(sizes.Largest()), Data: b64data}, nil
		case "jpeg":
			return &IconInfo{Type: "jpeg", Size: ptrIconSize(sizes.Largest()), Data: b64data}, nil
		case "gif":
			return &IconInfo{Type: "gif", Size: ptrIconSize(sizes.Largest()), Data: b64data}, nil
		case "ico":
			return &IconInfo{Type: "ico", Sizes: sizes, Data: b64data}, nil
		case "svg":
			return &IconInfo{Type: "svg", Size: ptrIconSize(sizes.Largest()), Data: b64data}, nil
		}
	}

	// Decode from binary data
	info, err := decodeIconInfo(data, contentType, sizes)
	if err != nil {
		return nil, err
	}
	info.Data = b64data
	return info, nil
}

// parseDataURI parses a data: URI and returns the decoded bytes and MIME type.
func parseDataURI(uri string) ([]byte, string, error) {
	if !strings.HasPrefix(uri, "data:") {
		return nil, "", fmt.Errorf("not a data URI")
	}

	rest := uri[5:]
	commaIdx := strings.Index(rest, ",")
	if commaIdx == -1 {
		return nil, "", fmt.Errorf("invalid data URI")
	}

	meta := rest[:commaIdx]
	data := rest[commaIdx+1:]

	mimeType := "text/plain"
	if strings.HasSuffix(meta, ";base64") {
		mimeType = strings.TrimSuffix(meta, ";base64")
		if mimeType == "" {
			mimeType = "text/plain"
		}
		decoded, err := base64.StdEncoding.DecodeString(data)
		return decoded, mimeType, err
	}

	if meta != "" {
		mimeType = meta
	}

	return []byte(data), mimeType, nil
}

// SortIcons sorts icons by type priority (SVG > PNG > GIF > JPEG > ICO) then by size descending.
func SortIcons(icons []*Icon) {
	sort.Slice(icons, func(i, j int) bool {
		return compareIconInfo(icons[i].Info, icons[j].Info) < 0
	})
}

// compareIconInfo returns -1 if a should come before b.
func compareIconInfo(a, b *IconInfo) int {
	// SVG always wins over non-SVG
	aIsSVG := a.Type == "svg"
	bIsSVG := b.Type == "svg"

	if aIsSVG && !bIsSVG {
		return -1
	}
	if !aIsSVG && bIsSVG {
		return 1
	}

	if aIsSVG && bIsSVG {
		// SVG with size comes before SVG without
		aHasSize := a.Size != nil
		bHasSize := b.Size != nil
		if aHasSize && !bHasSize {
			return -1
		}
		if !aHasSize && bHasSize {
			return 1
		}
		if aHasSize && bHasSize {
			return compareSizeDesc(a.Size, b.Size)
		}
		return 0
	}

	// Non-SVG: compare by size first, then by type priority
	aSize := a.SizeOrLargest()
	bSize := b.SizeOrLargest()

	if aSize != nil && bSize != nil {
		if cmp := compareSizeDesc(aSize, bSize); cmp != 0 {
			return cmp
		}
	}

	// Same size: compare by type
	aOrder := iconInfoTypeOrder(a.Type)
	bOrder := iconInfoTypeOrder(b.Type)
	return aOrder - bOrder
}

// compareSizeDesc returns -1 if a is larger than b.
func compareSizeDesc(a, b *IconSize) int {
	if a.MaxRect() > b.MaxRect() {
		return -1
	}
	if a.MaxRect() < b.MaxRect() {
		return 1
	}
	return 0
}

// DeduplicateIcons removes duplicate icons based on URL + sorted headers.
func DeduplicateIcons(icons []*Icon) []*Icon {
	seen := make(map[string]bool)
	result := make([]*Icon, 0, len(icons))

	for _, icon := range icons {
		key := icon.URL
		if len(icon.Headers) > 0 {
			keys := make([]string, 0, len(icon.Headers))
			for k := range icon.Headers {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				key += "|" + k + "=" + icon.Headers[k]
			}
		}

		if !seen[key] {
			seen[key] = true
			result = append(result, icon)
		}
	}

	return result
}
