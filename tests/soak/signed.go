package soak

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidSignature = errors.New("invalid signature")

type SignedEnvelope struct {
	Content   json.RawMessage `json:"content"`
	Metadata  Metadata        `json:"metadata"`
	Signature []byte          `json:"signature"`
}

type Metadata struct {
	Version        string     `json:"version"`
	CandidateSHA   string     `json:"candidate_sha"`
	StartTimestamp time.Time  `json:"start_timestamp"`
	EndTimestamp   time.Time  `json:"end_timestamp"`
	SampleInterval string     `json:"sample_interval"`
	Thresholds     Thresholds `json:"thresholds"`
	Status         string     `json:"status"`
	SignerID       string     `json:"signer_id"`
}

type Thresholds struct {
	ErrorRateCeiling      float64 `json:"error_rate_ceiling"`
	LatencyP95BoundMs     float64 `json:"latency_p95_bound_ms"`
	LatencyP99BoundMs     float64 `json:"latency_p99_bound_ms"`
	MemoryGrowthPercent   float64 `json:"memory_growth_percent"`
	GoroutineStabilityMax int     `json:"goroutine_stability_max"`
}

func (e *SignedEnvelope) Sign(secretKey []byte) error {
	hash := hmac.New(sha256.New, secretKey)
	contentJSON, err := json.Marshal(e.Content)
	if err != nil {
		return err
	}
	hash.Write(contentJSON)
	e.Signature = hash.Sum(nil)
	return nil
}

func (e *SignedEnvelope) Verify(secretKey []byte) error {
	hash := hmac.New(sha256.New, secretKey)
	contentJSON, err := json.Marshal(e.Content)
	if err != nil {
		return err
	}
	hash.Write(contentJSON)
	expected := hash.Sum(nil)
	if subtle.ConstantTimeCompare(e.Signature, expected) != 1 {
		return ErrInvalidSignature
	}
	return nil
}

func NewTestEnvelope(candidateSHA string) *SignedEnvelope {
	now := time.Now()
	return &SignedEnvelope{
		Content: json.RawMessage(`{"trend_points":[],"summary":{}}`),
		Metadata: Metadata{
			Version:        "p5-t13-signed-envelope.v1",
			CandidateSHA:   candidateSHA,
			StartTimestamp: now,
			EndTimestamp:   now.Add(72 * time.Hour),
			SampleInterval: "5m",
			Status:         "pending",
			SignerID:       "test-key-only",
			Thresholds: Thresholds{
				ErrorRateCeiling:      0.01,
				LatencyP95BoundMs:     50.0,
				LatencyP99BoundMs:     100.0,
				MemoryGrowthPercent:   20.0,
				GoroutineStabilityMax: 100,
			},
		},
	}
}

func RoundTripVerify(candidateSHA string) error {
	key := []byte("test-secret-key-for-verification-only-do-not-use-in-production")
	env := NewTestEnvelope(candidateSHA)
	if err := env.Sign(key); err != nil {
		return fmt.Errorf("sign failed: %v", err)
	}
	if err := env.Verify(key); err != nil {
		return fmt.Errorf("verify failed: %v", err)
	}
	return nil
}
