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
	precision := flag.Int("precision", 3, "decimal places kept for coordinates; 0 keeps exact values")
	transformPrecision := flag.Int("transform-precision", 0, "decimal places for transform translations; 0 keeps exact values")
	singlePass := flag.Bool("single-pass", false, "run the pipeline once instead of until stable")
	flag.Parse()

	var in []byte
	var err error
	switch flag.NArg() {
	case 0:
		in, err = io.ReadAll(os.Stdin)
	case 1:
		in, err = os.ReadFile(flag.Arg(0))
	default:
		fmt.Fprintln(os.Stderr, "usage: silk [flags] [file.svg]")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "silk:", err)
		os.Exit(1)
	}

	out, err := silk.Optimize(in, silk.Options{
		Precision:          *precision,
		TransformPrecision: *transformPrecision,
		Multipass:          !*singlePass,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "silk:", err)
		os.Exit(1)
	}
	os.Stdout.Write(out)
}
