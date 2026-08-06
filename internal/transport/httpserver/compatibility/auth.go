package compatibility

// authRows preserves the historical auth surface: the dashboard sign-in,
// sign-out and status routes plus the OIDC flow. The OIDC rows resolve only
// when an OIDC source is wired (their twin is registered conditionally).
func authRows() []compatRow {
	return []compatRow{
		{Method: "POST", Path: "/auth/login"},
		{Method: "POST", Path: "/auth/logout"},
		{Method: "GET", Path: "/auth/status"},
		{Method: "GET", Path: "/auth/oidc/start"},
		{Method: "GET", Path: "/auth/oidc/callback"},
		{Method: "POST", Path: "/auth/oidc/test"},
	}
}
