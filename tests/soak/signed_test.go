package soak

import (
	"bytes"
	"testing"
)

var testKey = []byte("test-secret-key-only-for-unit-tests-never-production")

func TestSignedEnvelopeRoundTrip(t *testing.T) {
	if err := RoundTripVerify("0123456789abcdef0123456789abcdef01234567"); err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
}

func TestSignedEnvelopeSignAndVerify(t *testing.T) {
	env := NewTestEnvelope("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := env.Sign(testKey); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if len(env.Signature) == 0 {
		t.Fatal("empty signature")
	}
	if err := env.Verify(testKey); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestSignedEnvelopeVerifyWrongKeyFails(t *testing.T) {
	env := NewTestEnvelope("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err := env.Sign(testKey); err != nil {
		t.Fatalf("sign: %v", err)
	}
	wrong := []byte("wrong-key")
	if err := env.Verify(wrong); err == nil {
		t.Fatal("expected verify failure with wrong key")
	}
}

func TestSignedEnvelopeTamperDetected(t *testing.T) {
	env := NewTestEnvelope("cccccccccccccccccccccccccccccccccccccccc")
	if err := env.Sign(testKey); err != nil {
		t.Fatalf("sign: %v", err)
	}
	env.Content = []byte(`{"tampered":true}`)
	if err := env.Verify(testKey); err == nil {
		t.Fatal("expected verify failure after tamper")
	}
}

func TestSignedEnvelopeMetadata(t *testing.T) {
	env := NewTestEnvelope("dddddddddddddddddddddddddddddddddddddddd")
	if env.Metadata.Version != "p5-t13-signed-envelope.v1" {
		t.Fatalf("version = %q", env.Metadata.Version)
	}
	if env.Metadata.Status != "pending" {
		t.Fatalf("status = %q, want pending", env.Metadata.Status)
	}
	if env.Metadata.CandidateSHA != "dddddddddddddddddddddddddddddddddddddddd" {
		t.Fatalf("candidate sha mismatch")
	}
	if env.Metadata.SignerID != "test-key-only" {
		t.Fatalf("signer id = %q", env.Metadata.SignerID)
	}
	if env.Metadata.Thresholds.ErrorRateCeiling <= 0 {
		t.Fatal("error rate ceiling must be positive")
	}
	if env.Metadata.Thresholds.LatencyP95BoundMs <= 0 {
		t.Fatal("p95 bound must be positive")
	}
}

func TestSignedEnvelopeSignDeterministic(t *testing.T) {
	env1 := NewTestEnvelope("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	env2 := NewTestEnvelope("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	if err := env1.Sign(testKey); err != nil {
		t.Fatalf("sign1: %v", err)
	}
	if err := env2.Sign(testKey); err != nil {
		t.Fatalf("sign2: %v", err)
	}
	if !bytes.Equal(env1.Signature, env2.Signature) {
		t.Fatal("signatures must be deterministic for identical content and key")
	}
}
