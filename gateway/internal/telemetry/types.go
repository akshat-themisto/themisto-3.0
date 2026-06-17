package telemetry

import "time"

type Event struct {
	Timestamp       time.Time
	DeviceID        string
	OrgID           string
	RequestMethod   string
	RequestHost     string
	RequestPath     string
	RequestPort     int
	ResponseStatus  int
	LatencyMs       int
	BytesSent       int64
	BytesReceived   int64
	PolicyDecision  string
	MatchedRuleID   *string
	PolicyVersion   *string
	AgentVersion    *string
	ProtocolVersion *string
	SourceApp       *string
	OS              *string
	ServiceCategory *string
	AIVendor        *string
	CaptureSurface  *string
}

// DLPEvent represents a DLP match detected by an agent.
type DLPEvent struct {
	Timestamp            time.Time
	DeviceID             string
	OrgID                string
	RequestID            string
	RequestHost          string
	RequestPath          string
	RequestMethod        string
	SourceApp            string
	ServiceCategory      string
	AIVendor             string
	MatchTypes           []string
	MatchedPatterns      []string
	MatchedFields        []string
	MatchCount           int
	Severity             string
	ClassificationReason string
	ContentType          string
	FileCount            int
	ActionTaken          string
	PolicyRuleID         string
	ReasonCode           string
	ReasonDetail         string
	Protocol             string
	InterceptedHTTPS     bool
	InspectionQuality    string
	InspectionSkipReason string
	Direction            string
	RequestBody          string
	RequestBodyTruncated bool
}
