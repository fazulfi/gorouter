package soak

type Bundle struct {
	Command     string            `json:"command"`
	Environment map[string]string `json:"environment"`
	TrendPoints []*TrendPoint     `json:"trend_points"`
}
