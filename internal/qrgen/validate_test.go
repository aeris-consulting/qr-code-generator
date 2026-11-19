package qrgen

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fieldOf(err error) string {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve.Field
	}
	return ""
}

func TestValidateText(t *testing.T) {
	tests := []struct {
		name, in string
		ok       bool
	}{
		{"plain", "hello world", true},
		{"unicode", "héllo 日本語 🙂", true},
		{"multiline", "a\nb\r\nc\td", true},
		{"https url", "https://example.com/a?b=c#d", true},
		{"mailto is plain text", "mailto:a@b.c", true},
		{"custom scheme", "myapp://open/thing", true},
		{"empty", "", false},
		{"whitespace only", " \n\t", false},
		{"invalid utf8", "abc\xff\xfe", false},
		{"NUL", "a\x00b", false},
		{"control", "a\x1bb", false},
		{"too long", strings.Repeat("a", MaxTextBytes+1), false},
		{"max length", strings.Repeat("a", MaxTextBytes), true},
		{"http without host", "http:///path", false},
		{"https without host", "https://", false},
		{"url with space", "https://example.com/a b", false},
		{"bad url escape", "https://example.com/%zz", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateText(tc.in)
			if (err == nil) != tc.ok {
				t.Fatalf("ValidateText(%q) = %v, want ok=%v", tc.in, err, tc.ok)
			}
			if err != nil && fieldOf(err) != "text" {
				t.Errorf("field = %q, want text", fieldOf(err))
			}
		})
	}
}

func TestValidateSize(t *testing.T) {
	for n, ok := range map[int]bool{-1: false, 0: false, 1: true, 512: true, MaxSize: true, MaxSize + 1: false} {
		if err := ValidateSize(n); (err == nil) != ok {
			t.Errorf("ValidateSize(%d) = %v, want ok=%v", n, err, ok)
		}
	}
}

func TestParseColor(t *testing.T) {
	good := map[string][3]uint8{
		"#000000": {0, 0, 0}, "000": {0, 0, 0}, "#F00": {255, 0, 0},
		"0a3d91": {0x0a, 0x3d, 0x91}, "#0A3D91": {0x0a, 0x3d, 0x91}, " #123 ": {0x11, 0x22, 0x33},
	}
	for in, want := range good {
		c, err := ParseColor(in)
		if err != nil {
			t.Errorf("ParseColor(%q) error: %v", in, err)
			continue
		}
		if got := [3]uint8{c.R, c.G, c.B}; got != want || c.A != 255 {
			t.Errorf("ParseColor(%q) = %v, want %v", in, got, want)
		}
	}
	for _, in := range []string{"", "#", "red", "#12", "#1234", "#12345", "#1234567", "#GGGGGG", "##000", "#ffffff", "#ffff00", "#cccccc"} {
		if _, err := ParseColor(in); err == nil {
			t.Errorf("ParseColor(%q) accepted, want error", in)
		} else if fieldOf(err) != "color" {
			t.Errorf("ParseColor(%q) field = %q", in, fieldOf(err))
		}
	}
}

func TestParseShapeAndHole(t *testing.T) {
	for in, want := range map[string]Shape{"": ShapeNone, "none": ShapeNone, " Circle ": ShapeCircle, "SQUARE": ShapeSquare} {
		if got, err := ParseShape(in); err != nil || got != want {
			t.Errorf("ParseShape(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseShape("triangle"); err == nil {
		t.Error("ParseShape(triangle) accepted")
	}
	nan := func() float64 { var z float64; return z / z }()
	cases := []struct {
		shape Shape
		pct   float64
		ok    bool
	}{
		{ShapeNone, 0, true}, {ShapeNone, 10, false},
		{ShapeCircle, 20, true}, {ShapeSquare, MaxHolePct, true}, {ShapeCircle, 0.5, true},
		{ShapeCircle, 0, false}, {ShapeCircle, -5, false}, {ShapeSquare, MaxHolePct + 0.1, false},
		{ShapeCircle, nan, false}, {Shape("star"), 10, false},
	}
	for _, c := range cases {
		if err := ValidateHole(c.shape, c.pct); (err == nil) != c.ok {
			t.Errorf("ValidateHole(%q, %v) = %v, want ok=%v", c.shape, c.pct, err, c.ok)
		}
	}
}

func TestValidateOutput(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		ok   bool
	}{
		{filepath.Join(dir, "a.png"), true},
		{filepath.Join(dir, "A.PNG"), true},
		{"", false},
		{filepath.Join(dir, "a.jpg"), false},
		{filepath.Join(dir, "noext"), false},
		{filepath.Join(dir, "missing", "a.png"), false},
		{filepath.Join(file, "a.png"), false}, // parent is a file
		{filepath.Join(dir, "d.png"), true},
	}
	if err := os.Mkdir(filepath.Join(dir, "dir.png"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		path string
		ok   bool
	}{filepath.Join(dir, "dir.png"), false}) // existing directory
	for _, c := range cases {
		if err := ValidateOutput(c.path); (err == nil) != c.ok {
			t.Errorf("ValidateOutput(%q) = %v, want ok=%v", c.path, err, c.ok)
		}
	}
}
