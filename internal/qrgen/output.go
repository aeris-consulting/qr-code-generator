package qrgen

import (
	"fmt"
	"image/png"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/skip2/go-qrcode"
)

// Summary describes what Generate will produce, for confirmation prompts.
func (p *Plan) Summary() string {
	level := map[qrcode.RecoveryLevel]string{qrcode.Medium: "M", qrcode.Highest: "H"}[p.Level]
	hole := "none"
	if p.Opts.HoleShape != ShapeNone {
		hole = fmt.Sprintf("%s, %g%% of the code area (%d modules cleared)", p.Opts.HoleShape, p.Opts.HolePercent, p.HoleModules)
	}
	col, _ := ParseColor(p.Opts.Color)
	out := p.Opts.Output
	if OutputExists(out) {
		out += "  (exists, will be overwritten)"
	}
	rows := [][2]string{
		{"Text", fmt.Sprintf("%s  (%d bytes)", preview(p.Opts.Text, 60), len(p.Opts.Text))},
		{"Size", fmt.Sprintf("%d px  (version %d, %d modules, %d px per module)", p.Opts.Size, p.Version, p.Modules, p.Scale)},
		{"Color", fmt.Sprintf("#%02X%02X%02X  (contrast %.1f:1)", col.R, col.G, col.B, contrastOnWhite(col))},
		{"Background", background(p.Opts.Transparent)},
		{"Hole", hole},
		{"Error correction", level},
		{"Output", out},
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-17s %s\n", r[0]+":", r[1])
	}
	return b.String()
}

func background(transparent bool) string {
	if transparent {
		return "transparent"
	}
	return "white"
}

func preview(s string, max int) string {
	s = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

// Generate renders the plan, optionally verifies it by decoding, and writes
// the PNG. Without overwrite an existing file is never replaced; the check is
// atomic (O_EXCL), so a concurrent writer cannot be clobbered.
func (p *Plan) Generate(verify, overwrite bool) error {
	img := p.Render()
	if verify {
		if err := p.Verify(img); err != nil {
			return err
		}
	}
	flags := os.O_WRONLY | os.O_CREATE
	if overwrite {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(p.Opts.Output, flags, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists, use --force to overwrite", p.Opts.Output)
		}
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		_ = os.Remove(p.Opts.Output) // best effort: do not leave a truncated PNG behind
		return err
	}
	return f.Close()
}
