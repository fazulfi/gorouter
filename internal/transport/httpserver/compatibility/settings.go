package compatibility

func settingRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/settings"},
		{Method: "PATCH", Path: "/settings"},
		{Method: "GET", Path: "/settings/database"},
		{Method: "POST", Path: "/settings/database"},
		{Method: "GET", Path: "/settings/require-login"},
		{Method: "PUT", Path: "/settings/require-login"},
		{Method: "POST", Path: "/settings/proxy-test"},
	}
}

func translatorRows() []compatRow {
	return []compatRow{
		{Method: "POST", Path: "/translator/translate"},
		{Method: "GET", Path: "/translator/load"},
		{Method: "POST", Path: "/translator/save"},
		{Method: "POST", Path: "/translator/send"},
		{Method: "GET", Path: "/translator/console-logs"},
		{Method: "DELETE", Path: "/translator/console-logs"},
		{Method: "GET", Path: "/translator/console-logs/stream"},
	}
}

func metaRows() []compatRow {
	return []compatRow{
		{Method: "POST", Path: "/init"},
		{Method: "GET", Path: "/locale"},
		{Method: "GET", Path: "/tags"},
		{Method: "GET", Path: "/health"},
	}
}
