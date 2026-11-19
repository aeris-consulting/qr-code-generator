// Package prompt implements the interactive mode: it asks for each option,
// validates every answer with the same rules as the command-line flags, shows
// a summary and asks for confirmation.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/aeris-consulting/qr-code-generator/internal/qrgen"
)

// ErrAborted is returned when the user declines the final confirmation.
var ErrAborted = errors.New("aborted, nothing was written")

// Field names, matching the flag names, used to mark values already given.
const (
	FieldText        = "text"
	FieldSize        = "size"
	FieldColor       = "color"
	FieldHole        = "hole"
	FieldHolePercent = "hole-percent"
	FieldOutput      = "output"
)

// Session reads answers from In and writes prompts to Out.
type Session struct {
	In  *bufio.Reader
	Out io.Writer
}

// New creates a Session over r and w.
func New(r io.Reader, w io.Writer) *Session {
	return &Session{In: bufio.NewReader(r), Out: w}
}

// Run asks for every field not listed in given (those keep their value from
// opts), shows the summary and asks for confirmation. When assumeYes is true
// the confirmation is skipped. It returns the validated plan.
func (s *Session) Run(opts qrgen.Options, given map[string]bool, assumeYes bool) (*qrgen.Plan, error) {
	// ask re-prompts until apply accepts the answer. Size and hole answers are
	// checked with buildCheck, i.e. against the actual text and each other.
	ask := func(field, label, def string, apply func(*qrgen.Options, string) error) error {
		if given[field] {
			return nil
		}
		for {
			ans, err := s.line(label, def)
			if err != nil {
				return err
			}
			cand := opts
			if err := apply(&cand, ans); err != nil {
				fmt.Fprintf(s.Out, "  ✗ %v\n", err)
				continue
			}
			opts = cand
			return nil
		}
	}

	// The text must be valid before anything that depends on it is checked.
	if err := ask(FieldText, "Text or URL", "", func(o *qrgen.Options, a string) error {
		o.Text = a
		if err := qrgen.ValidateText(a); err != nil {
			return err
		}
		return buildCheck(*o) // also checks it against any size or hole already given
	}); err != nil {
		return nil, err
	}
	if err := ask(FieldSize, "Size in pixels", strconv.Itoa(opts.Size), func(o *qrgen.Options, a string) error {
		n, err := strconv.Atoi(a)
		if err != nil {
			return fmt.Errorf("invalid size: %q is not a whole number", a)
		}
		o.Size = n
		return buildCheck(*o)
	}); err != nil {
		return nil, err
	}
	if err := ask(FieldColor, "Color (hex)", opts.Color, func(o *qrgen.Options, a string) error {
		o.Color = a
		_, err := qrgen.ParseColor(a)
		return err
	}); err != nil {
		return nil, err
	}
	shapeDef := "none"
	if opts.HoleShape != qrgen.ShapeNone {
		shapeDef = string(opts.HoleShape)
	}
	if err := ask(FieldHole, "Empty center shape (none/circle/square)", shapeDef, func(o *qrgen.Options, a string) error {
		sh, err := qrgen.ParseShape(a)
		o.HoleShape = sh
		if sh == qrgen.ShapeNone {
			o.HolePercent = 0
		}
		return err
	}); err != nil {
		return nil, err
	}
	if opts.HoleShape != qrgen.ShapeNone {
		def := ""
		if opts.HolePercent > 0 {
			def = strconv.FormatFloat(opts.HolePercent, 'f', -1, 64)
		}
		label := fmt.Sprintf("Empty center, percent of code area (max %g)", qrgen.MaxHolePct)
		if err := ask(FieldHolePercent, label, def, func(o *qrgen.Options, a string) error {
			f, err := strconv.ParseFloat(a, 64)
			if err != nil {
				return fmt.Errorf("invalid hole-percent: %q is not a number", a)
			}
			o.HolePercent = f
			return buildCheck(*o)
		}); err != nil {
			return nil, err
		}
	}
	if err := ask(FieldOutput, "Output file", opts.Output, func(o *qrgen.Options, a string) error {
		o.Output = a
		return qrgen.ValidateOutput(a)
	}); err != nil {
		return nil, err
	}

	plan, err := qrgen.Build(opts)
	if err != nil {
		return nil, err
	}
	if assumeYes {
		return plan, nil
	}
	fmt.Fprintf(s.Out, "\nSummary:\n%s\n", plan.Summary())
	ok, err := s.confirm("Generate?")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrAborted
	}
	return plan, nil
}

// buildCheck runs the full validation and layout, but only reports errors
// for the fields answered so far (a hole shape without percent, or the
// reverse, is not an error yet).
func buildCheck(o qrgen.Options) error {
	if o.HoleShape == qrgen.ShapeNone || o.HolePercent == 0 {
		o.HoleShape, o.HolePercent = qrgen.ShapeNone, 0 // not answered yet
	}
	_, err := qrgen.Build(o)
	return err
}

// line prints a prompt and returns the trimmed answer, or def when empty.
func (s *Session) line(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(s.Out, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(s.Out, "%s: ", label)
	}
	text, err := s.In.ReadString('\n')
	if err != nil && (err != io.EOF || text == "") {
		if err == io.EOF {
			return "", errors.New("input closed before all questions were answered")
		}
		return "", err
	}
	text = strings.TrimRight(text, "\r\n")
	if strings.TrimSpace(text) == "" {
		return def, nil
	}
	return text, nil
}

// confirm asks a yes/no question; anything but y/yes means no.
func (s *Session) confirm(label string) (bool, error) {
	fmt.Fprintf(s.Out, "%s [y/N]: ", label)
	text, err := s.In.ReadString('\n')
	if err != nil && text == "" {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}
