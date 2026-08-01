package translators

import "errors"

var (
	ErrTranslatorNotFound = errors.New("translator not found for format pair")
	ErrNilRequest         = errors.New("nil request")
	ErrNilResponse        = errors.New("nil response")
	ErrNilChunk           = errors.New("nil chunk")
)
