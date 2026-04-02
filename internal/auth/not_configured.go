package auth

// ErrNotConfigured signals that a credential source is absent, not broken.
type ErrNotConfigured struct {
	Msg string
}

func (e *ErrNotConfigured) Error() string {
	return e.Msg
}
