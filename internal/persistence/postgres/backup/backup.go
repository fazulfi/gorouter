package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type CommandRunner interface {
	Run(context.Context, string, []string, []string) ([]byte, []byte, error)
}
type CommandRunnerFunc func(context.Context, string, []string, []string) ([]byte, []byte, error)

func (f CommandRunnerFunc) Run(c context.Context, n string, a, e []string) ([]byte, []byte, error) {
	return f(c, n, a, e)
}

type Artifact struct {
	Path, SHA256 string
	Bytes        int64
	CreatedAt    time.Time
	Kind         string
	RestorePoint string
}
type PITRPoint struct {
	WALPath string
	LSN     string
	At      time.Time
}

var ErrCorrupt = errors.New("backup: corrupt artifact")

func HashFile(path string) (string, error) {
	f, e := os.Open(filepath.Clean(path))
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (a Artifact) Verify() error {
	info, e := os.Stat(a.Path)
	if e != nil {
		return fmt.Errorf("%w: %v", ErrCorrupt, e)
	}
	if !info.Mode().IsRegular() || info.Size() != a.Bytes {
		return ErrCorrupt
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		return ErrCorrupt
	}
	got, e := HashFile(a.Path)
	if e != nil || got != a.SHA256 {
		return ErrCorrupt
	}
	return nil
}
func Capture(path, kind, point string, now time.Time) (Artifact, error) {
	info, e := os.Stat(path)
	if e != nil {
		return Artifact{}, e
	}
	if e = os.Chmod(path, 0600); e != nil && runtime.GOOS != "windows" {
		return Artifact{}, e
	}
	sha, e := HashFile(path)
	if e != nil {
		return Artifact{}, e
	}
	return Artifact{Path: path, SHA256: sha, Bytes: info.Size(), CreatedAt: now, Kind: kind, RestorePoint: point}, nil
}
func Retain(items []Artifact, keep int, now time.Time, maxAge time.Duration) ([]Artifact, []Artifact, error) {
	if keep < 1 || maxAge < 0 {
		return nil, nil, errors.New("backup: invalid retention parameters")
	}
	valid := append([]Artifact(nil), items...)
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].CreatedAt.After(valid[j].CreatedAt) })
	kept := []Artifact{}
	pruned := []Artifact{}
	for _, a := range valid {
		if e := a.Verify(); e != nil {
			return nil, nil, e
		}
		if len(kept) == 0 || (maxAge <= 0 || now.Sub(a.CreatedAt) <= maxAge) && len(kept) < keep {
			kept = append(kept, a)
		} else {
			pruned = append(pruned, a)
		}
	}
	return kept, pruned, nil
}

var lsnPattern = regexp.MustCompile(`^[0-9A-Fa-f]+/[0-9A-Fa-f]+$`)

func safeDBTarget(target string) bool {
	if target == "" || filepath.IsAbs(target) || strings.ContainsAny(target, `/\\;\x00`) || strings.Contains(target, "..") {
		return false
	}
	for _, r := range target {
		if !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
func safeWALPath(path string) bool {
	return path != "" && !filepath.IsAbs(path) && filepath.VolumeName(path) == "" && !strings.Contains(path, ":") && filepath.Clean(path) == path && path != "." && !strings.HasPrefix(path, ".."+string(filepath.Separator)) && !strings.ContainsAny(path, `;\x00`)
}
func Restore(ctx context.Context, runner CommandRunner, a Artifact, target string, point *PITRPoint) error {
	if e := a.Verify(); e != nil {
		return e
	}
	if !safeDBTarget(target) {
		return errors.New("backup: invalid restore target")
	}
	args := []string{"--exit-on-error", "--no-owner", "-d", target, a.Path}
	if point != nil {
		if !safeWALPath(point.WALPath) || !lsnPattern.MatchString(point.LSN) || point.At.IsZero() {
			return errors.New("backup: invalid PITR point")
		}
		args = append(args, "--pitr-wal="+point.WALPath, "--pitr-lsn="+point.LSN)
	}
	_, stderr, e := runner.Run(ctx, "pg_restore", args, nil)
	if e != nil {
		return fmt.Errorf("backup: restore failed: %w: %s", e, string(stderr))
	}
	return nil
}
func Delete(baseDir, path string) error {
	if baseDir == "" || path == "" || filepath.IsAbs(path) {
		return errors.New("backup: invalid path")
	}
	base, e := filepath.Abs(baseDir)
	if e != nil {
		return e
	}
	candidate := filepath.Join(base, filepath.Clean(path))
	if filepath.Dir(candidate) != base || filepath.Clean(path) != path || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return errors.New("backup: path escapes backup directory")
	}
	info, e := os.Lstat(candidate)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return errors.New("backup: refusing non-regular file")
	}
	return os.Remove(candidate)
}
