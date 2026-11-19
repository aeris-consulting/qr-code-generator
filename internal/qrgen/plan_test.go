package qrgen

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleURL = "https://example.com/some/path?with=query&and=more&params=1234567890"

func opts(t *testing.T, mod func(*Options)) Options {
	t.Helper()
	o := DefaultOptions()
	o.Text = sampleURL
	o.Output = filepath.Join(t.TempDir(), "qr.png")
	if mod != nil {
		mod(&o)
	}
	return o
}

func TestBuildRejectsInvalidAndImpossible(t *testing.T) {
	cases := []struct {
		name  string
		mod   func(*Options)
		field string
	}{
		{"empty text", func(o *Options) { o.Text = "" }, "text"},
		{"size below module count", func(o *Options) { o.Size = 20 }, "size"},
		{"bad color", func(o *Options) { o.Color = "nope" }, "color"},
		{"hole without percent", func(o *Options) { o.HoleShape = ShapeCircle }, "hole-percent"},
		{"percent without hole", func(o *Options) { o.HolePercent = 10 }, "hole-percent"},
		{"hole over cap", func(o *Options) { o.HoleShape, o.HolePercent = ShapeSquare, 31 }, "hole-percent"},
		{"hole hits finders on tiny code", func(o *Options) { o.Text, o.HoleShape, o.HolePercent = "hi", ShapeSquare, 20 }, "hole-percent"},
		{"text too long for level H", func(o *Options) {
			o.Text, o.HoleShape, o.HolePercent = strings.Repeat("a", 1500), ShapeCircle, 5
		}, "text"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Build(opts(t, c.mod))
			if err == nil {
				t.Fatal("Build accepted invalid options")
			}
			if got := fieldOf(err); got != c.field {
				t.Errorf("field = %q, want %q (%v)", got, c.field, err)
			}
		})
	}
}

func TestRenderGeometry(t *testing.T) {
	for _, size := range []int{300, 512, 777} {
		o := opts(t, func(o *Options) { o.Size = size; o.Color = "#102030" })
		p, err := Build(o)
		if err != nil {
			t.Fatal(err)
		}
		img := p.Render()
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Fatalf("size %d: got %dx%d", size, b.Dx(), b.Dy())
		}
		if p.Scale*p.Total > size {
			t.Errorf("size %d: code %d px wider than image", size, p.Scale*p.Total)
		}
		// Corner is quiet zone (white); the first finder module is the code color.
		if r, g, b, _ := img.At(0, 0).RGBA(); r>>8 != 255 || g>>8 != 255 || b>>8 != 255 {
			t.Errorf("size %d: corner not white", size)
		}
		x := p.Margin + 4*p.Scale + p.Scale/2 // quiet zone is 4 modules
		if r, g, b, _ := img.At(x, x).RGBA(); r>>8 != 0x10 || g>>8 != 0x20 || b>>8 != 0x30 {
			t.Errorf("size %d: finder module not #102030", size)
		}
	}
}

func TestHoleIsEmptyAndRoundTrips(t *testing.T) {
	texts := []string{sampleURL, "héllo 日本語 🙂 " + strings.Repeat("x", 60), strings.Repeat("0123456789", 20)}
	for _, shape := range []Shape{ShapeCircle, ShapeSquare} {
		for _, pct := range []float64{5, 10, 15} {
			for _, text := range texts {
				o := opts(t, func(o *Options) { o.Text, o.Size, o.HoleShape, o.HolePercent = text, 700, shape, pct })
				p, err := Build(o)
				if err != nil {
					t.Fatalf("%s %v%%: %v", shape, pct, err)
				}
				img := p.Render()
				// Center pixel is inside the hole, so it must be white.
				if r, _, _, _ := img.At(350, 350).RGBA(); r>>8 != 255 {
					t.Errorf("%s %v%%: center not empty", shape, pct)
				}
				if p.HoleModules == 0 {
					t.Errorf("%s %v%%: no modules cleared", shape, pct)
				}
				if err := p.Verify(img); err != nil {
					t.Errorf("%s %v%% len=%d: %v", shape, pct, len(text), err)
				}
			}
		}
	}
}

func TestHoleAreaMatchesRequest(t *testing.T) {
	for _, shape := range []Shape{ShapeCircle, ShapeSquare} {
		p, err := Build(opts(t, func(o *Options) {
			o.Text, o.Size, o.HoleShape, o.HolePercent = strings.Repeat("0123456789", 30), 1000, shape, 20
		}))
		if err != nil {
			t.Fatal(err)
		}
		got := float64(p.HoleModules) / float64(p.Modules*p.Modules) * 100
		if got < 18 || got > 22 {
			t.Errorf("%s: cleared %.1f%% of the code area, want about 20%%", shape, got)
		}
	}
}

func TestNoHoleRoundTrips(t *testing.T) {
	p, err := Build(opts(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Verify(p.Render()); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsOversizedHole(t *testing.T) {
	// At the 20% cap a typical URL cannot survive error correction, and Verify
	// must say so instead of letting an unreadable code be written.
	p, err := Build(opts(t, func(o *Options) { o.Size, o.HoleShape, o.HolePercent = 700, ShapeSquare, 20 }))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Verify(p.Render()); err == nil || !strings.Contains(err.Error(), "self-verify failed") {
		t.Fatalf("Verify = %v, want self-verify failure", err)
	}
}

func TestGenerateWritesPNGAndProtectsExistingFile(t *testing.T) {
	o := opts(t, func(o *Options) { o.Size = 400 })
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Generate(true, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(o.Output)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 400 || cfg.Height != 400 {
		t.Fatalf("output PNG: %+v, %v", cfg, err)
	}
	if err := p.Generate(true, false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second Generate without overwrite = %v, want already exists", err)
	}
	if err := p.Generate(true, true); err != nil {
		t.Fatalf("Generate with overwrite: %v", err)
	}
}

func TestFailedVerifyWritesNothing(t *testing.T) {
	o := opts(t, func(o *Options) { o.Size, o.HoleShape, o.HolePercent = 700, ShapeSquare, 20 })
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Generate(true, false); err == nil {
		t.Fatal("Generate succeeded with an unreadable code")
	}
	if _, err := os.Stat(o.Output); err == nil {
		t.Error("output file exists after failed verify")
	}
}

func TestSummaryMentionsKeyFacts(t *testing.T) {
	p, err := Build(opts(t, func(o *Options) { o.HoleShape, o.HolePercent = ShapeCircle, 10 }))
	if err != nil {
		t.Fatal(err)
	}
	s := p.Summary()
	for _, want := range []string{"Text:", "512 px", "#000000", "circle, 10%", "Error correction:", "H"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
}

func TestTransparentBackground(t *testing.T) {
	o := opts(t, func(o *Options) {
		o.Size, o.Color, o.Transparent = 400, "#102030", true
		o.HoleShape, o.HolePercent = ShapeCircle, 10
	})
	p, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	img := p.Render()
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Errorf("corner alpha = %d, want 0", a)
	}
	if _, _, _, a := img.At(200, 200).RGBA(); a != 0 {
		t.Errorf("hole alpha = %d, want 0", a)
	}
	x := p.Margin + 4*p.Scale + p.Scale/2
	if r, g, b, a := img.At(x, x).RGBA(); a != 0xffff || r>>8 != 0x10 || g>>8 != 0x20 || b>>8 != 0x30 {
		t.Errorf("code pixel not opaque #102030")
	}
	if err := p.Verify(img); err != nil {
		t.Errorf("verify on transparent image: %v", err)
	}
	if err := p.Generate(true, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(o.Output)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := decoded.At(0, 0).RGBA(); a != 0 {
		t.Errorf("written PNG corner alpha = %d, want 0", a)
	}
	if !strings.Contains(p.Summary(), "transparent") {
		t.Error("summary does not mention the transparent background")
	}
}
