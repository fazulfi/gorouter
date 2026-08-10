package firstrun

import (
	"errors"
	"os"
)

type Options struct {
	Headless bool
	NoOpen   bool
}
type Wizard struct {
	Open func(string) error
	URL  string
}

func (w Wizard) Run(opts Options) error {
	if w.URL == "" {
		return errors.New("dashboard URL is required")
	}
	if opts.Headless || opts.NoOpen || w.Open == nil {
		return nil
	}
	return w.Open(w.URL)
}
func Default() Wizard                   { return Wizard{URL: "http://127.0.0.1:8080"} }
func (w Wizard) TerminalDetached() bool { return os.Getenv("GOROUTER_DETACHED") == "1" }
