package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOptionsRequiresExactlyOneTrimmedSelector(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"neither", nil},
		{"both", []string{"--run-id", "run-1", "--experiment-id", "experiment-1"}},
		{"blank run", []string{"--run-id", " \t "}},
		{"blank output", []string{"--run-id", "run-1", "--output", " "}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseOptions(test.args); err == nil {
				t.Fatal("parseOptions() error = nil")
			}
		})
	}

	options, err := parseOptions([]string{
		"--run-id", " run-1 ",
		"--output", " trajectory.jsonl ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.runID != "run-1" || options.experimentID != "" ||
		options.output != "trajectory.jsonl" {
		t.Fatalf("options = %#v", options)
	}
}

func TestWriteOutputAtomicallyReplacesOrPreservesDestination(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "trajectory.jsonl")
	if err := os.WriteFile(path, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("export failed")
	err := writeOutput(path, func(writer io.Writer) error {
		if _, writeErr := writer.Write([]byte("partial\n")); writeErr != nil {
			return writeErr
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("writeOutput() error = %v, want sentinel", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "original\n" {
		t.Fatalf("failed export replaced destination: %q", raw)
	}
	matches, err := filepath.Glob(filepath.Join(directory, ".research-export-*.tmp"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %v, %v", matches, err)
	}

	if err := writeOutput(path, func(writer io.Writer) error {
		_, writeErr := writer.Write([]byte("replacement\n"))
		return writeErr
	}); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "replacement\n" {
		t.Fatalf("destination = %q", raw)
	}
}

func TestParseOptionsDoesNotWriteGlobalFlagState(t *testing.T) {
	first, err := parseOptions([]string{"--run-id", "run-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := parseOptions([]string{"--experiment-id", "experiment-1"})
	if err != nil {
		t.Fatal(err)
	}
	if first.runID != "run-1" || second.experimentID != "experiment-1" {
		t.Fatalf("independent parses = %#v, %#v", first, second)
	}
	if strings.TrimSpace(first.output) != "-" || strings.TrimSpace(second.output) != "-" {
		t.Fatalf("default outputs = %q, %q", first.output, second.output)
	}
}

func TestParseOptionsSupportsHelp(t *testing.T) {
	if _, err := parseOptions([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseOptions(--help) error = %v, want flag.ErrHelp", err)
	}
}
