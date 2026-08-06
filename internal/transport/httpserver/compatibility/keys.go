package compatibility

func keyRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/keys"},
		{Method: "POST", Path: "/keys"},
		{Method: "GET", Path: "/keys/{id}"},
		{Method: "PUT", Path: "/keys/{id}"},
		{Method: "DELETE", Path: "/keys/{id}"},
	}
}
