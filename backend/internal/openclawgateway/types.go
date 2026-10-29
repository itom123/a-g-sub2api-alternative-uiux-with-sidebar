// Package openclawgateway defines the credential-free device management contract.
package openclawgateway

type Device struct {
	DeviceID       string   `json:"deviceId"`
	RequestID      string   `json:"requestId,omitempty"`
	DisplayName    string   `json:"displayName,omitempty"`
	OperatorLabel  string   `json:"operatorLabel,omitempty"`
	ClientID       string   `json:"clientId,omitempty"`
	Platform       string   `json:"platform,omitempty"`
	Role           string   `json:"role,omitempty"`
	Roles          []string `json:"roles,omitempty"`
	Scopes         []string `json:"scopes,omitempty"`
	ApprovedScopes []string `json:"approvedScopes,omitempty"`
	RequestedAt    int64    `json:"ts,omitempty"`
	ApprovedAt     int64    `json:"approvedAtMs,omitempty"`
	LastSeenAt     int64    `json:"lastSeenAtMs,omitempty"`
	EnrollmentID   string   `json:"enrollmentId,omitempty"`
}

type Inventory struct {
	Pending []Device `json:"pending"`
	Paired  []Device `json:"paired"`
}

type Request struct {
	Action    string `json:"action"`
	RequestID string `json:"request_id,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
}
