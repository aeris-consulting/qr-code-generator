// Package qrgen validates user input and renders QR codes with an optional
// empty central area. It is the single source of truth for validation: both
// the command-line flags and the interactive prompts go through it.
package qrgen

import (
	"fmt"
	"image/color"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultSize   = 512
	MaxSize       = 4096
	MaxHolePct    = 20.0
	MinContrast   = 3.0  // WCAG contrast ratio against the white background.
	MaxTextBytes  = 2953 // QR version 40, level L, byte mode.
	DefaultColor  = "#000000"
	DefaultOutput = "qr.png"
)

// Shape is the form of the empty central area.
type Shape string

const (
	ShapeNone   Shape = ""
	ShapeCircle Shape = "circle"
	ShapeSquare Shape = "square"
)

// Options holds everything the user can configure.
type Options struct {
	Text        string
	Size        int     // image width and height in pixels
	Color       string  // #RGB or #RRGGBB, '#' optional
	Transparent bool    // transparent instead of white background
	HoleShape   Shape   // none, circle or square
	HolePercent float64 // percent of the QR symbol area, (0, MaxHolePct]
	Output      string  // .png path
}

// DefaultOptions returns the options used when the user specifies nothing.
func DefaultOptions() Options {
	return Options{Size: DefaultSize, Color: DefaultColor, Output: DefaultOutput}
}

// ValidationError reports invalid user input for one field.
type ValidationError struct {
	Field string // flag name: text, size, color, hole, hole-percent, output
	Msg   string
}

func (e *ValidationError) Error() string { return fmt.Sprintf("invalid %s: %s", e.Field, e.Msg) }

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Msg: fmt.Sprintf(format, args...)}
}

var schemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)

// ValidateText checks the payload: non-empty, valid UTF-8, no control
// characters, within QR capacity, and a well-formed URL when it looks like one.
func ValidateText(s string) error {
	if strings.TrimSpace(s) == "" {
		return invalid("text", "must not be empty or whitespace only")
	}
	if !utf8.ValidString(s) {
		return invalid("text", "must be valid UTF-8")
	}
	if len(s) > MaxTextBytes {
		return invalid("text", "%d bytes exceeds the QR maximum of %d", len(s), MaxTextBytes)
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return invalid("text", "contains control character U+%04X", r)
		}
	}
	if schemeRe.MatchString(s) {
		if strings.IndexFunc(s, unicode.IsSpace) >= 0 {
			return invalid("text", "looks like a URL but contains whitespace")
		}
		u, err := url.Parse(s)
		if err != nil {
			return invalid("text", "malformed URL: %v", err)
		}
		if (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() == "" {
			return invalid("text", "%s URL has no host", u.Scheme)
		}
	}
	return nil
}

// ValidateSize checks the pixel size range. The lower bound that depends on
// the text length is enforced by Build.
func ValidateSize(n int) error {
	if n < 1 || n > MaxSize {
		return invalid("size", "%d is out of range, must be 1-%d pixels", n, MaxSize)
	}
	return nil
}

var hexRe = regexp.MustCompile(`^#?([0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$`)

// ParseColor parses #RGB or #RRGGBB ('#' optional because shells treat an
// unquoted '#' as a comment) and rejects colors too light to scan on white.
func ParseColor(s string) (color.RGBA, error) {
	m := hexRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return color.RGBA{}, invalid("color", "%q is not a hex color, use #RGB or #RRGGBB", s)
	}
	h := m[1]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	v, _ := strconv.ParseUint(h, 16, 32)
	c := color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
	if ratio := contrastOnWhite(c); ratio < MinContrast {
		return color.RGBA{}, invalid("color", "contrast against white is %.1f:1, needs at least %.1f:1 to scan", ratio, MinContrast)
	}
	return c, nil
}

func contrastOnWhite(c color.RGBA) float64 {
	lin := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	l := 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
	return 1.05 / (l + 0.05)
}

// ParseShape accepts "", "none", "circle" and "square" (case-insensitive).
func ParseShape(s string) (Shape, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none":
		return ShapeNone, nil
	case "circle":
		return ShapeCircle, nil
	case "square":
		return ShapeSquare, nil
	}
	return ShapeNone, invalid("hole", "%q is not one of none, circle, square", s)
}

// ValidateHole checks shape and percentage together.
func ValidateHole(shape Shape, pct float64) error {
	if math.IsNaN(pct) || math.IsInf(pct, 0) {
		return invalid("hole-percent", "must be a finite number")
	}
	switch shape {
	case ShapeNone:
		if pct != 0 {
			return invalid("hole-percent", "set together with a hole shape (circle or square)")
		}
	case ShapeCircle, ShapeSquare:
		if pct <= 0 || pct > MaxHolePct {
			return invalid("hole-percent", "%g is out of range, must be above 0 and at most %g", pct, MaxHolePct)
		}
	default:
		return invalid("hole", "%q is not one of none, circle, square", string(shape))
	}
	return nil
}

// ValidateOutput checks the output path syntax and parent directory. Whether
// the file already exists is decided by the caller (see OutputExists).
func ValidateOutput(path string) error {
	if path == "" {
		return invalid("output", "must not be empty")
	}
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		return invalid("output", "%q must end in .png", path)
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return invalid("output", "%q is a directory", path)
	}
	dir := filepath.Dir(path)
	fi, err := os.Stat(dir)
	if err != nil {
		return invalid("output", "directory %q does not exist", dir)
	}
	if !fi.IsDir() {
		return invalid("output", "%q is not a directory", dir)
	}
	return nil
}

// OutputExists reports whether path already exists.
func OutputExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Validate checks every field that does not depend on the encoded QR size.
func (o Options) Validate() error {
	if err := ValidateText(o.Text); err != nil {
		return err
	}
	if err := ValidateSize(o.Size); err != nil {
		return err
	}
	if _, err := ParseColor(o.Color); err != nil {
		return err
	}
	if err := ValidateHole(o.HoleShape, o.HolePercent); err != nil {
		return err
	}
	return ValidateOutput(o.Output)
}
