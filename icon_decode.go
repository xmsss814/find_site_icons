package site_icons

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// detectFormat identifies the image format from magic bytes.
func detectFormat(data []byte) string {
	if len(data) < 2 {
		return ""
	}
	if len(data) >= 8 && bytes.HasPrefix(data, []byte{0x89, 0x50, 0x4E, 0x47}) {
		return "png"
	}
	if bytes.HasPrefix(data, []byte{0xFF, 0xD8}) {
		return "jpeg"
	}
	if bytes.HasPrefix(data, []byte{0x00, 0x00}) {
		return "ico"
	}
	if len(data) >= 4 && bytes.HasPrefix(data, []byte("GIF8")) {
		return "gif"
	}
	if bytes.HasPrefix(data, []byte{0x3C}) || bytes.HasPrefix(data, []byte{0x60}) {
		// SVG starts with '<' or '`'
		return "svg"
	}
	return ""
}

// getPNGSize reads PNG dimensions from the IHDR chunk.
func getPNGSize(r io.Reader) (*IconSize, error) {
	// PNG signature (8 bytes) + IHDR length (4) + "IHDR" (4) = 16 bytes before dimensions
	header := make([]byte, 24)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read PNG header: %w", err)
	}

	// Verify PNG signature
	if !bytes.HasPrefix(header, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return nil, fmt.Errorf("bad PNG header")
	}

	// Verify IHDR chunk type at offset 12
	if string(header[12:16]) != "IHDR" {
		return nil, fmt.Errorf("bad PNG header: expected IHDR")
	}

	width := binary.BigEndian.Uint32(header[16:20])
	height := binary.BigEndian.Uint32(header[20:24])

	return &IconSize{Width: width, Height: height}, nil
}

// getJPEGSize reads JPEG dimensions by scanning for SOFn markers.
func getJPEGSize(r io.Reader) (*IconSize, error) {
	marker := make([]byte, 2)
	depth := int32(0)

	for {
		if _, err := io.ReadFull(r, marker); err != nil {
			return nil, fmt.Errorf("invalid jpeg: %w", err)
		}

		if marker[0] != 0xFF {
			return nil, fmt.Errorf("invalid jpeg: missing marker")
		}

		page := marker[1]

		// Check for valid SOFn markers (dimension markers)
		if (page >= 0xC0 && page <= 0xC3) ||
			(page >= 0xC5 && page <= 0xC7) ||
			(page >= 0xC9 && page <= 0xCB) ||
			(page >= 0xCD && page <= 0xCF) {
			if depth == 0 {
				// Skip 3 bytes to reach height offset
				skip := make([]byte, 3)
				if _, err := io.ReadFull(r, skip); err != nil {
					return nil, fmt.Errorf("invalid jpeg: %w", err)
				}
				break
			}
		} else if page == 0xD8 {
			depth++
		} else if page == 0xD9 {
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("invalid jpeg: unmatched SOI/EOI")
			}
		}

		// Read marker length and skip
		var pageSize uint16
		if err := binary.Read(r, binary.BigEndian, &pageSize); err != nil {
			return nil, fmt.Errorf("invalid jpeg: %w", err)
		}
		skip := make([]byte, pageSize-2)
		if _, err := io.ReadFull(r, skip); err != nil {
			return nil, fmt.Errorf("invalid jpeg: %w", err)
		}
	}

	var height, width uint16
	if err := binary.Read(r, binary.BigEndian, &height); err != nil {
		return nil, fmt.Errorf("invalid jpeg: %w", err)
	}
	if err := binary.Read(r, binary.BigEndian, &width); err != nil {
		return nil, fmt.Errorf("invalid jpeg: %w", err)
	}

	return &IconSize{Width: uint32(width), Height: uint32(height)}, nil
}

// getGIFSize reads GIF dimensions from the logical screen descriptor.
func getGIFSize(r io.Reader) (*IconSize, error) {
	header := make([]byte, 10)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read GIF header: %w", err)
	}

	// Verify GIF87a or GIF89a signature
	if !bytes.HasPrefix(header, []byte("GIF8")) {
		return nil, fmt.Errorf("bad GIF header")
	}

	// Bytes 6-7: width (little-endian), Bytes 8-9: height (little-endian)
	width := binary.LittleEndian.Uint16(header[6:8])
	height := binary.LittleEndian.Uint16(header[8:10])

	return &IconSize{Width: uint32(width), Height: uint32(height)}, nil
}

// getICOSizes reads ICO directory entries to get all sizes.
func getICOSizes(r io.Reader) (*IconSizes, error) {
	// Read ICO header (6 bytes)
	header := make([]byte, 6)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("failed to read ICO header: %w", err)
	}

	iconType := binary.LittleEndian.Uint16(header[2:4])
	if iconType != 1 {
		return nil, fmt.Errorf("bad ICO header: not an icon file")
	}

	iconCount := binary.LittleEndian.Uint16(header[4:6])

	// Read directory entries (16 bytes each)
	entries := make([]byte, int(iconCount)*16)
	if _, err := io.ReadFull(r, entries); err != nil {
		return nil, fmt.Errorf("failed to read ICO entries: %w", err)
	}

	sizes := make([]IconSize, 0, iconCount)
	for i := 0; i < int(iconCount); i++ {
		offset := i * 16
		w := entries[offset]
		h := entries[offset+1]

		if w == 0 && h == 0 {
			// Embedded PNG - read the PNG to get actual size
			// The image data follows after the directory. We need to seek to it.
			// Read remaining data to find the PNG
			imageOffset := binary.LittleEndian.Uint32(entries[offset+12:])
			imageSize := binary.LittleEndian.Uint32(entries[offset+8:])

			_ = imageOffset
			_ = imageSize
			// For simplicity in streaming case, use 256x256 as default for 0x0 ICO entries
			sizes = append(sizes, IconSize{Width: 256, Height: 256})
		} else {
			sizes = append(sizes, IconSize{Width: uint32(w), Height: uint32(h)})
		}
	}

	return NewIconSizes(sizes)
}

var svgViewBoxRegex = regexp.MustCompile(`-?\d+\.?\d*\s+-?\d+\.?\d*\s+(\d+\.?\d*)\s+(\d+\.?\d*)`)
var svgWidthHeightRegex = regexp.MustCompile(`<svg[^>]*>`)

// getSVGSize parses SVG to extract dimensions from viewBox or width/height.
func getSVGSize(r io.Reader) (*IconSize, error) {
	// Read the SVG content (up to a reasonable limit)
	var buf bytes.Buffer
	limited := io.LimitReader(r, 65536)
	if _, err := io.Copy(&buf, limited); err != nil {
		return nil, fmt.Errorf("failed to read SVG: %w", err)
	}
	svgContent := buf.String()

	// Try to find width and height attributes
	width := parseSVGAttr(svgContent, "width")
	height := parseSVGAttr(svgContent, "height")

	if width > 0 && height > 0 {
		return &IconSize{Width: width, Height: height}, nil
	}

	// Try viewBox
	if matches := svgViewBoxRegex.FindStringSubmatch(svgContent); matches != nil {
		w, _ := strconv.ParseFloat(matches[1], 64)
		h, _ := strconv.ParseFloat(matches[2], 64)
		if w > 0 && h > 0 {
			return &IconSize{Width: uint32(w + 0.5), Height: uint32(h + 0.5)}, nil
		}
	}

	return nil, nil // SVG without dimensions is OK (scalable)
}

var svgAttrRegex = regexp.MustCompile(`(?i)(width|height)\s*=\s*["']?(\d+\.?\d*)`)

func parseSVGAttr(svg, attr string) uint32 {
	// Find the opening svg tag
	tagStart := strings.Index(strings.ToLower(svg), "<svg")
	if tagStart == -1 {
		return 0
	}
	tagEnd := strings.Index(svg[tagStart:], ">")
	if tagEnd == -1 {
		return 0
	}
	tag := svg[tagStart : tagStart+tagEnd+1]

	matches := svgAttrRegex.FindAllStringSubmatch(tag, -1)
	for _, m := range matches {
		if strings.EqualFold(m[1], attr) {
			v, _ := strconv.ParseFloat(m[2], 64)
			return uint32(v + 0.5)
		}
	}
	return 0
}

// decodeIconInfo determines image type and dimensions from raw bytes.
func decodeIconInfo(data []byte, contentType string, sizes *IconSizes) (*IconInfo, error) {
	format := detectFormat(data)

	// If we already have sizes from a <link> tag, use them
	if sizes != nil {
		switch format {
		case "png":
			return &IconInfo{Type: "png", Size: ptrIconSize(sizes.Largest()), Data: ""}, nil
		case "jpeg":
			return &IconInfo{Type: "jpeg", Size: ptrIconSize(sizes.Largest()), Data: ""}, nil
		case "gif":
			return &IconInfo{Type: "gif", Size: ptrIconSize(sizes.Largest()), Data: ""}, nil
		case "ico":
			return &IconInfo{Type: "ico", Sizes: sizes, Data: ""}, nil
		case "svg":
			return &IconInfo{Type: "svg", Size: ptrIconSize(sizes.Largest()), Data: ""}, nil
		}
	}

	// Otherwise, determine format from content type or magic bytes
	kind := format
	if kind == "" {
		kind = mimeToKind(contentType)
	}

	r := bytes.NewReader(data)

	switch kind {
	case "png":
		size, err := getPNGSize(r)
		if err != nil {
			return nil, err
		}
		return &IconInfo{Type: "png", Size: size, Data: ""}, nil

	case "jpeg":
		size, err := getJPEGSize(r)
		if err != nil {
			return nil, err
		}
		return &IconInfo{Type: "jpeg", Size: size, Data: ""}, nil

	case "gif":
		size, err := getGIFSize(r)
		if err != nil {
			return nil, err
		}
		return &IconInfo{Type: "gif", Size: size, Data: ""}, nil

	case "ico":
		icoSizes, err := getICOSizes(r)
		if err != nil {
			return nil, err
		}
		return &IconInfo{Type: "ico", Sizes: icoSizes, Data: ""}, nil

	case "svg":
		size, err := getSVGSize(r)
		if err != nil {
			return nil, err
		}
		return &IconInfo{Type: "svg", Size: size, Data: ""}, nil

	default:
		return nil, fmt.Errorf("unknown icon type: %s (content-type: %s)", format, contentType)
	}
}

func mimeToKind(contentType string) string {
	ct := strings.ToLower(contentType)
	ct = strings.SplitN(ct, ";", 2)[0]
	ct = strings.TrimSpace(ct)

	switch ct {
	case "image/png":
		return "png"
	case "image/jpeg", "image/jpg":
		return "jpeg"
	case "image/gif":
		return "gif"
	case "image/x-icon", "image/vnd.microsoft.icon":
		return "ico"
	case "image/svg+xml", "text/plain":
		return "svg"
	default:
		return ""
	}
}

func ptrIconSize(s IconSize) *IconSize {
	return &s
}
