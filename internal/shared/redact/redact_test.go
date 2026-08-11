package redact

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	// Synthetic secrets are assembled from non-secret fragments so secret
	// scanners never see a token-shaped literal in this source file, while
	// the assembled values still exercise every redaction category.
	skProj := "sk-" + "proj-" + "abc123xyz"
	skProjMixed := "sk-" + "proj-" + "AbCdEf123456"
	ghpClassic := "gh" + "p_" + "16C7e42F292c6912E7710c838347Ae178B4a"
	glpatPAT := "gl" + "pat-" + "4x7y8z9a0b1c2d3e4f5g6h7i"
	ya29Short := "ya" + "29." + "a0AfH6SMDZ9g"
	ya29Long := "ya" + "29." + "a0AfH6SMDZ9gQbCdEf123"
	googleRefresh := "1//" + "0c7wVh8x9y2aA3bB4cC5dD6eE7fF8gG9hH0iI"

	cases := []struct {
		name    string
		input   string
		secrets []string // substrings that must not survive redaction
	}{
		{
			name:    "api key value",
			input:   "provider call failed: api_key_value=" + skProj + " unauthorized",
			secrets: []string{skProj, "api_key_value=" + skProj},
		},
		{
			name:    "labeled api key",
			input:   "x-api-key: " + "sk-" + "ant-api03-9f86d081884c7d659a2feaa0c55ad015",
			secrets: []string{"sk-" + "ant-api03-9f86d081884c7d659a2feaa0c55ad015"},
		},
		{
			name:    "bare provider key",
			input:   "request rejected " + "sk-" + "live-8f14e45fceea167a5a36dedd4bea2543",
			secrets: []string{"sk-" + "live-8f14e45fceea167a5a36dedd4bea2543"},
		},
		{
			name:    "github classic pat",
			input:   "clone failed with " + ghpClassic,
			secrets: []string{ghpClassic},
		},
		{
			name:    "github fine-grained pat",
			input:   "pull error " + "github_" + "pat_11AAASSSS8888VVVV0000_XXXXXXYYYYYYZZZZZ",
			secrets: []string{"github_" + "pat_11AAASSSS8888VVVV0000_XXXXXXYYYYYYZZZZZ"},
		},
		{
			name:    "gitlab pat",
			input:   "ci failed " + glpatPAT,
			secrets: []string{glpatPAT},
		},
		{
			name:    "session hash labeled",
			input:   "worker session_hash=9f86d081884c7d659a2feaa0c55ad015 expired",
			secrets: []string{"9f86d081884c7d659a2feaa0c55ad015"},
		},
		{
			name:    "session id labeled",
			input:   "retrying sid=abc123def456ghi789",
			secrets: []string{"abc123def456ghi789"},
		},
		{
			name:    "sess prefixed session",
			input:   "timeout " + "sess_" + "8f14e45fceea167a5a36dedd4bea2543",
			secrets: []string{"sess_" + "8f14e45fceea167a5a36dedd4bea2543"},
		},
		{
			name:    "oauth access token",
			input:   "oauth failed access_token=" + ya29Short,
			secrets: []string{ya29Short},
		},
		{
			name:    "oauth refresh token",
			input:   "refresh_token=" + googleRefresh,
			secrets: []string{googleRefresh},
		},
		{
			name:    "bearer token",
			input:   "status 401: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "cookie header",
			input:   "upstream 400 Cookie: session=abc123; theme=dark",
			secrets: []string{"abc123"},
		},
		{
			name:    "set-cookie value",
			input:   "set-cookie: sessionid=x7y8z9; Path=/",
			secrets: []string{"x7y8z9"},
		},
		{
			name:    "password labeled",
			input:   "login failed password=hunter2secret! for user admin",
			secrets: []string{"hunter2secret!"},
		},
		{
			name:    "passwd labeled",
			input:   "passwd=Tr0ub4dor&3 rejected",
			secrets: []string{"Tr0ub4dor&3"},
		},
		{
			name:    "pwd labeled",
			input:   "pwd=correct-horse-battery rejected",
			secrets: []string{"correct-horse-battery"},
		},
		// Quoted and JSON-shaped labeled values, one per mandated category.
		{
			name:    "quoted api key value",
			input:   `provider call failed: api_key_value="` + skProjMixed + `" unauthorized`,
			secrets: []string{skProjMixed, `api_key_value="` + skProjMixed + `"`},
		},
		{
			name:    "json api key value",
			input:   `{"level":"error","msg":"provider call failed","api_key_value":"sk-" + "ant-api03-9f86d081884c7d659a2feaa0c55ad015"}`,
			secrets: []string{"sk-" + "ant-api03-9f86d081884c7d659a2feaa0c55ad015"},
		},
		{
			name:    "quoted pat token",
			input:   `token value "pat_AbCdEf123456789" rejected`,
			secrets: []string{"pat_AbCdEf123456789"},
		},
		{
			name:    "json pat token",
			input:   `{"pat":"github_" + "pat_11AAASSSS8888VVVV0000_XXXXXXYYYYYYZZZZZ"}`,
			secrets: []string{"github_" + "pat_11AAASSSS8888VVVV0000_XXXXXXYYYYYYZZZZZ"},
		},
		{
			name:    "quoted session hash",
			input:   `worker session_hash="9f86d081884c7d659a2feaa0c55ad015" expired`,
			secrets: []string{"9f86d081884c7d659a2feaa0c55ad015", `session_hash="9f86d081884c7d659a2feaa0c55ad015"`},
		},
		{
			name:    "json session hash",
			input:   `{"session_hash":"8f14e45fceea167a5a36dedd4bea2543"}`,
			secrets: []string{"8f14e45fceea167a5a36dedd4bea2543"},
		},
		{
			name:    "quoted oauth token",
			input:   `oauth failed access_token="` + ya29Long + `"`,
			secrets: []string{ya29Long, `access_token="` + ya29Long + `"`},
		},
		{
			name:    "json oauth token",
			input:   `{"refresh_token":"` + googleRefresh + `"}`,
			secrets: []string{googleRefresh},
		},
		{
			name:    "quoted cookie",
			input:   `upstream 400 set-cookie: "sessionid=x7y8z9; Path=/"`,
			secrets: []string{"sessionid=x7y8z9", "x7y8z9"},
		},
		{
			name:    "json cookie header",
			input:   `{"cookie_header":"session=abc123; theme=dark"}`,
			secrets: []string{"session=abc123", "abc123"},
		},
		{
			name:    "quoted password",
			input:   `login failed password="hunter2secret!" for user admin`,
			secrets: []string{"hunter2secret!", `password="hunter2secret!"`},
		},
		{
			name:    "single-quoted password",
			input:   `passwd='Tr0ub4dor&3' rejected`,
			secrets: []string{"Tr0ub4dor&3"},
		},
		{
			name:    "json password",
			input:   `{"level":"error","password":"correct-horse-battery"}`,
			secrets: []string{"correct-horse-battery"},
		},
		{
			name:    "json password with space in quoted value",
			input:   `{"password":"hunter 2 secret"}`,
			secrets: []string{"hunter 2 secret"},
		},
		// Mixed-case bare tokens (uppercase value characters).
		{
			name:    "mixed-case bare provider key",
			input:   "request rejected sk-Proj-AbCdEf123456",
			secrets: []string{"sk-Proj-AbCdEf123456"},
		},
		{
			name:    "mixed-case bare sess token",
			input:   "timeout sess_aBcDeFgHiJkLmNoPqRsTuV",
			secrets: []string{"sess_aBcDeFgHiJkLmNoPqRsTuV"},
		},
		{
			name:    "mixed-case bare ya29 token",
			input:   "oauth failed " + "ya" + "29.a0AfH6SMDZ9gQbCdEf123",
			secrets: []string{"ya" + "29." + "a0AfH6SMDZ9gQbCdEf123"},
		},
		{
			name:    "mixed-case bare sk with api03 segment",
			input:   "sk-ant-api03-AbCdEf123456GhIj",
			secrets: []string{"sk-ant-api03-AbCdEf123456GhIj"},
		},
		// Delimiter-joined fragments inside unquoted labeled values are
		// masked whole, including the tail fragment, so no partial secret
		// survives after the delimiter.
		{
			name:    "comma-joined password tail",
			input:   "password=a,b rejected",
			secrets: []string{"a,b", "b"},
		},
		{
			name:    "semicolon-joined password tail leaves both fragments",
			input:   "password=a;b rejected",
			secrets: []string{"a;b", "b", "a"},
		},
		{
			name:    "comma-joined token tail",
			input:   "token=abc123,def456 rejected",
			secrets: []string{"abc123,def456", "def456"},
		},
		// Escaped characters inside quoted labeled values must not
		// truncate the mask at the escaped quote.
		{
			name:    "json password escaped double quote",
			input:   `{"password":"hunter \"2\" secret"}`,
			secrets: []string{`2\`, `secret`},
		},
		{
			name:    "single-quoted password escaped quote",
			input:   `passwd='Tr0ub\'4dor&3' rejected`,
			secrets: []string{`4dor&3`},
		},
		// Authorization headers in canonical unquoted form: the whole
		// scheme-plus-credential is masked, so the raw JWT or base64
		// credential never survives after its Bearer/Basic scheme.
		{
			name:    "authorization bearer jwt",
			input:   "proxy error: authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "authorization basic base64",
			input:   "auth failed: authorization: Basic dXNlcjpwYXNzd29yZA==",
			secrets: []string{"dXNlcjpwYXNzd29yZA=="},
		},
		{
			name:    "authorization lowercase bearer jwt",
			input:   "authorization: bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "capitalized authorization header basic",
			input:   "Authorization: Basic dXNlcjpwYXNzd29yZA==",
			secrets: []string{"dXNlcjpwYXNzd29yZA=="},
		},
		// Bare Bearer/Basic scheme credentials under any other credential
		// label: the scheme matcher runs before the labeled set, so the
		// full credential is masked even when the label would otherwise
		// consume only the scheme word.
		{
			name:    "token labeled bearer jwt",
			input:   "oauth failed: token: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "access token labeled bearer jwt",
			input:   "upstream 401 access_token: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "refresh token labeled bearer jwt",
			input:   "refresh_token: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "oauth token labeled bearer jwt",
			input:   "oauth_token: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "id token labeled bearer jwt",
			input:   "id_token: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "api key labeled basic base64",
			input:   "api_key: Basic dXNlcjpwYXNzd29yZA==",
			secrets: []string{"dXNlcjpwYXNzd29yZA=="},
		},
		{
			name:    "x-api-key labeled basic base64",
			input:   "upstream 403 x-api-key: Basic dXNlcjpwYXNzd29yZA==",
			secrets: []string{"dXNlcjpwYXNzd29yZA=="},
		},
		{
			name:    "apikey labeled bearer jwt",
			input:   "apikey: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "sid labeled bearer jwt",
			input:   "session expired sid: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "session id labeled bearer jwt",
			input:   "session_id: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "sessionid labeled basic base64",
			input:   "sessionid: Basic dXNlcjpwYXNzd29yZA==",
			secrets: []string{"dXNlcjpwYXNzd29yZA=="},
		},
		{
			name:    "session hash labeled bearer jwt",
			input:   "session_hash: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "token labeled lowercase bearer jwt",
			input:   "token: bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:    "mixed-case token label bearer jwt",
			input:   "TOKEN: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			secrets: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.input)
			for _, s := range tc.secrets {
				if strings.Contains(got, s) {
					t.Errorf("Redact(%q) = %q, still contains secret %q", tc.input, got, s)
				}
			}
			if !strings.Contains(got, Mask) {
				t.Errorf("Redact(%q) = %q, want mask %q present", tc.input, got, Mask)
			}
		})
	}
}

func TestRedactMultipleSecretsInOneLine(t *testing.T) {
	skProjShort := "sk-" + "proj-" + "abc123"
	ghpShort := "gh" + "p_" + "16C7e42F"
	in := "api_key_value=" + skProjShort + " token=" + ghpShort + " Cookie: session=" + "abc" + "123"
	got := Redact(in)
	for _, s := range []string{skProjShort, ghpShort, "abc123"} {
		if strings.Contains(got, s) {
			t.Errorf("Redact(%q) = %q, still contains secret %q", in, got, s)
		}
	}
}

func TestRedactLeavesNonSecretTextUntouched(t *testing.T) {
	in := "request 42 failed: connection reset by peer, retrying in 3s"
	if got := Redact(in); got != in {
		t.Errorf("Redact(%q) = %q, want unchanged", in, got)
	}
}

func TestRedactEmpty(t *testing.T) {
	if got := Redact(""); got != "" {
		t.Errorf("Redact(\"\") = %q, want empty", got)
	}
}
