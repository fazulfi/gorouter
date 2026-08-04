package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/keys"
)

const (
	testSessionCookie = "gorouter_session"
	testUserID        = "11111111-1111-1111-1111-111111111111"
	testUserAgent     = "test-agent/1.0"
	testRemoteIP      = "192.0.2.10"
	testTrustedIP     = "203.0.113.77"
)

var errFakeInvalid = errors.New("invalid credential")

func fakeSessionValidator(ctx context.Context, raw string) (*auth.Actor, error) {
	if raw != "valid-session" {
		return nil, errFakeInvalid
	}
	return &auth.Actor{UserID: uuid.MustParse(testUserID)}, nil
}

func fakePATValidator(ctx context.Context, raw string) (*keys.PAT, error) {
	if raw != "valid-pat" {
		return nil, errFakeInvalid
	}
	return &keys.PAT{UserID: uuid.MustParse(testUserID)}, nil
}

func captureActor(dst **auth.Actor) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a, ok := auth.FromContext(r.Context()); ok {
			*dst = a
		}
		w.WriteHeader(http.StatusOK)
	})
}

func assertUnauthorized(t *testing.T, h http.Handler, req *http.Request) {
	t.Helper()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestSessionAuthMissingCookie(t *testing.T) {
	var unused *auth.Actor
	assertUnauthorized(t, SessionAuth(fakeSessionValidator, testSessionCookie)(captureActor(&unused)),
		httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestSessionAuthInvalidCredential(t *testing.T) {
	var unused *auth.Actor
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: testSessionCookie, Value: "bad-session"})
	assertUnauthorized(t, SessionAuth(fakeSessionValidator, testSessionCookie)(captureActor(&unused)), req)
}

func TestSessionAuthNilActorRejected(t *testing.T) {
	var unused *auth.Actor
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: testSessionCookie, Value: "whatever"})
	assertUnauthorized(t, SessionAuth(func(context.Context, string) (*auth.Actor, error) {
		return nil, nil
	}, testSessionCookie)(captureActor(&unused)), req)
}

func TestSessionAuthValidStampsActor(t *testing.T) {
	var got *auth.Actor
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", testUserAgent)
	req.RemoteAddr = testRemoteIP + ":4321"
	req.AddCookie(&http.Cookie{Name: testSessionCookie, Value: "valid-session"})
	rr := httptest.NewRecorder()
	SessionAuth(fakeSessionValidator, testSessionCookie)(captureActor(&got)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got == nil {
		t.Fatal("expected actor in context")
	}
	if got.Kind != auth.ActorKindSession || got.Origin != auth.ActorOriginRemote || got.UserAgent != testUserAgent {
		t.Fatalf("unexpected actor identity: kind=%q origin=%q ua=%q", got.Kind, got.Origin, got.UserAgent)
	}
	if got.IP == nil || got.IP.String() != testRemoteIP {
		t.Fatalf("expected IP %s, got %v", testRemoteIP, got.IP)
	}
}

func TestSessionAuthValidUsesTrustedProxyRealIP(t *testing.T) {
	var got *auth.Actor
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), realIPKey, testTrustedIP))
	req.RemoteAddr = "192.0.2.99:9999"
	req.AddCookie(&http.Cookie{Name: testSessionCookie, Value: "valid-session"})
	rr := httptest.NewRecorder()
	SessionAuth(fakeSessionValidator, testSessionCookie)(captureActor(&got)).ServeHTTP(rr, req)

	if got == nil || got.IP == nil || got.IP.String() != testTrustedIP {
		t.Fatalf("expected trusted real IP %s, got %v", testTrustedIP, got.IP)
	}
}

func TestPATAuthMissingAuthorization(t *testing.T) {
	var unused *auth.Actor
	assertUnauthorized(t, PATAuth(fakePATValidator)(captureActor(&unused)),
		httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestPATAuthMalformedBearer(t *testing.T) {
	headers := []struct {
		name string
		val  string
	}{
		{"basic scheme", "Basic abc123"},
		{"bare bearer word", "Bearer"},
		{"lowercase bearer", "bearer token"},
		{"no space", "bearer-token"},
	}
	for _, tc := range headers {
		t.Run(tc.name, func(t *testing.T) {
			var unused *auth.Actor
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", tc.val)
			assertUnauthorized(t, PATAuth(fakePATValidator)(captureActor(&unused)), req)
		})
	}
}

func TestPATAuthEmptyTokenAfterTrim(t *testing.T) {
	headers := []string{"Bearer ", "Bearer    ", "Bearer \t "}
	for i, hdr := range headers {
		t.Run("variant "+string(rune('a'+i)), func(t *testing.T) {
			var unused *auth.Actor
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", hdr)
			assertUnauthorized(t, PATAuth(fakePATValidator)(captureActor(&unused)), req)
		})
	}
}

func TestPATAuthInvalidCredential(t *testing.T) {
	var unused *auth.Actor
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-pat")
	assertUnauthorized(t, PATAuth(fakePATValidator)(captureActor(&unused)), req)
}

func TestPATAuthValidStampsActor(t *testing.T) {
	var got *auth.Actor
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", testUserAgent)
	req.Header.Set("Authorization", "Bearer valid-pat")
	req.RemoteAddr = testRemoteIP + ":4321"
	rr := httptest.NewRecorder()
	PATAuth(fakePATValidator)(captureActor(&got)).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got == nil {
		t.Fatal("expected actor in context")
	}
	if got.Kind != auth.ActorKindPAT || got.Origin != auth.ActorOriginRemote || got.UserAgent != testUserAgent {
		t.Fatalf("unexpected actor identity: kind=%q origin=%q ua=%q", got.Kind, got.Origin, got.UserAgent)
	}
	if got.UserID != uuid.MustParse(testUserID) {
		t.Fatalf("expected PAT user id %s, got %s", testUserID, got.UserID)
	}
	if got.IP == nil || got.IP.String() != testRemoteIP {
		t.Fatalf("expected IP %s, got %v", testRemoteIP, got.IP)
	}
}

func TestPATAuthExtraSpacesBeforeToken(t *testing.T) {
	var got *auth.Actor
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer    valid-pat")
	rr := httptest.NewRecorder()
	PATAuth(fakePATValidator)(captureActor(&got)).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 after trimming, got %d", rr.Code)
	}
	if got == nil || got.Kind != auth.ActorKindPAT {
		t.Fatalf("expected PAT actor, got %v", got)
	}
}

func TestPATAuthValidUsesTrustedProxyRealIP(t *testing.T) {
	var got *auth.Actor
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), realIPKey, testTrustedIP))
	req.RemoteAddr = "192.0.2.99:9999"
	req.Header.Set("Authorization", "Bearer valid-pat")
	rr := httptest.NewRecorder()
	PATAuth(fakePATValidator)(captureActor(&got)).ServeHTTP(rr, req)
	if got == nil || got.IP == nil || got.IP.String() != testTrustedIP {
		t.Fatalf("expected trusted real IP %s, got %v", testTrustedIP, got.IP)
	}
}
