package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type result struct {
	code        int
	stdout, err string
}

func do(t *testing.T, stdin string, tty bool, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), tty, &out, &errb)
	return result{code, out.String(), errb.String()}
}

func pngSize(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil || cfg.Width != cfg.Height {
		t.Fatalf("%s: %+v %v", path, cfg, err)
	}
	return cfg.Width
}

func TestPositionalTextWritesPNG(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a.png")
	r := do(t, "", false, "-size", "300", "-color", "0a3d91", "-o", out, "https://example.com")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if got := pngSize(t, out); got != 300 {
		t.Errorf("size %d, want 300", got)
	}
}

func TestStdinText(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a.png")
	if r := do(t, "hello from a pipe\n", false, "-o", out); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if r := do(t, "", false, "-o", filepath.Join(t.TempDir(), "b.png")); r.code != 2 {
		t.Errorf("empty stdin: code %d, want 2 (%s)", r.code, r.err)
	}
}

func TestInvalidInputExitsWith2AndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "a.png")
	cases := map[string][]string{
		"bad color":          {"-color", "nope", "-o", out, "x"},
		"low contrast":       {"-color", "#ffff00", "-o", out, "x"},
		"size zero":          {"-size", "0", "-o", out, "x"},
		"size huge":          {"-size", "99999", "-o", out, "x"},
		"size too small":     {"-size", "10", "-o", out, "x"},
		"bad shape":          {"-hole", "star", "-hole-percent", "10", "-o", out, "x"},
		"shape no percent":   {"-hole", "circle", "-o", out, "x"},
		"percent no shape":   {"-hole-percent", "10", "-o", out, "x"},
		"percent over cap":   {"-hole", "circle", "-hole-percent", "31", "-o", out, "x"},
		"not png":            {"-o", filepath.Join(dir, "a.jpg"), "x"},
		"missing dir":        {"-o", filepath.Join(dir, "no", "a.png"), "x"},
		"flags after text":   {"x", "-size", "300", "-o", out},
		"text twice":         {"-text", "a", "-o", out, "b"},
		"url without host":   {"-o", out, "https://"},
		"unknown flag":       {"-nope", "-o", out, "x"},
		"hole too big, tiny": {"-hole", "square", "-hole-percent", "20", "-o", out, "hi"},
	}
	for name, args := range cases {
		if r := do(t, "", false, args...); r.code != 2 {
			t.Errorf("%s: code %d, want 2 (%s)", name, r.code, r.err)
		}
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("output written despite invalid input")
	}
}

func TestExistingFileNeedsForce(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a.png")
	if r := do(t, "", false, "-o", out, "one"); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	if r := do(t, "", false, "-o", out, "two"); r.code != 1 || !strings.Contains(r.err, "already exists") {
		t.Errorf("without -force: %+v", r)
	}
	if r := do(t, "", false, "-force", "-o", out, "two"); r.code != 0 {
		t.Errorf("with -force: %+v", r)
	}
}

func TestUnreadableHoleIsRefusedUnlessNoVerify(t *testing.T) {
	out := filepath.Join(t.TempDir(), "a.png")
	args := []string{"-size", "700", "-hole", "square", "-hole-percent", "20", "-o", out, "https://example.com/some/path?with=query&and=more&params=1234567890"}
	if r := do(t, "", false, args...); r.code != 1 || !strings.Contains(r.err, "self-verify failed") {
		t.Fatalf("%+v", r)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("file written although verify failed")
	}
	if r := do(t, "", false, append([]string{"-no-verify"}, args...)...); r.code != 0 {
		t.Errorf("-no-verify: %+v", r)
	}
}

func TestInteractiveViaTTYAndConfirmation(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "i.png")
	// No text and stdin is a terminal: prompts. Answers: text, size, color, shape, (percent), output, confirm.
	in := "https://example.com\n350\n\ncircle\n10\n" + out + "\ny\n"
	r := do(t, in, true)
	if r.code != 0 || !strings.Contains(r.stdout, "Summary:") {
		t.Fatalf("%+v", r)
	}
	if got := pngSize(t, out); got != 350 {
		t.Errorf("size %d", got)
	}

	// Declining writes nothing and exits 1.
	out2 := filepath.Join(dir, "declined.png")
	r = do(t, "hello\n\n\nnone\n"+out2+"\nn\n", true)
	if r.code != 1 {
		t.Errorf("declined: %+v", r)
	}
	if _, err := os.Stat(out2); err == nil {
		t.Error("file written after declining")
	}
}

func TestInteractiveFlagPrefillsAndOverwriteIsConfirmed(t *testing.T) {
	out := filepath.Join(t.TempDir(), "p.png")
	if r := do(t, "", false, "-o", out, "old"); r.code != 0 {
		t.Fatal(r)
	}
	// text and output given by flag: only size, color, shape are asked.
	r := do(t, "\n\nnone\ny\n", false, "-i", "-o", out, "new")
	if r.code != 0 || !strings.Contains(r.stdout, "will be overwritten") || strings.Contains(r.stdout, "Output file") {
		t.Fatalf("%+v", r)
	}
	// -yes without -force must not overwrite silently.
	r = do(t, "\n\nnone\n", false, "-i", "-yes", "-o", out, "newer")
	if r.code != 1 || !strings.Contains(r.err, "already exists") {
		t.Errorf("-yes without -force: %+v", r)
	}
}

func TestInteractiveBadFlagFailsFast(t *testing.T) {
	r := do(t, "", true, "-i", "-color", "nope")
	if r.code != 2 || strings.Contains(r.stdout, "Text or URL") {
		t.Errorf("%+v", r)
	}
}

func TestHelpAndVersion(t *testing.T) {
	if r := do(t, "", false, "-version"); r.code != 0 || !strings.Contains(r.stdout, "qrgen") {
		t.Errorf("%+v", r)
	}
	if r := do(t, "", false, "-h"); r.code != 0 || !strings.Contains(r.err, "Usage:") {
		t.Errorf("%+v", r)
	}
}
