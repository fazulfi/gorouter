package compatibility

func comboRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/combos"},
		{Method: "POST", Path: "/combos"},
		{Method: "GET", Path: "/combos/{id}"},
		{Method: "PUT", Path: "/combos/{id}"},
		{Method: "DELETE", Path: "/combos/{id}"},
	}
}

func pricingRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/pricing"},
		{Method: "PATCH", Path: "/pricing"},
		{Method: "DELETE", Path: "/pricing"},
	}
}
