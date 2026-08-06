package compatibility

func poolRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/proxy-pools"},
		{Method: "POST", Path: "/proxy-pools"},
		{Method: "GET", Path: "/proxy-pools/{id}"},
		{Method: "PUT", Path: "/proxy-pools/{id}"},
		{Method: "DELETE", Path: "/proxy-pools/{id}"},
		{Method: "POST", Path: "/proxy-pools/cloudflare-deploy"},
		{Method: "POST", Path: "/proxy-pools/deno-deploy"},
		{Method: "POST", Path: "/proxy-pools/vercel-deploy"},
	}
}
