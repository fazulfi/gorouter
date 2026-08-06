package compatibility

func modelRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/models"},
		{Method: "PUT", Path: "/models"},
		{Method: "GET", Path: "/models/alias"},
		{Method: "PUT", Path: "/models/alias"},
		{Method: "DELETE", Path: "/models/alias"},
		{Method: "GET", Path: "/models/custom"},
		{Method: "POST", Path: "/models/custom"},
		{Method: "DELETE", Path: "/models/custom"},
		{Method: "GET", Path: "/models/disabled"},
		{Method: "POST", Path: "/models/disabled"},
		{Method: "DELETE", Path: "/models/disabled"},
		{Method: "POST", Path: "/models/test"},
		{Method: "GET", Path: "/models/availability"},
	}
}
