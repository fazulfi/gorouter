// Package release provides the Phase 5 stable-candidate freeze tooling.
//
// The freeze tool records the exact stable-candidate commit and dependency
// lock hashes into a machine-verifiable freeze manifest, so that the T16
// stable publication publishes artifacts built from precisely this candidate
// and from no other tree state.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// FreezeRecord is the persisted stable-candidate freeze manifest produced by
// the freeze tool and consumed (never fabricated) by the T16 publication
// gate. The T16 gate's candidate-sha-pin check embeds this commit SHA.
type FreezeRecord struct {
	// Artifact identifies this document and its owning Phase 5 unit.
	Artifact string `json:"artifact"`
	// Phase is the owning unit label.
	Phase string `json:"phase"`
	// StableVersion is the stable version naming that T16 MUST publish
	// (the beta prerelease suffix is dropped: v1.2.3-beta.1 -> v1.2.3).
	StableVersion string `json:"stable_version"`
	// CandidateSHA is the frozen stable-candidate commit (40 hex chars).
	CandidateSHA string `json:"candidate_sha"`
	// CandidateTag is the beta tag the candidate was published under (T14).
	CandidateTag string `json:"candidate_tag"`
	// DefectDisposition summarises the beta defect triage outcome.
	DefectDisposition string `json:"defect_disposition"`
	// FreezeTimestamp is the UTC wall-clock time the freeze was recorded.
	// It is informational only; determinism of the artifact itself is not
	// guaranteed by this field (the T16 gate re-derives hashes).
	FreezeTimestamp string `json:"freeze_timestamp"`
	// Locks carries the dependency lock hashes pinned by the freeze.
	Locks map[string]string `json:"locks"`
}

// Validate returns an error if the record is not a structurally sound,
// freeze-worthy stable candidate. The candidate SHA must be 40 lowercase
// hex characters and the version must be a valid semver string.
func (r *FreezeRecord) Validate() error {
	if r == nil {
		return fmt.Errorf("freeze record is nil")
	}
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(r.CandidateSHA) {
		return fmt.Errorf("candidate_sha %q is not a 40-hex commit SHA", r.CandidateSHA)
	}
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(r.StableVersion) {
		return fmt.Errorf("stable_version %q is not a plain semver (vMAJOR.MINOR.PATCH)", r.StableVersion)
	}
	if r.StableVersion == r.CandidateTag {
		return fmt.Errorf("stable_version %q must drop the beta prerelease suffix", r.StableVersion)
	}
	if len(r.Locks) == 0 {
		return fmt.Errorf("freeze record has no dependency lock hashes")
	}
	for name, sum := range r.Locks {
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(sum) {
			return fmt.Errorf("lock %q hash %q is not a 64-hex SHA-256", name, sum)
		}
	}
	return nil
}

// LockPaths is the ordered set of dependency lock files that the freeze pins.
// The keys are the logical names recorded in the freeze manifest.
var LockPaths = []struct {
	Name string
	Path string
}{
	{Name: "go.mod", Path: "go.mod"},
	{Name: "go.sum", Path: "go.sum"},
	{Name: "frontend-package-lock.json", Path: "frontend/package-lock.json"},
}

// ComputeLockHashes hashes the repository dependency lock files named by
// LockPaths, resolving each relative path against root. A lock file that is
// absent is recorded as an error: the freeze must fail closed rather than
// silently pin an incomplete dependency set.
func ComputeLockHashes(root string) (map[string]string, error) {
	locks := make(map[string]string, len(LockPaths))
	for _, lp := range LockPaths {
		sum, err := HashFile(filepath.Join(root, lp.Path))
		if err != nil {
			return nil, fmt.Errorf("lock %s: %w", lp.Name, err)
		}
		locks[lp.Name] = sum
	}
	return locks, nil
}

// HashFile returns the lowercase hex SHA-256 of the file at path.
func HashFile(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- path derives from hardcoded LockPaths table, never user input
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// BuildFreezeRecord assembles a FreezeRecord for the given candidate. It
// computes the dependency lock hashes relative to root and stamps a UTC
// timestamp. The defect disposition string is passed through verbatim from
// the caller so the tool never invents beta-defect facts.
func BuildFreezeRecord(root, candidateSHA, candidateTag, stableVersion, defectDisposition string) (*FreezeRecord, error) {
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(candidateSHA) {
		return nil, fmt.Errorf("candidate SHA %q is not a 40-hex commit SHA", candidateSHA)
	}
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(stableVersion) {
		return nil, fmt.Errorf("stable version %q is not a plain semver", stableVersion)
	}
	locks, err := ComputeLockHashes(root)
	if err != nil {
		return nil, err
	}
	rec := &FreezeRecord{
		Artifact:          "stable-candidate-freeze",
		Phase:             "P5-T15",
		StableVersion:     stableVersion,
		CandidateSHA:      candidateSHA,
		CandidateTag:      candidateTag,
		DefectDisposition: defectDisposition,
		FreezeTimestamp:   time.Now().UTC().Format(time.RFC3339),
		Locks:             locks,
	}
	if err := rec.Validate(); err != nil {
		return nil, err
	}
	return rec, nil
}

// WriteFreezeRecord marshals the record deterministically (sorted lock keys,
// fixed field order via the struct) and writes it to path.
func WriteFreezeRecord(rec *FreezeRecord, path string) error {
	if err := rec.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644) // #nosec G306 -- freeze manifest is a public release artifact, 0644 readability required
}

// SortedLockNames returns the lock names in a stable order for display.
func SortedLockNames(rec *FreezeRecord) []string {
	names := make([]string, 0, len(rec.Locks))
	for name := range rec.Locks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
