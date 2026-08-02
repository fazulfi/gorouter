package redact

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		secrets []string // substrings that must not survive redaction
	}{
		{
			name:    "api key value",
			input:   "provider call failed: api_key_value=sk-proj-abc123xyz unauthorized",
			secrets: []string{"sk-proj-abc123xyz", "api_key_value=sk-proj-abc123xyz"},
		},
		{
			name:    "labeled api key",
			input:   "x-api-key: sk-ant-api03-9f86d081884c7d659a2feaa0c55ad015",
			secrets: []string{"sk-ant-api03-9f86d081884c7d659a2feaa0c55ad015"},
		},
		{
			name:    "bare provider key",
			input:   "request rejected sk-live-8f14e45fceea167a5a36dedd4bea2543",
			secrets: []string{"sk-live-8f14e45fceea167a5a36dedd4bea2543"},
		},
		{
			name:    "github classic pat",
			input:   "clone failed with ghp_16C7e42F292c6912E7710c838347Ae178B4a",
			secrets: []string{"ghp_16C7e42F292c6912E7710c838347Ae178B4a"},
		},
		{
			name:    "github fine-grained pat",
			input:   "pull error github_pat_11AAASSSS8888VVVV0000_XXXXXXYYYYYYZZZZZ",
			secrets: []string{"github_pat_11AAASSSS8888VVVV0000_XXXXXXYYYYYYZZZZZ"},
		},
		{
			name:    "gitlab pat",
			input:   "ci failed glpat-4x7y8z9a0b1c2d3e4f5g6h7i",
			secrets: []string{"glpat-4x7y8z9a0b1c2d3e4f5g6h7i"},
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
			input:   "timeout sess_8f14e45fceea167a5a36dedd4bea2543",
			secrets: []string{"sess_8f14e45fceea167a5a36dedd4bea2543"},
		},
		{
			name:    "oauth access token",
			input:   "oauth failed access_token=ya29.a0AfH6SMDZ9g",
			secrets: []string{"ya29.a0AfH6SMDZ9g"},
		},
		{
			name:    "oauth refresh token",
			input:   "refresh_token=1//0c7wVh8x9y2aA3bB4cC5dD6eE7fF8gG9hH0iI",
			secrets: []string{"1//0c7wVh8x9y2aA3bB4cC5dD6eE7fF8gG9hH0iI"},
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
			input:   `provider call failed: api_key_value="sk-proj-AbCdEf123456" unauthorized`,
			secrets: []string{"sk-proj-AbCdEf123456", `api_key_value="sk-proj-AbCdEf123456"`},
		},
		{
			name:    "json api key value",
			input:   `{"level":"error","msg":"provider call failed","api_key_value":"sk-ant-api03-9f86d081884c7d659a2feaa0c55ad015"}`,
			secrets: []string{"sk-ant-api03-9f86d081884c7d659a2feaa0c55ad015"},
		},
		{
			name:    "quoted pat token",
			input:   `token value "pat_AbCdEf123456789" rejected`,
			secrets: []string{"pat_AbCdEf123456789"},
		},
		{
			name:    "json pat token",
			input:   `{"pat":"github_pat_11AAASSSS8888VVVV0000_XXXXXXYYYYYYZZZZZ"}`,
			secrets: []string{"github_pat_11AAASSSS8888VVVV0000_XXXXXXYYYYYYZZZZZ"},
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
			input:   `oauth failed access_token="ya29.a0AfH6SMDZ9gQbCdEf123"`,
			secrets: []string{"ya29.a0AfH6SMDZ9gQbCdEf123", `access_token="ya29.a0AfH6SMDZ9gQbCdEf123"`},
		},
		{
			name:    "json oauth token",
			input:   `{"refresh_token":"1//0c7wVh8x9y2aA3bB4cC5dD6eE7fF8gG9hH0iI"}`,
			secrets: []string{"1//0c7wVh8x9y2aA3bB4cC5dD6eE7fF8gG9hH0iI"},
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
			input:   "oauth failed ya29.a0AfH6SMDZ9gQbCdEf123",
			secrets: []string{"ya29.a0AfH6SMDZ9gQbCdEf123"},
		},
		{
			name:    "mixed-case bare sk with api03 segment",
			input:   "sk-ant-api03-AbCdEf123456GhIj",
			secrets: []string{"sk-ant-api03-AbCdEf123456GhIj"},
		},
		// Comma- and semicolon-joined fragments are masked whole, not
		// truncated at the delimiter (no partial secret survives).
		{
			name:    "comma-joined password tail",
			input:   "password=a,b rejected",
			secrets: []string{"a,b"},
		},
		{
			name:    "semicolon-joined password tail",
			input:   "password=a;b rejected",
			secrets: []string{"a;b"},
		},
		{
			name:    "comma-joined token tail",
			input:   "token=abc123,def456 rejected",
			secrets: []string{"abc123,def456"},
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
	in := "api_key_value=sk-proj-abc123 token=ghp_16C7e42F Cookie: session=abc123"
	got := Redact(in)
	for _, s := range []string{"sk-proj-abc123", "ghp_16C7e42F", "abc123"} {
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
