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
	ConnectorID string                 `json:"connector_id"`
	Revision    int                    `json:"revision"`
	ServiceID   string                 `json:"service_id"`
	Target      Target                 `json:"target"`
	Tunnel      Tunnel                 `json:"tunnel"`
	Services    []ServiceConfiguration `json:"services,omitempty"`
}

type ServiceConfiguration struct {
	ServiceID string `json:"service_id"`
	Target    Target `json:"target"`
	Tunnel    Tunnel `json:"tunnel"`
}

// ActiveServices accepts the original single-service configuration so existing
// connector sessions and Outlet state remain readable after an upgrade.
func (c Configuration) ActiveServices() []ServiceConfiguration {
	if len(c.Services) != 0 {
		return c.Services
	}
	if c.ServiceID != "" {
		return []ServiceConfiguration{{c.ServiceID, c.Target, c.Tunnel}}
	}
	return nil
}

type EnrollmentRequest struct {
	Name      string `json:"connector_name"`
	Version   string `json:"connector_version"`
	PublicKey string `json:"public_key"`
}

type EnrollmentResponse struct {
	Configuration
	Credential          string    `json:"credential"`
	CredentialExpiresAt time.Time `json:"credential_expires_at,omitzero"`
}

type Heartbeat struct {
	ConnectorID     string            `json:"connector_id"`
	ServiceID       string            `json:"service_id"`
	Revision        int               `json:"revision"`
	Version         string            `json:"connector_version"`
	ProcessState    string            `json:"process_state"`
	ProcessRestarts int               `json:"process_restarts"`
	ServiceHealth   string            `json:"service_health"`
	ServiceHealths  map[string]string `json:"service_healths,omitempty"`
	ObservedAt      time.Time         `json:"observed_at"`
}

type Error struct {
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}
