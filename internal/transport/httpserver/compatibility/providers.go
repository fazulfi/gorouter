package compatibility

func providerRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/providers"},
		{Method: "POST", Path: "/providers"},
		{Method: "GET", Path: "/providers/{id}"},
		{Method: "PUT", Path: "/providers/{id}"},
		{Method: "DELETE", Path: "/providers/{id}"},
		{Method: "GET", Path: "/providers/client"},
		{Method: "GET", Path: "/providers/kilo/free-models"},
		{Method: "GET", Path: "/providers/suggested-models"},
		{Method: "POST", Path: "/providers/test-batch"},
		{Method: "POST", Path: "/providers/validate"},
	}
}
