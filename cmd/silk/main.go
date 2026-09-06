// Command silk optimizes an SVG document from a file or stdin and writes the
// result to stdout.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Gheop/silk"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("silk", flag.ContinueOnError)
	fs.SetOutput(stderr)
	precision := fs.Int("precision", 3, "decimal places kept for coordinates; 0 keeps exact values")
	transformPrecision := fs.Int("transform-precision", 0, "decimal places for transform translations; 0 keeps exact values")
	singlePass := fs.Bool("single-pass", false, "run the pipeline once instead of until stable")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var in []byte
	var err error
	switch fs.NArg() {
	case 0:
		in, err = io.ReadAll(stdin)
	case 1:
		in, err = os.ReadFile(fs.Arg(0))
	default:
		fmt.Fprintln(stderr, "usage: silk [flags] [file.svg]")
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "silk:", err)
		return 1
	}

	out, err := silk.Optimize(in, silk.Options{
		Precision:          *precision,
		TransformPrecision: *transformPrecision,
		Multipass:          !*singlePass,
	})
	if err != nil {
		fmt.Fprintln(stderr, "silk:", err)
		return 1
	}
	// A short or failed write (closed pipe, full disk) must not exit 0: the
	// consumer would treat a truncated document as a valid SVG.
	if _, err := stdout.Write(out); err != nil {
		fmt.Fprintln(stderr, "silk: write:", err)
		return 1
	}
	return 0
}
