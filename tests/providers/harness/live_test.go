package harness_test

import (
	"context"
	"os"
	"testing"

	"gorouter/tests/providers/harness"
)

// TestLiveDryRun_UnavailableCredentialsSkip is the dry-run live
// harness: with no real credentials present it must SKIP every
// provider with an explicit reason — never a false PASS and never a
// spurious FAIL. In the protected lab, providers with credentials
// present may pass or skip explicitly, but must not fail spuriously.
func TestLiveDryRun_UnavailableCredentialsSkip(t *testing.T) {
	ms := loadManifests(t)
	ctx := context.Background()
	for _, m := range ms {
		res, err := harness.RunProvider(ctx, m, harness.Options{
			Mode:  harness.ModeLive,
			Creds: harness.EnvSource{},
		})
		if err != nil {
			t.Errorf("provider %s: live dry-run must not error, got %v", m.ProviderID, err)
			continue
		}
		if m.CredentialEnvVar == "" {
			if res.Status == harness.ResultFail {
				t.Errorf("provider %s (auth none): dry-run must not fail", m.ProviderID)
			}
			continue
		}
		if _, ok := os.LookupEnv(m.CredentialEnvVar); !ok {
			if res.Status != harness.ResultSkip {
				t.Errorf("provider %s: credential %s unavailable — must SKIP, got %s (no false pass)", m.ProviderID, m.CredentialEnvVar, res.Status)
			}
			if res.SkipReason == "" {
				t.Errorf("provider %s: skip must carry an explicit reason", m.ProviderID)
			}
		} else {
			if res.Status == harness.ResultFail {
				t.Errorf("provider %s: live run with credentials present must not fail spuriously", m.ProviderID)
			}
		}
		if res.RoundTrips != 0 {
			t.Errorf("provider %s: dry-run must not execute requests, round trips = %d", m.ProviderID, res.RoundTrips)
		}
	}
}

// TestLiveDryRunModeAlwaysSkips asserts ModeDryRun semantics: every
// provider is skipped with a dry-run reason regardless of credential
// availability, and no network call is attempted.
func TestLiveDryRunModeAlwaysSkips(t *testing.T) {
	ms := loadManifests(t)
	ctx := context.Background()
	for _, m := range ms {
		res, err := harness.RunProvider(ctx, m, harness.Options{Mode: harness.ModeDryRun})
		if err != nil {
			t.Errorf("provider %s: dry-run mode must not error, got %v", m.ProviderID, err)
			continue
		}
		if res.Status != harness.ResultSkip {
			t.Errorf("provider %s: dry-run mode status = %s, want skip", m.ProviderID, res.Status)
		}
		if res.SkipReason == "" || res.RoundTrips != 0 {
			t.Errorf("provider %s: dry-run skip must carry a reason and make no requests (reason %q, trips %d)",
				m.ProviderID, res.SkipReason, res.RoundTrips)
		}
	}
}
