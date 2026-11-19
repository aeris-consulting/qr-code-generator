# qrgen

Generate QR codes as PNG images from a URL or any text, with a configurable
size, color and an optional empty center (circle or square) for adding a logo.

```
qrgen [flags] TEXT
qrgen -i                      # interactive mode, asks and shows a summary first
echo "hello" | qrgen -o hello.png
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-size` | `512` | image width and height in pixels (1-4096) |
| `-color` | `#000000` | `#RGB` or `#RRGGBB`, `#` optional; must have 3:1 contrast on white |
| `-hole` | `none` | empty center: `none`, `circle`, `square` |
| `-hole-percent` | | empty center as % of the code area (above 0, at most 20) |
| `-o`, `-output` | `qr.png` | output file, must end in `.png` |
| `-force` | | overwrite an existing file |
| `-i`, `-interactive` | | prompt for each value, then confirm a summary |
| `-yes` | | interactive: skip the final confirmation |
| `-no-verify` | | skip decoding the result to check it scans |

Flags go before the text. Exit codes: `0` ok, `1` failure or aborted, `2` invalid input.

## Empty center

A hole forces error-correction level H. The generated image is decoded again
before it is written, and nothing is written if it does not decode. A hole of
up to about 15% always decodes; between 15% and the 20% maximum it decodes for
larger codes (long text) but can fail for short ones, in which case you get an
error. Use `-no-verify` to write it anyway.

The hole is a percentage of the code area (without the quiet zone). Modules are
cleared whole, so the hole has a slightly jagged edge on small codes. A hole
that would touch a finder pattern (tiny codes) is rejected.

## Build

```
go build -o qrgen ./cmd/qrgen
go test ./...
```

Releases (tag `vX.Y.Z`) publish binaries for linux/amd64, linux/arm64,
windows/amd64 and darwin/arm64, with checksums, via GitHub Actions.
