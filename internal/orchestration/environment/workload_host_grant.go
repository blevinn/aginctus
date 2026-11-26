package environment

import (
	"encoding/json"
)

// WorkloadHostGrant is independently installed by an administrator for one
// exact server/project and attachment. It is not part of accepted intent.
type WorkloadHostGrant struct {
	APIVersion      string `json:"apiVersion"`
	ServerID        string `json:"serverId"`
	Project         string `json:"project"`
	EnvironmentID   string `json:"environmentId"`
	WorkloadID      string `json:"workloadId"`
	Instance        string `json:"instance"`
	Pool            string `json:"pool"`
	Bridge          string `json:"bridge"`
	IPv4Address     string `json:"ipv4Address"`
	IPv4NAT         bool   `json:"ipv4Nat"`
	IPv4Routing     bool   `json:"ipv4Routing"`
	IPv4DHCPGateway string `json:"ipv4DhcpGateway"`
	IPv6Address     string `json:"ipv6Address"`
}

func ReadWorkloadHostGrant(path string) (WorkloadHostGrant, error) {
	var grant WorkloadHostGrant
	data, err := readAdministratorPolicy(path)
	if err != nil {
		return grant, errProjectExecutionDenied
	}
	var members map[string]json.RawMessage
	if rejectCandidateDuplicateKeys(data) != nil || json.Unmarshal(data, &members) != nil || candidateKeys(members,
		"apiVersion", "serverId", "project", "environmentId", "workloadId", "instance", "pool", "bridge",
		"ipv4Address", "ipv4Nat", "ipv4Routing", "ipv4DhcpGateway", "ipv6Address") != nil ||
		json.Unmarshal(data, &grant) != nil || grant.APIVersion != "aginctus.dev/workload-host-grant/v1alpha1" {
		return WorkloadHostGrant{}, errProjectExecutionDenied
	}
	return grant, nil
}
