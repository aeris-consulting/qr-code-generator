// Command qrgen generates QR codes as PNG images, with optional color and an
// empty central area (circle or square) for adding a logo later.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aeris-consulting/qr-code-generator/internal/prompt"
	"github.com/aeris-consulting/qr-code-generator/internal/qrgen"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `qrgen - generate QR codes as PNG images

Usage:
  qrgen [flags] TEXT         flags must come before TEXT; quote TEXT if it has spaces
  qrgen [flags] < file       read the text from stdin
  qrgen -i                   interactive mode (also the default when run in a
                             terminal without TEXT)

Flags:
`

const examples = `
Examples:
  qrgen -size 600 -color 0a3d91 -o site.png https://example.com
  qrgen -hole circle -hole-percent 20 -o logo-ready.png https://example.com
  echo "hello" | qrgen -o hello.png

The hole is a percentage of the QR code area (without the quiet zone), at most 20.
Any hole forces error-correction level H. Unless -no-verify is given, the
generated image is decoded again and nothing is written if it does not scan.
Exit codes: 0 ok, 1 failure or aborted, 2 invalid input.
`

func main() {
	st, err := os.Stdin.Stat()
	tty := err == nil && st.Mode()&os.ModeCharDevice != 0
	os.Exit(run(os.Args[1:], os.Stdin, tty, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdinTTY bool, stdout, stderr io.Writer) int {
	def := qrgen.DefaultOptions()
	fs := flag.NewFlagSet("qrgen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		textFlag    = fs.String("text", "", "text or URL to encode (alternative to the positional TEXT)")
		size        = fs.Int("size", def.Size, "image width and height in pixels")
		colorFlag   = fs.String("color", def.Color, "code color as #RGB or #RRGGBB")
		hole        = fs.String("hole", "none", "empty center shape: none, circle or square")
		holePct     = fs.Float64("hole-percent", 0, "empty center as percent of the code area, above 0 and at most 20")
		output      string
		force       = fs.Bool("force", false, "overwrite the output file if it exists")
		yes         = fs.Bool("yes", false, "interactive mode: skip the final confirmation")
		noVerify    = fs.Bool("no-verify", false, "skip decoding the result to check it scans")
		interactive = fs.Bool("i", false, "interactive mode")
		showVersion = fs.Bool("version", false, "print the version and exit")
	)
	fs.StringVar(&output, "o", def.Output, "output PNG file")
	fs.StringVar(&output, "output", def.Output, "same as -o")
	fs.BoolVar(interactive, "interactive", false, "same as -i")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
		fmt.Fprint(stderr, examples)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "qrgen", version)
		return 0
	}

	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "text":
			given[prompt.FieldText] = true
		case "size":
			given[prompt.FieldSize] = true
		case "color":
			given[prompt.FieldColor] = true
		case "hole":
			given[prompt.FieldHole] = true
		case "hole-percent":
			given[prompt.FieldHolePercent] = true
		case "o", "output":
			given[prompt.FieldOutput] = true
		}
	})

	if fs.NArg() > 1 {
		return fail(stderr, 2, fmt.Errorf("unexpected arguments after the text: %q (flags must come before the text; quote text that contains spaces)", strings.Join(fs.Args()[1:], " ")))
	}
	text := *textFlag
	if fs.NArg() == 1 {
		if given[prompt.FieldText] {
			return fail(stderr, 2, errors.New("give the text either as an argument or with -text, not both"))
		}
		text = fs.Arg(0)
		given[prompt.FieldText] = true
	}

	shape, err := qrgen.ParseShape(*hole)
	if err != nil {
		return fail(stderr, 2, err)
	}
	opts := qrgen.Options{Text: text, Size: *size, Color: *colorFlag, HoleShape: shape, HolePercent: *holePct, Output: output}

	useInteractive := *interactive || (!given[prompt.FieldText] && stdinTTY)
	var plan *qrgen.Plan
	if useInteractive {
		if err := checkGiven(opts, given); err != nil {
			return fail(stderr, 2, err)
		}
		if *yes && qrgen.OutputExists(opts.Output) && !*force && given[prompt.FieldOutput] {
			return fail(stderr, 1, fmt.Errorf("%s already exists, use -force to overwrite", opts.Output))
		}
		plan, err = prompt.New(stdin, stdout).Run(opts, given, *yes)
	} else {
		if !given[prompt.FieldText] {
			opts.Text, err = readStdin(stdin)
			if err != nil {
				return fail(stderr, 1, err)
			}
		}
		plan, err = qrgen.Build(opts)
	}
	if err != nil {
		return failErr(stderr, err)
	}

	// In interactive mode the summary already warned about an overwrite and the
	// user confirmed it; with -yes only -force allows it.
	overwrite := *force || (useInteractive && !*yes)
	if err := plan.Generate(!*noVerify, overwrite); err != nil {
		return fail(stderr, 1, err)
	}
	fmt.Fprintf(stdout, "Wrote %s (%d×%d px)\n", plan.Opts.Output, plan.Opts.Size, plan.Opts.Size)
	return 0
}

// checkGiven validates the values passed as flags before any prompt, so a bad
// flag is reported at once instead of looping inside a prompt.
func checkGiven(o qrgen.Options, given map[string]bool) error {
	if given[prompt.FieldText] {
		if err := qrgen.ValidateText(o.Text); err != nil {
			return err
		}
	}
	if given[prompt.FieldSize] {
		if err := qrgen.ValidateSize(o.Size); err != nil {
			return err
		}
	}
	if given[prompt.FieldColor] {
		if _, err := qrgen.ParseColor(o.Color); err != nil {
			return err
		}
	}
	if given[prompt.FieldHolePercent] && given[prompt.FieldHole] {
		if err := qrgen.ValidateHole(o.HoleShape, o.HolePercent); err != nil {
			return err
		}
	}
	if given[prompt.FieldOutput] {
		if err := qrgen.ValidateOutput(o.Output); err != nil {
			return err
		}
	}
	return nil
}

// readStdin reads the text from a pipe, bounded to the QR capacity, and drops
// one trailing newline (what echo and most editors add).
func readStdin(r io.Reader) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, qrgen.MaxTextBytes+3))
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}
	s := string(b)
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	return s, nil
}

func failErr(w io.Writer, err error) int {
	var ve *qrgen.ValidationError
	if errors.As(err, &ve) {
		return fail(w, 2, err)
	}
	return fail(w, 1, err)
}

func fail(w io.Writer, code int, err error) int {
	fmt.Fprintln(w, "qrgen:", err)
	return code
}
