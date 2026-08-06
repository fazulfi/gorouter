package compatibility

func nodeRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/provider-nodes"},
		{Method: "POST", Path: "/provider-nodes"},
		{Method: "GET", Path: "/provider-nodes/{id}"},
		{Method: "PUT", Path: "/provider-nodes/{id}"},
		{Method: "DELETE", Path: "/provider-nodes/{id}"},
		{Method: "POST", Path: "/provider-nodes/validate"},
	}
}
