package compatibility

func usageRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/usage/stats"},
		{Method: "GET", Path: "/usage/history"},
		{Method: "GET", Path: "/usage/chart"},
		{Method: "GET", Path: "/usage/providers"},
		{Method: "GET", Path: "/usage/request-details"},
		{Method: "GET", Path: "/usage/request-logs"},
		{Method: "GET", Path: "/usage/logs"},
		{Method: "GET", Path: "/usage/{connectionId}"},
		{Method: "GET", Path: "/usage/stream"},
	}
}
