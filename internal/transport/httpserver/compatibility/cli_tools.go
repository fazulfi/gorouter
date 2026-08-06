package compatibility

func cliToolRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/cli-tools/all-statuses"},
		{Method: "GET", Path: "/cli-tools/{tool}"},
		{Method: "POST", Path: "/cli-tools/{tool}"},
		{Method: "POST", Path: "/cli-tools/antigravity-mitm/alias"},
	}
}

func mediaRows() []compatRow {
	return []compatRow{
		{Method: "GET", Path: "/media-providers/tts/voices"},
		{Method: "GET", Path: "/media-providers/tts/deepgram/voices"},
		{Method: "GET", Path: "/media-providers/tts/elevenlabs/voices"},
		{Method: "GET", Path: "/media-providers/tts/inworld/voices"},
		{Method: "GET", Path: "/media-providers/tts/minimax/voices"},
	}
}
