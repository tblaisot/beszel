package common

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/henrygd/beszel/internal/entities/smart"
	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/henrygd/beszel/internal/entities/systemd"
)

type WebSocketAction = uint8

const (
	// Request system data from agent
	GetData WebSocketAction = iota
	// Check the fingerprint of the agent
	CheckFingerprint
	// Request container logs from agent
	GetContainerLogs
	// Request container info from agent
	GetContainerInfo
	// Request SMART data from agent
	GetSmartData
	// Request detailed systemd service info from agent
	GetSystemdInfo
	// Request ZFS detail data from agent
	GetZfsData
	// Sync network monitor configuration to agent
	SyncNetworkMonitors
	// Request the list of pending package updates from agent
	GetPackageUpdates
	// Request recent logs for a systemd service from the agent.
	GetSystemdLogs
	// Start, stop or restart a container or service (fork)
	Control
	// Open an interactive shell in a container, streamed over an SSH session (fork)
	Exec
	// Add new actions here...
)

// HubRequest defines the structure for requests sent from hub to agent.
type HubRequest[T any] struct {
	Action WebSocketAction `cbor:"0,keyasint"`
	Data   T               `cbor:"1,keyasint,omitempty,omitzero"`
	Id     *uint32         `cbor:"2,keyasint,omitempty"`
}

// AgentResponse defines the structure for responses sent from agent to hub.
type AgentResponse struct {
	Id          *uint32                    `cbor:"0,keyasint,omitempty"`
	SystemData  *system.CombinedData       `cbor:"1,keyasint,omitempty,omitzero"` // Legacy (<= 0.17)
	Fingerprint *FingerprintResponse       `cbor:"2,keyasint,omitempty,omitzero"` // Legacy (<= 0.17)
	Error       string                     `cbor:"3,keyasint,omitempty,omitzero"`
	String      *string                    `cbor:"4,keyasint,omitempty,omitzero"` // Legacy (<= 0.17)
	SmartData   map[string]smart.SmartData `cbor:"5,keyasint,omitzero"`           // Legacy (<= 0.17)
	ServiceInfo systemd.ServiceDetails     `cbor:"6,keyasint,omitempty,omitzero"` // Legacy (<= 0.17)
	// Data is the generic response payload for new endpoints (0.18+)
	Data          cbor.RawMessage `cbor:"7,keyasint,omitempty,omitzero"`
	SmartComplete bool            `cbor:"8,keyasint,omitempty,omitzero"`
}

type FingerprintRequest struct {
	Signature   []byte `cbor:"0,keyasint"`
	NeedSysInfo bool   `cbor:"1,keyasint"` // For universal token system creation
}

type FingerprintResponse struct {
	Fingerprint string `cbor:"0,keyasint"`
	// Optional system info for universal token system creation
	Hostname string `cbor:"1,keyasint,omitzero"`
	Port     string `cbor:"2,keyasint,omitzero"`
	Name     string `cbor:"3,keyasint,omitzero"`
}

type DataRequestOptions struct {
	CacheTimeMs    uint16 `cbor:"0,keyasint"`
	IncludeDetails bool   `cbor:"1,keyasint"`
}

type ZfsDataRequest struct {
	Force bool `cbor:"0,keyasint,omitempty"`
}

type ContainerLogsRequest struct {
	ContainerID string `cbor:"0,keyasint"`
}

type ContainerInfoRequest struct {
	ContainerID string `cbor:"0,keyasint"`
}

type SystemdInfoRequest struct {
	ServiceName string `cbor:"0,keyasint"`
}

type SystemdLogsRequest struct {
	ServiceName string `cbor:"0,keyasint"`
}

// ControlRequest asks the agent to start, stop or restart a container or a systemd service.
type ControlRequest struct {
	Kind   string `cbor:"0,keyasint"` // "container" or "service"
	Target string `cbor:"1,keyasint"` // container id or service unit name
	Action string `cbor:"2,keyasint"` // "start", "stop" or "restart"
}

// ExecRequest opens an interactive shell in a container. It is the first message of an SSH
// session; after the agent answers it, the session carries ExecFrames in both directions.
type ExecRequest struct {
	ContainerID string `cbor:"0,keyasint"`
	Cols        uint16 `cbor:"1,keyasint,omitempty"`
	Rows        uint16 `cbor:"2,keyasint,omitempty"`
}

// Kinds of ExecFrame.
const (
	ExecData   uint8 = iota // terminal bytes: stdin to the container, or its output to the hub
	ExecResize              // hub to agent: new terminal size in Cols and Rows
	ExecPing                // hub to agent: keeps an idle connection open
	ExecExit                // agent to hub: the shell ended with Code
)

// ExecFrame is one message of an exec stream.
type ExecFrame struct {
	Kind uint8  `cbor:"0,keyasint"`
	Data []byte `cbor:"1,keyasint,omitempty"`
	Cols uint16 `cbor:"2,keyasint,omitempty"`
	Rows uint16 `cbor:"3,keyasint,omitempty"`
	Code int    `cbor:"4,keyasint,omitempty"`
}
