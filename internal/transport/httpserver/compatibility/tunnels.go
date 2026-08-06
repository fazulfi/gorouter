package compatibility

func tunnelRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/tunnel"},
		{Method: "POST", Path: "/tunnel/enable"},
		{Method: "POST", Path: "/tunnel/disable"},
		{Method: "GET", Path: "/tunnel/status"},
		{Method: "GET", Path: "/tunnel/tailscale-check"},
		{Method: "POST", Path: "/tunnel/tailscale-install"},
		{Method: "POST", Path: "/tunnel/tailscale-enable"},
		{Method: "POST", Path: "/tunnel/tailscale-disable"},
	}
}

func hostOpRows() []compatRow {
	return []compatRow{
		{Method: "POST", Path: "/headroom/start"},
		{Method: "POST", Path: "/headroom/stop"},
		{Method: "POST", Path: "/headroom/restart"},
		{Method: "GET", Path: "/headroom/status"},
		{Method: "GET", Path: "/headroom/extras"},
		{Method: "GET", Path: "/headroom/proxy/{path}"},
		{Method: "POST", Path: "/headroom/proxy/{path}"},
		{Method: "POST", Path: "/pxpipe/install"},
		{Method: "POST", Path: "/pxpipe/start"},
		{Method: "POST", Path: "/pxpipe/stop"},
		{Method: "POST", Path: "/pxpipe/restart"},
		{Method: "GET", Path: "/pxpipe/status"},
		{Method: "GET", Path: "/pxpipe/health"},
		{Method: "POST", Path: "/pxpipe/health"},
		{Method: "GET", Path: "/pxpipe/logs"},
		{Method: "GET", Path: "/pxpipe/stats"},
		{Method: "POST", Path: "/mcp/{plugin}/message"},
		{Method: "GET", Path: "/mcp/{plugin}/sse"},
		{Method: "POST", Path: "/shutdown"},
		{Method: "GET", Path: "/version"},
		{Method: "POST", Path: "/version/update"},
		{Method: "POST", Path: "/version/shutdown"},
	}
}
