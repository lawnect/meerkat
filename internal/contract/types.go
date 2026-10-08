package contract

import "time"

const Version = "0.1.0"

type Target struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type Tunnel struct {
	RemoteAddress   string `json:"remote_address"`
	ServerPublicKey string `json:"server_public_key"`
	ServiceName     string `json:"service_name"`
	ServiceToken    string `json:"service_token"`
}

type Configuration struct {
	ConnectorID string `json:"connector_id"`
	Revision    int    `json:"revision"`
	ServiceID   string `json:"service_id"`
	Target      Target `json:"target"`
	Tunnel      Tunnel `json:"tunnel"`
}

type EnrollmentRequest struct {
	Name      string `json:"connector_name"`
	Version   string `json:"connector_version"`
	PublicKey string `json:"public_key"`
}

type EnrollmentResponse struct {
	Configuration
	Credential          string    `json:"credential"`
	CredentialExpiresAt time.Time `json:"credential_expires_at"`
}

type Heartbeat struct {
	ConnectorID     string    `json:"connector_id"`
	ServiceID       string    `json:"service_id"`
	Revision        int       `json:"revision"`
	Version         string    `json:"connector_version"`
	ProcessState    string    `json:"process_state"`
	ProcessRestarts int       `json:"process_restarts"`
	ServiceHealth   string    `json:"service_health"`
	ObservedAt      time.Time `json:"observed_at"`
}

type Error struct {
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}
