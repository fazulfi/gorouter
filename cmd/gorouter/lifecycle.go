package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"gorouter/internal/host/firstrun"
	"gorouter/internal/host/service"
	"gorouter/internal/host/tray"
)

func runLifecycleCommand(args []string, out io.Writer) int {
	if len(args) == 0 {
		return 2
	}
	m := service.NewManager(8080, "service")
	switch args[0] {
	case "service":
		if len(args) < 2 {
			return 2
		}
		switch args[1] {
		case "start":
			if err := m.RunFirstRun(); err != nil {
				return 1
			}
			if err := firstrun.Default().Run(firstrun.Options{NoOpen: true}); err != nil {
				return 1
			}
		case "stop":
			if err := m.Stop(service.Graceful); err != nil {
				return 1
			}
		case "restart":
			_ = m.Stop(service.Graceful)
			if err := m.RunFirstRun(); err != nil {
				return 1
			}
		case "status":
			s, err := m.Status()
			if err != nil {
				return 1
			}
			if err := json.NewEncoder(out).Encode(s); err != nil {
				return 1
			}
		case "enable":
			m.SetAutostart(true)
		case "disable":
			m.SetAutostart(false)
		default:
			return 2
		}
	case "exit":
		fs := flag.NewFlagSet("exit", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		grace := fs.Bool("grace", false, "graceful exit")
		force := fs.Bool("force", false, "force exit")
		if err := fs.Parse(args[1:]); err != nil || (*grace == *force) {
			return 2
		}
		mode := service.Force
		if *grace {
			mode = service.Graceful
		}
		if err := m.Stop(mode); err != nil && !*force {
			return 1
		}
	case "logs":
		fs := flag.NewFlagSet("logs", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		tail := fs.Int("tail", 0, "number of lines")
		follow := fs.Bool("follow", false, "follow logs")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		opts := []service.LogOption{service.Tail(*tail)}
		if *follow {
			opts = append(opts, service.Follow())
		}
		ch, err := m.Logs(opts...)
		if err != nil {
			return 1
		}
		for line := range ch {
			fmt.Fprint(out, line)
		}
	default:
		return 2
	}
	return 0
}

func runBare() int {
	m := service.NewManager(8080, "service")
	if _, err := m.Status(); err != nil {
		return 1
	}
	if err := m.RunFirstRun(); err != nil {
		return 1
	}
	t := tray.New()
	t.SetRunning(true)
	if err := firstrun.Default().Run(firstrun.Options{}); err != nil {
		return 1
	}
	return 0
}
