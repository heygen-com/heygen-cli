package errors

// APIError represents the HeyGen standard error envelope.
// Maps to: {"error": {"code": ..., "message": ..., "param": ..., "doc_url": ...}}
type APIError struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Param   *string `json:"param,omitempty"`
	DocURL  *string `json:"doc_url,omitempty"`
}
