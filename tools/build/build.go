package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// runner invokes the Go toolchain. It is injectable so orchestration logic can
// be tested without compiling the full binary.
type runner func(ctx context.Context, goBin string, env, args []string) error

func defaultRunner(ctx context.Context, goBin string, env, args []string) error {
	resolvedGoBin, err := resolveGoBinary(goBin)
	if err != nil {
		return err
	}
	cmd := &exec.Cmd{Path: resolvedGoBin, Args: append([]string{resolvedGoBin}, args...), Env: env} // #nosec G204 -- goBin is the validated Go toolchain path selected by the build configuration.
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func resolveGoBinary(goBin string) (string, error) {
	if strings.ContainsAny(goBin, `/\\`) {
		return goBin, nil
	}
	resolved, err := exec.LookPath(goBin)
	if err != nil {
		return "", fmt.Errorf("resolve go toolchain %q: %w", goBin, err)
	}
	return resolved, nil
}

// reproducibleBuildArgs returns the go build invocation that strips everything
// non-deterministic: -trimpath removes local paths, -buildvcs=false suppresses
// the VCS timestamp/dirty stamp, and -ldflags="-s -w" strips the symbol table
// and DWARF so no build host metadata survives into the binary.
func reproducibleBuildArgs(output, target string) []string {
	return []string{
		"build",
		"-tags", "prod",
		"-trimpath",
		"-buildvcs=false",
		"-ldflags=-s -w",
		"-o", output,
		target,
	}
}

// buildEnv returns a cleaned environment for the release build. CGO is disabled
// for a static binary, GOOS/GOARCH pin the target, and GOFLAGS is cleared so an
// ambient user GOFLAGS cannot alter reproducibility.
func buildEnv(goos, goarch string) []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+4)
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch key {
		case "CGO_ENABLED", "GOOS", "GOARCH", "GOFLAGS":
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"CGO_ENABLED=0",
		"GOOS="+goos,
		"GOARCH="+goarch,
		"GOFLAGS=",
	)
}

func payloadName(goos string) string {
	if goos == "windows" {
		return "payload.exe"
	}
	return "payload"
}

func build(ctx context.Context, opts *options, r runner) (*Manifest, error) {
	frontendHash, err := HashFrontendDir(opts.frontendDir)
	if err != nil {
		return nil, fmt.Errorf("frontend hash: %w", err)
	}

	if err := os.MkdirAll(filepath.Clean(opts.outDir), 0o700); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	payloadPath := opts.outDir + string(os.PathSeparator) + payloadName(opts.os)
	env := buildEnv(opts.os, opts.arch)
	args := reproducibleBuildArgs(payloadPath, opts.target)
	if err := r(ctx, opts.goBinary, env, args); err != nil {
		return nil, fmt.Errorf("go build: %w", err)
	}

	payloadHash, err := hashFile(payloadPath)
	if err != nil {
		return nil, fmt.Errorf("hash payload: %w", err)
	}

	m := &Manifest{
		Commit:        opts.commit,
		Version:       opts.version,
		GoVersion:     runtime.Version(),
		OS:            opts.os,
		Arch:          opts.arch,
		FrontendHash:  frontendHash,
		PayloadSHA256: payloadHash,
		Signature:     nil,
	}
	if err := WriteManifest(m, opts.outDir); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}
	return m, nil
}
