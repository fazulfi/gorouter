package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gorouter/internal/domain/engine"
)

// Mode selects the harness execution mode.
type Mode string

const (
	// ModeMock runs the provider matrix against controlled mock
	// servers with inert placeholder credentials and golden fixtures.
	ModeMock Mode = "mock"
	// ModeLive executes real provider probes; it requires real
	// credentials and runs only in the protected validation lab.
	ModeLive Mode = "live"
	// ModeDryRun never executes network calls; every provider is
	// reported as SKIP with an explicit reason.
	ModeDryRun Mode = "dry-run"
)

// ResultStatus is the disposition of one provider run.
type ResultStatus string

const (
	ResultPass ResultStatus = "pass"
	ResultSkip ResultStatus = "skip"
	ResultFail ResultStatus = "fail"
)

// Result describes one provider run. Artifact is always sanitised.
type Result struct {
	ProviderID string
	Mode       Mode
	Status     ResultStatus
	SkipReason string
	Artifact   []byte
	RoundTrips int
	Duration   time.Duration
}

// Options configures RunProvider.
type Options struct {
	Mode     Mode
	Creds    CredentialSource
	Redactor *Redactor
	BaseURL  string
	BaseDir  string
	Client   *http.Client
}

// CanonicalFormatOrder returns the deterministic probe-format order:
// the first supported format with a fixture is the primary probe
// format.
func CanonicalFormatOrder() []engine.RequestFormat {
	return []engine.RequestFormat{
		engine.FormatOpenAIChat,
		engine.FormatOpenAICompat,
		engine.FormatCodexResponses,
		engine.FormatAnthropic,
		engine.FormatGemini,
	}
}

// FormatName maps a format constant to its manifest/constant name.
func FormatName(format engine.RequestFormat) string {
	switch format {
	case engine.FormatOpenAIChat:
		return "FormatOpenAIChat"
	case engine.FormatOpenAICompat:
		return "FormatOpenAICompat"
	case engine.FormatCodexResponses:
		return "FormatCodexResponses"
	case engine.FormatAnthropic:
		return "FormatAnthropic"
	case engine.FormatGemini:
		return "FormatGemini"
	}
	return ""
}

// LiveEndpoint returns the canonical live probe endpoint for a format.
func LiveEndpoint(format engine.RequestFormat) string {
	switch format {
	case engine.FormatOpenAIChat, engine.FormatOpenAICompat:
		return "https://api.openai.com/v1/chat/completions"
	case engine.FormatCodexResponses:
		return "https://api.openai.com/v1/responses"
	case engine.FormatAnthropic:
		return "https://api.anthropic.com/v1/messages"
	case engine.FormatGemini:
		return "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent"
	}
	return ""
}

// AuthHeaders returns the credential transport headers for the
// provider manifest (decision #127: Authorization Bearer, x-api-key,
// x-goog-api-key, Cookie for webCookie providers; none for auth_type
// none).
func AuthHeaders(m *Manifest, credential string) map[string]string {
	switch m.AuthType {
	case "none":
		return nil
	case "cookie":
		return map[string]string{"Cookie": "session=" + credential}
	}
	format, err := m.PrimaryFormat()
	if err != nil {
		return map[string]string{"Authorization": "Bearer " + credential}
	}
	switch format {
	case engine.FormatAnthropic:
		return map[string]string{"X-Api-Key": credential}
	case engine.FormatGemini:
		return map[string]string{"X-Goog-Api-Key": credential}
	default:
		return map[string]string{"Authorization": "Bearer " + credential}
	}
}

// ProbeRequest builds the minimal probe request body for a format.
func ProbeRequest(format engine.RequestFormat) ([]byte, error) {
	var body any
	switch format {
	case engine.FormatOpenAIChat, engine.FormatOpenAICompat:
		body = map[string]any{
			"model": "fixture-model",
			"messages": []map[string]any{
				{"role": "user", "content": "ping"},
			},
		}
	case engine.FormatCodexResponses:
		body = map[string]any{"model": "fixture-model", "input": "ping"}
	case engine.FormatAnthropic:
		body = map[string]any{
			"model":      "fixture-model",
			"max_tokens": 16,
			"messages": []map[string]any{
				{"role": "user", "content": "ping"},
			},
		}
	case engine.FormatGemini:
		body = map[string]any{
			"contents": []map[string]any{
				{"parts": []map[string]any{{"text": "ping"}}},
			},
		}
	default:
		return nil, fmt.Errorf("no probe request defined for format %s", format)
	}
	return json.Marshal(body)
}

type httpStatusError struct {
	status int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("provider returned HTTP %d", e.status)
}

func (e *httpStatusError) retryable() bool {
	return e.status == http.StatusTooManyRequests || e.status >= 500
}

func doProbe(ctx context.Context, opts Options, m *Manifest, credential string, format engine.RequestFormat, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range AuthHeaders(m, credential) {
		req.Header.Set(name, value)
	}
	client := opts.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("probe transport error: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read probe response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{status: resp.StatusCode}
	}
	return data, nil
}

func backoff(attempt int) time.Duration {
	d := 50 * time.Millisecond * time.Duration(attempt+1)
	if d > 250*time.Millisecond {
		return 250 * time.Millisecond
	}
	return d
}

func runAttempts(ctx context.Context, opts Options, m *Manifest, credential string, format engine.RequestFormat, body []byte) (artifact []byte, trips int, err error) {
	attempts := 1 + m.Retries
	for attempt := 0; attempt < attempts; attempt++ {
		trips = attempt + 1
		attemptCtx, cancel := context.WithTimeout(ctx, m.Timeout)
		data, err := doProbe(attemptCtx, opts, m, credential, format, body)
		cancel()
		if err == nil {
			return data, trips, nil
		}
		if ctx.Err() != nil {
			return nil, trips, ctx.Err()
		}
		var statusErr *httpStatusError
		if !errors.As(err, &statusErr) || !statusErr.retryable() {
			return nil, trips, err
		}
		select {
		case <-time.After(backoff(attempt)):
		case <-ctx.Done():
			return nil, trips, ctx.Err()
		}
	}
	return nil, trips, fmt.Errorf("provider %s: retries exhausted", m.ProviderID)
}

func redactorFor(opts Options) *Redactor {
	if opts.Redactor != nil {
		return opts.Redactor
	}
	return NewRedactor(nil, nil)
}

func resolveFixture(opts Options, m *Manifest, format engine.RequestFormat) ([]byte, error) {
	rel, err := m.FixturePath(format)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(opts.BaseDir, rel))
}

func runMock(ctx context.Context, m *Manifest, opts Options, red *Redactor, res Result) (Result, error) {
	if opts.BaseURL == "" {
		return res, fmt.Errorf("mock mode requires a BaseURL for provider %s", m.ProviderID)
	}
	format, err := m.PrimaryFormat()
	if err != nil {
		return res, err
	}
	fixture, err := resolveFixture(opts, m, format)
	if err != nil {
		return res, fmt.Errorf("provider %s: %w", m.ProviderID, err)
	}
	body, err := ProbeRequest(format)
	if err != nil {
		return res, err
	}
	credential := PlaceholderCredential(m.ProviderID)
	artifact, trips, err := runAttempts(ctx, opts, m, credential, format, body)
	res.RoundTrips = trips
	if err != nil {
		res.Status = ResultFail
		return res, err
	}
	if err := CompareArtifacts(artifact, fixture); err != nil {
		res.Status = ResultFail
		return res, fmt.Errorf("provider %s: %w", m.ProviderID, err)
	}
	res.Status = ResultPass
	res.Artifact = red.RedactBytes(artifact)
	return res, nil
}

func runLive(ctx context.Context, m *Manifest, opts Options, red *Redactor, res Result) (Result, error) {
	if m.CredentialEnvVar == "" {
		res.Status = ResultSkip
		res.SkipReason = "auth_type none: no credential required; no live probe defined for provider " + m.ProviderID
		return res, nil
	}
	credential, ok := opts.Creds.Lookup(m.CredentialEnvVar)
	if !ok {
		res.Status = ResultSkip
		res.SkipReason = fmt.Sprintf(
			"credential %s unavailable — live validation requires the protected lab; provider %s skipped (no false pass)",
			m.CredentialEnvVar, m.ProviderID)
		return res, nil
	}
	format, err := m.PrimaryFormat()
	if err != nil {
		return res, err
	}
	body, err := ProbeRequest(format)
	if err != nil {
		return res, err
	}
	target := opts.BaseURL
	if target == "" {
		if override, ok := opts.Creds.Lookup(LiveBaseURLEnvVar(m.ProviderID)); ok && override != "" {
			target = override
		} else {
			target = LiveEndpoint(format)
		}
	}
	opts.BaseURL = target
	artifact, trips, err := runAttempts(ctx, opts, m, credential, format, body)
	res.RoundTrips = trips
	if err != nil {
		res.Status = ResultFail
		return res, err
	}
	if err := ValidateArtifactShape(format, artifact); err != nil {
		res.Status = ResultFail
		return res, fmt.Errorf("provider %s: %w", m.ProviderID, err)
	}
	res.Status = ResultPass
	res.Artifact = red.RedactBytes(artifact)
	return res, nil
}

// RunProvider executes one manifest row in the given mode. It returns
// an error only for failed runs; skips return Result with a nil error.
func RunProvider(ctx context.Context, m *Manifest, opts Options) (Result, error) {
	start := time.Now()
	res := Result{ProviderID: m.ProviderID, Mode: opts.Mode}

	if err := m.Validate(); err != nil {
		res.Status = ResultFail
		res.Duration = time.Since(start)
		return res, fmt.Errorf("harness: provider %s: invalid manifest: %w", m.ProviderID, err)
	}
	red := redactorFor(opts)

	switch opts.Mode {
	case ModeDryRun:
		res.Status = ResultSkip
		if m.CredentialEnvVar == "" {
			res.SkipReason = "dry-run: live validation requires the protected lab; provider " + m.ProviderID + " skipped"
		} else {
			res.SkipReason = fmt.Sprintf(
				"dry-run: live validation requires the protected lab; credential %s unavailable", m.CredentialEnvVar)
		}
		res.Duration = time.Since(start)
		return res, nil
	case ModeLive:
		res, err := runLive(ctx, m, opts, red, res)
		res.Duration = time.Since(start)
		return res, err
	case ModeMock:
		res, err := runMock(ctx, m, opts, red, res)
		res.Duration = time.Since(start)
		return res, err
	default:
		res.Status = ResultFail
		res.Duration = time.Since(start)
		return res, fmt.Errorf("harness: unknown mode %q", opts.Mode)
	}
}
