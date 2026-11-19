package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// DriveOffsetMax is the website gateway's maximum list offset (int32 max).
const DriveOffsetMax int64 = 2147483647

// DriveGatewayResp is the body the website gateway returns for the 13 drive
// routes. The IDL response encoder does not run on these paths. BaseResp is
// the only object the gateway validates (status_code number or decimal string,
// status_msg string). Raw is the full JSON body.
type DriveGatewayResp struct {
	BaseResp *DriveBaseResp  `json:"base_resp,omitempty"`
	Raw      json.RawMessage `json:"-"`
}

// DriveBaseResp is the gateway envelope checked before the body is returned.
type DriveBaseResp struct {
	StatusCode json.RawMessage `json:"status_code,omitempty"`
	StatusMsg  string          `json:"status_msg,omitempty"`
}

// StatusCodeInt parses status_code when it is a finite number or a decimal string.
func (b DriveBaseResp) StatusCodeInt() (int64, bool) {
	raw := bytes.TrimSpace(b.StatusCode)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, false
		}
		return parseDecimalInt(s)
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n != math.Trunc(n) {
		return 0, false
	}
	return int64(n), true
}

func (r *DriveGatewayResp) setGatewayRaw(b []byte) {
	r.Raw = append(json.RawMessage(nil), b...)
}

// UnmarshalJSON keeps the raw body and decodes base_resp.
func (r *DriveGatewayResp) UnmarshalJSON(b []byte) error {
	if len(r.Raw) == 0 {
		r.Raw = append(json.RawMessage(nil), b...)
	}
	var aux struct {
		BaseResp *DriveBaseResp `json:"base_resp"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	r.BaseResp = aux.BaseResp
	return nil
}

func validateDriveGateway(name string, req any) error {
	switch name {
	case "ListDeployedWebsites":
		r := req.(ListDeployedWebsitesReq)
		off, ok := wholeNumber(r.Offset)
		if !ok || off < 0 || off > DriveOffsetMax {
			return fmt.Errorf("desktop: offset must be an integer from 0 to %d", DriveOffsetMax)
		}
		lim, ok := wholeNumber(r.Limit)
		if !ok || lim < 1 || lim > 100 {
			return fmt.Errorf("desktop: limit must be an integer from 1 to 100")
		}
		if r.SortBy != nil && *r.SortBy != "deployed_at" && *r.SortBy != "updated_at" {
			return fmt.Errorf("desktop: sort_by must be deployed_at or updated_at")
		}
		if r.SortOrder != nil && *r.SortOrder != "asc" && *r.SortOrder != "desc" {
			return fmt.Errorf("desktop: sort_order must be asc or desc")
		}
	case "GetWebsiteTrafficStats":
		r := req.(GetWebsiteTrafficStatsReq)
		if err := requireNode(r.NodeID); err != nil {
			return err
		}
		days, ok := wholeNumber(r.Days)
		if !ok || (days != 7 && days != 30) {
			return fmt.Errorf("desktop: days must be 7 or 30")
		}
		if r.Granularity != nil && *r.Granularity != "hour" && *r.Granularity != "day" {
			return fmt.Errorf("desktop: granularity must be hour or day")
		}
	case "UpdateWebsiteWatermark":
		r := req.(UpdateWebsiteWatermarkReq)
		if err := requireNode(r.NodeID); err != nil {
			return err
		}
	case "UpdateWebsiteDisplayName":
		r := req.(UpdateWebsiteDisplayNameReq)
		if err := requireNode(r.NodeID); err != nil {
			return err
		}
		if r.DisplayName == nil || strings.TrimSpace(*r.DisplayName) == "" {
			return fmt.Errorf("desktop: display_name must be a non-empty string")
		}
	case "SetWebsiteAlias", "CheckWebsiteAliasAvailability", "UnbindWebsiteAlias":
		if err := requireAlias(req); err != nil {
			return err
		}
	default:
		if node, ok := nodeID(req); ok {
			return requireNode(node)
		}
	}
	return nil
}

func requireAlias(req any) error {
	switch r := req.(type) {
	case SetWebsiteAliasReq:
		if err := requireNode(r.NodeID); err != nil {
			return err
		}
		if strings.TrimSpace(r.Alias) == "" {
			return fmt.Errorf("desktop: alias must be a non-empty string")
		}
	case CheckWebsiteAliasAvailabilityReq:
		if err := requireNode(r.NodeID); err != nil {
			return err
		}
		if r.Alias == nil || strings.TrimSpace(*r.Alias) == "" {
			return fmt.Errorf("desktop: alias must be a non-empty string")
		}
	case UnbindWebsiteAliasReq:
		if err := requireNode(r.NodeID); err != nil {
			return err
		}
		if r.Alias == nil || strings.TrimSpace(*r.Alias) == "" {
			return fmt.Errorf("desktop: alias must be a non-empty string")
		}
	default:
		return fmt.Errorf("desktop: unexpected alias request %T", req)
	}
	return nil
}

func nodeID(req any) (string, bool) {
	switch r := req.(type) {
	case GetWebsiteStateReq:
		return r.NodeID, true
	case CancelWebsiteDeploymentReq:
		return r.NodeID, true
	case UnpublishWebsiteDeploymentReq:
		return r.NodeID, true
	case RepublishWebsiteDeploymentReq:
		return r.NodeID, true
	case RemoveWebsiteAliasReq:
		return r.NodeID, true
	case GetWebsiteProjectSourceDownloadURLReq:
		return r.NodeID, true
	default:
		return "", false
	}
}

func requireNode(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("desktop: node_id is required")
	}
	return nil
}

func wholeNumber(n Number) (int64, bool) {
	f := float64(n)
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
		return 0, false
	}
	return int64(f), true
}

func parseDecimalInt(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, ".eE") {
		return 0, false
	}
	var n int64
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}
