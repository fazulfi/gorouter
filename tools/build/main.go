package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
)

type options struct {
	commit      string
	version     string
	os          string
	arch        string
	outDir      string
	frontendDir string
	target      string
	goBinary    string
}

func parseFlags(args []string, stderr io.Writer) (*options, error) {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var opts options
	fs.StringVar(&opts.commit, "commit", "", "git commit SHA (40 hex)")
	fs.StringVar(&opts.version, "version", "", "semantic version (e.g. v1.2.3)")
	fs.StringVar(&opts.os, "os", runtime.GOOS, "target GOOS")
	fs.StringVar(&opts.arch, "arch", runtime.GOARCH, "target GOARCH")
	fs.StringVar(&opts.outDir, "out", "", "output directory")
	fs.StringVar(&opts.frontendDir, "frontend-dir", "frontend/dist", "frontend build dir to hash")
	fs.StringVar(&opts.target, "target", "./cmd/gorouter", "Go package to build")
	fs.StringVar(&opts.goBinary, "go", "go", "go toolchain binary")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if opts.commit == "" {
		return nil, fmt.Errorf("--commit is required")
	}
	if opts.version == "" {
		return nil, fmt.Errorf("--version is required")
	}
	if opts.outDir == "" {
		return nil, fmt.Errorf("--out is required")
	}
	return &opts, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		return 2
	}
	m, err := build(context.Background(), opts, defaultRunner)
	if err != nil {
		fmt.Fprintf(stderr, "build failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "payload_sha256=%s\nfrontend_hash=%s\nmanifest=%s/manifest.json\n",
		m.PayloadSHA256, m.FrontendHash, opts.outDir)
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
