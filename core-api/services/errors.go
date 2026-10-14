package services

// BadRequestError marks errors caused by client input (mapped to HTTP 400).
// Anything else is treated as an internal error (HTTP 500).
type BadRequestError struct{ Msg string }

func (e *BadRequestError) Error() string { return e.Msg }
