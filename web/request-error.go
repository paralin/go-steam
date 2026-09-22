package web

import "strconv"

// RequestError reports a failed operation without its URL, key, or response body.
type RequestError struct {
	// Operation identifies the interface and method.
	Operation string
	// StatusCode is the HTTP failure status, or zero for a transport failure.
	StatusCode int
	// RetryAfter retains Valve's retry hint without scheduling an automatic retry.
	RetryAfter string
	// Err retains the transport cause for cancellation and timeout checks.
	Err error
}

// Error reports the operation and status without printing authentication data.
func (e *RequestError) Error() string {
	if e.StatusCode != 0 {
		return "Steam Web API " + e.Operation + ": HTTP " + strconv.Itoa(e.StatusCode)
	}
	return "Steam Web API " + e.Operation + ": request failed"
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *RequestError) Unwrap() error { return e.Err }
