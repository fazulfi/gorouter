package oauth

import "errors"

var (
	ErrSessionNotFound = errors.New("oauth session not found")
	ErrSessionExpired  = errors.New("oauth session has expired")
	ErrTerminalState   = errors.New("oauth session already in terminal state")
)
