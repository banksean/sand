package daemon

// EnsureImageRequest is the request body for the /ensure-image endpoint.
type EnsureImageRequest struct {
	ImageName string `json:"imageName"`
}

// IDRequest is the request body for endpoints that operate on a single sandbox by ID.
type IDRequest struct {
	ID string `json:"id"`
}

// ExportRequest is the request body for the /export endpoint.
type ExportRequest struct {
	ID              string `json:"id"`
	DestinationPath string `json:"destinationPath"`
}

// StatsRequest is the request body for the /stats endpoint.
type StatsRequest struct {
	IDs []string `json:"ids,omitempty"`
}

// StatusResponse is the response body for endpoints that return a simple status string.
type StatusResponse struct {
	Status string `json:"status"`
}

// ErrorResponse is the response body when a handler returns an error.
type ErrorResponse struct {
	Error string `json:"error"`
}

// SandboxConfigResponse is the response body for the /sandbox-config endpoint.
type SandboxConfigResponse struct {
	Domains []string `json:"domains"`
}
