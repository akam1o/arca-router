package grpc

type getRunningRequest struct{}

type getRunningResponse struct {
	ConfigText string `json:"config_text"`
	Version    uint64 `json:"version"`
	CommitID   string `json:"commit_id,omitempty"`
}

type getCandidateRequest struct {
	SessionID string `json:"session_id"`
}

type getCandidateResponse struct {
	ConfigText string `json:"config_text"`
}

type editCandidateRequest struct {
	SessionID  string `json:"session_id"`
	ConfigText string `json:"config_text"`
}

type editCandidateResponse struct{}

type commitRequest struct {
	SessionID string `json:"session_id"`
	User      string `json:"user"`
	Message   string `json:"message"`
}

type commitResponse struct {
	CommitID string `json:"commit_id"`
	Version  uint64 `json:"version"`
}

type validateCandidateRequest struct {
	SessionID string `json:"session_id"`
}

type validateCandidateResponse struct{}

type discardRequest struct {
	SessionID string `json:"session_id"`
}

type discardResponse struct{}

type rollbackRequest struct {
	SessionID string `json:"session_id"`
	CommitID  string `json:"commit_id"`
	User      string `json:"user"`
	Message   string `json:"message"`
}

type rollbackResponse struct {
	NewCommitID string `json:"new_commit_id"`
	Version     uint64 `json:"version"`
}

type diffRequest struct {
	SessionID string `json:"session_id"`
}

type diffResponse struct {
	DiffText   string `json:"diff_text"`
	HasChanges bool   `json:"has_changes"`
}

type listHistoryRequest struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type listHistoryResponse struct {
	Entries []CommitInfo `json:"entries"`
}

type createSessionRequest struct {
	User string `json:"user"`
}

type createSessionResponse struct {
	SessionID string `json:"session_id"`
}

type closeSessionRequest struct {
	SessionID string `json:"session_id"`
}

type closeSessionResponse struct{}

type acquireLockRequest struct {
	SessionID string `json:"session_id"`
	User      string `json:"user"`
}

type acquireLockResponse struct{}

type releaseLockRequest struct {
	SessionID string `json:"session_id"`
}

type releaseLockResponse struct{}

type getInterfacesRequest struct {
	NameFilter string `json:"name_filter"`
}

type getInterfacesResponse struct {
	Interfaces []InterfaceInfo `json:"interfaces"`
}

type getRoutesRequest struct {
	PrefixFilter string `json:"prefix_filter"`
	ProtoFilter  string `json:"protocol_filter"`
}

type getRoutesResponse struct {
	Routes []RouteInfo `json:"routes"`
}

type getBGPNeighborsRequest struct{}

type getBGPNeighborsResponse struct {
	Neighbors []BGPNeighborInfo `json:"neighbors"`
}

type getRouteTextRequest struct {
	ProtoFilter string `json:"protocol_filter"`
}

type getRouteTextResponse struct {
	Output string `json:"output"`
}

type getBGPSummaryTextRequest struct{}

type getBGPSummaryTextResponse struct {
	Output string `json:"output"`
}

type getBGPNeighborTextRequest struct {
	PeerAddress string `json:"peer_address"`
}

type getBGPNeighborTextResponse struct {
	Output string `json:"output"`
}

type getOSPFNeighborsTextRequest struct{}

type getOSPFNeighborsTextResponse struct {
	Output string `json:"output"`
}

type getSystemInfoRequest struct{}

type getSystemInfoResponse struct {
	Info *SystemInfo `json:"info"`
}
