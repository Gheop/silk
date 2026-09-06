package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `<svg xmlns="http://www.w3.org/2000/svg"><path d="M 0,0 L 10,0 L 20,0" fill="red"/></svg>`

func TestRunStdin(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(nil, strings.NewReader(sample), &out, &errb); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), `d="M0 0h20"`) {
		t.Errorf("unexpected output: %s", out.String())
	}
}

func TestRunFileAndFlags(t *testing.T) {
	f := filepath.Join(t.TempDir(), "in.svg")
	if err := os.WriteFile(f, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := run([]string{"-precision", "2", f}, nil, &out, &errb); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if out.Len() == 0 || out.Len() >= len(sample) {
		t.Errorf("output not smaller: %d vs %d", out.Len(), len(sample))
	}
}

func TestRunErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"a.svg", "b.svg"}, nil, &out, &errb); code != 2 {
		t.Errorf("two files: exit %d, want 2", code)
	}
	if code := run([]string{"-nope"}, nil, &out, &errb); code != 2 {
		t.Errorf("bad flag: exit %d, want 2", code)
	}
	if code := run([]string{filepath.Join(t.TempDir(), "missing.svg")}, nil, &out, &errb); code != 1 {
		t.Errorf("missing file: exit %d, want 1", code)
	}
	if code := run(nil, strings.NewReader("<svg><unclosed>"), &out, &errb); code != 1 {
		t.Errorf("unparseable input: exit %d, want 1", code)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestRunWriteFailureIsAnError(t *testing.T) {
	// A truncated stdout with exit 0 would hand the consumer a broken SVG.
	var errb bytes.Buffer
	if code := run(nil, strings.NewReader(sample), failWriter{}, &errb); code != 1 {
		t.Errorf("write failure: exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "write") {
		t.Errorf("stderr should name the write failure: %s", errb.String())
	}
}
