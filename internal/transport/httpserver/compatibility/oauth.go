package compatibility

func oauthRows() []compatRow {
	return []compatRow{
		{Method: "POST", Path: "/oauth/codex/import-token"},
		{Method: "POST", Path: "/oauth/codex/bulk-import"},
		{Method: "POST", Path: "/oauth/cursor/import"},
		{Method: "POST", Path: "/oauth/cursor/auto-import"},
		{Method: "POST", Path: "/oauth/gitlab/pat"},
		{Method: "POST", Path: "/oauth/iflow/cookie"},
		{Method: "POST", Path: "/oauth/kiro/import"},
		{Method: "POST", Path: "/oauth/kiro/auto-import"},
		{Method: "POST", Path: "/oauth/kiro/import-cli-proxy"},
		{Method: "POST", Path: "/oauth/kiro/api-key"},
		{Method: "POST", Path: "/oauth/kiro/social-authorize"},
		{Method: "POST", Path: "/oauth/kiro/social-exchange"},
		{Method: "GET", Path: "/oauth/{provider}"},
		{Method: "POST", Path: "/oauth/{provider}"},
		{Method: "DELETE", Path: "/oauth/{provider}"},
	}
}
