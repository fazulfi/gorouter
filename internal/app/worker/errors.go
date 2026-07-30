// Package worker provides a durable asynchronous job worker that polls the jobs
// table, creates execution contexts, handles job lifecycle (pending → running →
// completed / failed), and stores results.
package worker

import (
	"errors"
)

// Sentinel errors returned by the worker and handler.
var (
	// ErrJobNotFound is returned when a job ID does not match any known job.
	ErrJobNotFound = errors.New("job not found")

	// ErrJobAlreadyCompleted is returned when attempting to process a job that
	// is already in a terminal state.
	ErrJobAlreadyCompleted = errors.New("job already completed")

	// ErrWorkerStopped is returned when attempting to enqueue a job after the
	// worker has been shut down.
	ErrWorkerStopped = errors.New("worker is stopped")

	// ErrSignalQueueFull is returned when the worker's signal channel is at
	// capacity and cannot accept more enqueue requests.
	ErrSignalQueueFull = errors.New("worker signal queue is full")
)
