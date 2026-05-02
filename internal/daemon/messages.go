package daemon

// ErrorResponse is the response body when a handler returns an error.
type ErrorResponse struct {
	Error string `json:"error"`
}

// SandboxConfigResponse is the response body for the /sandbox-config endpoint.
type SandboxConfigResponse struct {
	Domains []string `json:"domains"`
}
