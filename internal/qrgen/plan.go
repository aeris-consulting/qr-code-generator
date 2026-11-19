package qrgen

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/makiuchi-d/gozxing"
	gozxingqr "github.com/makiuchi-d/gozxing/qrcode"
	"github.com/skip2/go-qrcode"
)

// finderSize is the side, in modules, of a finder pattern (the three big
// squares in the corners). They are never allowed to be cleared.
const finderSize = 7

// Plan is a fully validated and encoded QR code, ready to render.
type Plan struct {
	Opts        Options
	Color       color.RGBA
	Level       qrcode.RecoveryLevel
	Version     int // QR version, 1-40
	Modules     int // modules per side of the symbol, without quiet zone
	Total       int // modules per side including the quiet zone
	Scale       int // pixels per module
	Margin      int // blank pixels left of and above the code
	HoleModules int // number of modules cleared by the hole

	bitmap  [][]bool // Total x Total, true = dark, includes quiet zone
	cleared [][]bool // same shape, true = inside the hole
}

// Build validates o, encodes the text and computes the layout and hole.
// Any hole forces error-correction level H so the code stays readable.
func Build(o Options) (*Plan, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	col, _ := ParseColor(o.Color) // already validated

	level := qrcode.Medium
	if o.HoleShape != ShapeNone {
		level = qrcode.Highest
	}
	q, err := qrcode.New(o.Text, level)
	if err != nil {
		if o.HoleShape != ShapeNone {
			return nil, invalid("text", "%d bytes do not fit in a QR code with a hole (error-correction level H holds at most 1273 bytes); shorten the text or remove the hole", len(o.Text))
		}
		return nil, invalid("text", "%d bytes do not fit in a QR code", len(o.Text))
	}
	bm := q.Bitmap()

	p := &Plan{
		Opts:    o,
		Color:   col,
		Level:   level,
		Version: q.VersionNumber,
		Modules: 17 + 4*q.VersionNumber,
		Total:   len(bm),
		bitmap:  bm,
	}
	if o.Size < p.Total {
		return nil, invalid("size", "%d px is too small, this text needs at least %d px (%d modules including quiet zone)", o.Size, p.Total, p.Total)
	}
	p.Scale = o.Size / p.Total
	p.Margin = (o.Size - p.Scale*p.Total) / 2

	if err := p.computeHole(); err != nil {
		return nil, err
	}
	return p, nil
}

// computeHole clears every module whose center lies inside the hole shape,
// centered on the symbol, covering HolePercent of the symbol area.
func (p *Plan) computeHole() error {
	if p.Opts.HoleShape == ShapeNone {
		return nil
	}
	n, m := p.Total, float64(p.Modules)
	frac := p.Opts.HolePercent / 100
	c := float64(n) / 2
	half := math.Sqrt(frac) * m / 2       // square: half side
	radius := m * math.Sqrt(frac/math.Pi) // circle: radius

	inside := func(x, y float64) bool {
		dx, dy := math.Abs(x-c), math.Abs(y-c)
		if p.Opts.HoleShape == ShapeSquare {
			return dx <= half && dy <= half
		}
		return math.Hypot(dx, dy) <= radius
	}

	quiet := (n - p.Modules) / 2
	p.cleared = make([][]bool, n)
	for r := range p.cleared {
		p.cleared[r] = make([]bool, n)
		for col := 0; col < n; col++ {
			if !inside(float64(col)+0.5, float64(r)+0.5) {
				continue
			}
			p.cleared[r][col] = true
			p.HoleModules++
			sr, sc := r-quiet, col-quiet
			far := p.Modules - finderSize
			if (sr < finderSize && sc < finderSize) || (sr < finderSize && sc >= far) || (sr >= far && sc < finderSize) {
				return invalid("hole-percent", "%g%% would damage the finder patterns of this %d×%d module code; use a smaller percentage or longer text", p.Opts.HolePercent, p.Modules, p.Modules)
			}
		}
	}
	return nil
}

// Render draws the code on a Size×Size image with a white (or, with
// Transparent, fully transparent) background. Hole modules are left empty.
// Using a 2-color palette keeps large images at one byte per pixel.
func (p *Plan) Render() *image.Paletted {
	size := p.Opts.Size
	var bg color.Color = color.White
	if p.Opts.Transparent {
		bg = color.NRGBA{}
	}
	img := image.NewPaletted(image.Rect(0, 0, size, size), color.Palette{bg, p.Color})
	for r := 0; r < p.Total; r++ {
		for c := 0; c < p.Total; c++ {
			if !p.bitmap[r][c] || (p.cleared != nil && p.cleared[r][c]) {
				continue
			}
			x0, y0 := p.Margin+c*p.Scale, p.Margin+r*p.Scale
			for dy := 0; dy < p.Scale; dy++ {
				off := (y0+dy)*img.Stride + x0
				row := img.Pix[off : off+p.Scale]
				for i := range row {
					row[i] = 1
				}
			}
		}
	}
	return img
}

// Verify decodes img with an independent QR reader and checks that it yields
// the original text, so an unreadable code is never written. The reader's
// edge detector is tried first; if it fails, the image is decoded as a clean,
// axis-aligned render (which it always is), because that detector gives up on
// some valid codes. Either way the data must survive error correction.
func (p *Plan) Verify(img image.Image) error {
	// The reader treats transparent pixels as black, so decode as if the
	// image were placed on white. Palette index 0 is always the background.
	if pi, ok := img.(*image.Paletted); ok && p.Opts.Transparent {
		onWhite := *pi
		onWhite.Palette = color.Palette{color.White, p.Color}
		img = &onWhite
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return fmt.Errorf("self-verify: %w", err)
	}
	var res *gozxing.Result
	for _, hint := range []gozxing.DecodeHintType{gozxing.DecodeHintType_TRY_HARDER, gozxing.DecodeHintType_PURE_BARCODE} {
		res, err = gozxingqr.NewQRCodeReader().Decode(bmp, map[gozxing.DecodeHintType]interface{}{hint: true})
		if err == nil {
			break
		}
	}
	if err != nil {
		return errors.New("self-verify failed: the generated code cannot be decoded; " + p.verifyAdvice())
	}
	if res.GetText() != p.Opts.Text {
		return errors.New("self-verify failed: the generated code decodes to different text; " + p.verifyAdvice())
	}
	return nil
}

func (p *Plan) verifyAdvice() string {
	return "try a smaller hole percentage, a larger size, a darker color or shorter text (--no-verify skips this check)"
}
