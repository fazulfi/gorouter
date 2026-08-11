package tray

import "sync"

type Action string

const (
	ActionStart Action = "start"
	ActionStop  Action = "stop"
	ActionOpen  Action = "open"
	ActionExit  Action = "exit"
)

type Item struct {
	Label  string
	Action Action
}
type Menu struct{ Items []Item }
type Tray struct {
	mu      sync.Mutex
	running bool
	menu    Menu
}

func New() *Tray {
	return &Tray{menu: Menu{Items: []Item{{"Open dashboard", ActionOpen}, {"Start", ActionStart}, {"Stop", ActionStop}, {"Exit", ActionExit}}}}
}
func (t *Tray) Menu() Menu              { t.mu.Lock(); defer t.mu.Unlock(); return t.menu }
func (t *Tray) SetRunning(running bool) { t.mu.Lock(); defer t.mu.Unlock(); t.running = running }
func (t *Tray) Running() bool           { t.mu.Lock(); defer t.mu.Unlock(); return t.running }
