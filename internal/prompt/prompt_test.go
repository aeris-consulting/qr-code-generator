package prompt

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aeris-consulting/qr-code-generator/internal/qrgen"
)

func run(t *testing.T, input string, opts qrgen.Options, given map[string]bool, yes bool) (*qrgen.Plan, string, error) {
	t.Helper()
	var out bytes.Buffer
	plan, err := New(strings.NewReader(input), &out).Run(opts, given, yes)
	return plan, out.String(), err
}

func base(t *testing.T) qrgen.Options {
	o := qrgen.DefaultOptions()
	o.Output = filepath.Join(t.TempDir(), "qr.png")
	return o
}

func TestHappyPathWithDefaults(t *testing.T) {
	o := base(t)
	// text, size, color, hole shape, output all answered; empty = default.
	plan, out, err := run(t, "https://example.com\n\n\n\n\ny\n", o, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Opts.Text != "https://example.com" || plan.Opts.Size != qrgen.DefaultSize || plan.Opts.HoleShape != qrgen.ShapeNone {
		t.Errorf("unexpected options: %+v", plan.Opts)
	}
	for _, want := range []string{"Summary:", "https://example.com", "Generate? [y/N]"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestHoleQuestionsAndValidationRetries(t *testing.T) {
	o := base(t)
	input := strings.Join([]string{
		"",                    // empty text -> rejected
		"https://example.com", // ok
		"abc",                 // size not a number
		"10",                  // size too small for the code
		"600",                 // ok
		"red",                 // bad color
		"#102030",             // ok
		"triangle",            // bad shape
		"circle",              // ok
		"45",                  // over cap
		"10",                  // ok
		"out.txt",             // wrong extension
		"",                    // default output
		"yes",
	}, "\n") + "\n"
	plan, out, err := run(t, input, o, nil, false)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := plan.Opts; got.Size != 600 || got.Color != "#102030" || got.HoleShape != qrgen.ShapeCircle || got.HolePercent != 10 {
		t.Errorf("options = %+v", got)
	}
	if n := strings.Count(out, "✗"); n != 7 {
		t.Errorf("%d rejected answers shown, want 7:\n%s", n, out)
	}
}

func TestGivenFieldsAreNotAsked(t *testing.T) {
	o := base(t)
	o.Text, o.Size = "hello", 400
	given := map[string]bool{FieldText: true, FieldSize: true, FieldOutput: true, FieldColor: true}
	// Only the hole shape is asked; answer none, then confirm.
	plan, out, err := run(t, "none\ny\n", o, given, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Opts.Size != 400 || strings.Contains(out, "Size in pixels") || strings.Contains(out, "Text or URL") {
		t.Errorf("given fields were asked again:\n%s", out)
	}
}

func TestDeclineAndEOFWriteNothing(t *testing.T) {
	for name, tail := range map[string]string{"no": "n\n", "empty": "\n", "other": "maybe\n", "eof": ""} {
		_, _, err := run(t, "hello\n\n\n\n\n"+tail, base(t), nil, false)
		if !errors.Is(err, ErrAborted) {
			t.Errorf("%s: err = %v, want ErrAborted", name, err)
		}
	}
}

func TestEOFMidQuestions(t *testing.T) {
	_, _, err := run(t, "hello\n", base(t), nil, false)
	if err == nil || errors.Is(err, ErrAborted) || !strings.Contains(err.Error(), "input closed") {
		t.Errorf("err = %v, want input closed", err)
	}
}

func TestAssumeYesSkipsSummary(t *testing.T) {
	plan, out, err := run(t, "hello\n\n\n\n\n", base(t), nil, true)
	if err != nil || plan == nil {
		t.Fatalf("%v", err)
	}
	if strings.Contains(out, "Summary:") {
		t.Errorf("summary shown with assumeYes:\n%s", out)
	}
}

func TestShapeNoneDropsGivenPercent(t *testing.T) {
	o := base(t)
	o.HolePercent = 10
	given := map[string]bool{FieldHolePercent: true}
	plan, _, err := run(t, "hello\n\n\nnone\n\ny\n", o, given, false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Opts.HoleShape != qrgen.ShapeNone || plan.Opts.HolePercent != 0 {
		t.Errorf("options = %+v", plan.Opts)
	}
}
