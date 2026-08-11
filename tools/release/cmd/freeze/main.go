// Command freeze records the exact stable-candidate commit and dependency
// lock hashes into a freeze manifest consumed by the T16 publication gate.
package main

import (
	"flag"
	"fmt"
	"os"

	"gorouter/tools/release"
)

func main() {
	root := flag.String("root", ".", "repository root for dependency lock files")
	candidateSHA := flag.String("commit", "", "frozen stable-candidate commit SHA (40 hex)")
	candidateTag := flag.String("tag", "", "beta tag the candidate was published under")
	stableVersion := flag.String("version", "", "stable version naming for T16 (vMAJOR.MINOR.PATCH)")
	disposition := flag.String("disposition", "", "beta defect disposition summary (verbatim)")
	out := flag.String("out", "freeze-manifest.json", "output path for the freeze manifest")
	flag.Parse()

	if *candidateSHA == "" || *candidateTag == "" || *stableVersion == "" {
		fmt.Fprintln(os.Stderr, "freeze: --commit, --tag and --version are required")
		os.Exit(2)
	}

	rec, err := release.BuildFreezeRecord(*root, *candidateSHA, *candidateTag, *stableVersion, *disposition)
	if err != nil {
		fmt.Fprintf(os.Stderr, "freeze: %v\n", err)
		os.Exit(1)
	}
	if err := release.WriteFreezeRecord(rec, *out); err != nil {
		fmt.Fprintf(os.Stderr, "freeze: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("freeze: wrote %s (candidate %s, stable %s)\n", *out, rec.CandidateSHA, rec.StableVersion)
}
