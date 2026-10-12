package service

import (
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// AppendUpstreamResponseLogInfo shares the same attempt facts between consume
// and error logs without changing their accounting or error-mapping semantics.
func AppendUpstreamResponseLogInfo(info *relaycommon.RelayInfo, other *model.LogOther) {
	if info == nil || other == nil {
		return
	}
	other.SetPublic("request_model", info.OriginModelName)
	other.SetPublic("request_reasoning_effort", info.RequestReasoningEffort)
	response := info.UpstreamResponse
	if response == nil {
		return
	}
	other.SetPublic("upstream_request_status", response.RequestStatus())
	if response.StatusCode != 0 {
		other.SetPublic("upstream_status_code", response.StatusCode)
	}
}
