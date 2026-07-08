package site_icons

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// IconSize represents the width and height of an icon.
type IconSize struct {
	Width  uint32 `json:"width"`
	Height uint32 `json:"height"`
}

// NewIconSize creates a new IconSize.
func NewIconSize(width, height uint32) IconSize {
	return IconSize{Width: width, Height: height}
}

// ParseIconSize parses an IconSize from a "WxH" string.
func ParseIconSize(s string) (IconSize, error) {
	parts := strings.Split(s, "x")
	if len(parts) != 2 {
		return IconSize{}, fmt.Errorf("invalid icon size: %s", s)
	}
	w, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return IconSize{}, fmt.Errorf("invalid icon size width: %s", parts[0])
	}
	h, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return IconSize{}, fmt.Errorf("invalid icon size height: %s", parts[1])
	}
	return IconSize{Width: uint32(w), Height: uint32(h)}, nil
}

// MaxRect returns the larger dimension of the icon.
func (s IconSize) MaxRect() uint32 {
	if s.Width > s.Height {
		return s.Width
	}
	return s.Height
}

// String returns the "WxH" representation.
func (s IconSize) String() string {
	return fmt.Sprintf("%dx%d", s.Width, s.Height)
}

// MarshalJSON serializes IconSize as "WxH".
func (s IconSize) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// UnmarshalJSON deserializes IconSize from "WxH".
func (s *IconSize) UnmarshalJSON(data []byte) error {
	str := strings.Trim(string(data), `"`)
	parsed, err := ParseIconSize(str)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// IconSizes is a non-empty, sorted collection of IconSize (largest first).
type IconSizes struct {
	items []IconSize
}

// NewIconSizes creates an IconSizes from a slice, sorting largest first.
func NewIconSizes(sizes []IconSize) (*IconSizes, error) {
	if len(sizes) == 0 {
		return nil, fmt.Errorf("must contain at least one size")
	}
	sort.Slice(sizes, func(i, j int) bool {
		return sizes[i].MaxRect() > sizes[j].MaxRect()
	})
	return &IconSizes{items: sizes}, nil
}

// NewIconSizesFromSingle creates an IconSizes from a single size.
func NewIconSizesFromSingle(size IconSize) *IconSizes {
	return &IconSizes{items: []IconSize{size}}
}

// ParseIconSizes parses a space-separated sizes string like "72x72 96x96".
func ParseIconSizes(sizesStr string) (*IconSizes, error) {
	parts := strings.Fields(sizesStr)
	sizes := make([]IconSize, 0, len(parts))
	for _, part := range parts {
		size, err := ParseIconSize(part)
		if err != nil {
			continue
		}
		sizes = append(sizes, size)
	}
	if len(sizes) == 0 {
		return nil, fmt.Errorf("no valid sizes in: %s", sizesStr)
	}
	return NewIconSizes(sizes)
}

// Largest returns the largest size.
func (is *IconSizes) Largest() IconSize {
	return is.items[0]
}

// Sizes returns all sizes.
func (is *IconSizes) Sizes() []IconSize {
	return is.items
}

// AddSize adds a size in sorted position.
func (is *IconSizes) AddSize(size IconSize) {
	idx := sort.Search(len(is.items), func(i int) bool {
		return is.items[i].MaxRect() <= size.MaxRect()
	})
	if idx < len(is.items) && is.items[idx] == size {
		return
	}
	is.items = append(is.items, IconSize{})
	copy(is.items[idx+1:], is.items[idx:])
	is.items[idx] = size
}

// String returns space-separated sizes.
func (is *IconSizes) String() string {
	strs := make([]string, len(is.items))
	for i, s := range is.items {
		strs[i] = s.String()
	}
	return strings.Join(strs, " ")
}

// MarshalJSON serializes IconSizes as a space-separated string.
func (is IconSizes) MarshalJSON() ([]byte, error) {
	return []byte(`"` + is.String() + `"`), nil
}

// UnmarshalJSON deserializes IconSizes from a space-separated string.
func (is *IconSizes) UnmarshalJSON(data []byte) error {
	str := strings.Trim(string(data), `"`)
	parsed, err := ParseIconSizes(str)
	if err != nil {
		return err
	}
	is.items = parsed.items
	return nil
}
