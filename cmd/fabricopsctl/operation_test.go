/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
)

func TestSelectOperationTargetsSupportsMultiOrgEndorsement(t *testing.T) {
	statuses := operationTestOrgStatuses()

	targets, submitter, err := selectOperationTargets(
		statuses,
		"BankA",
		[]string{"BankA/peer0", "BankB/peer0"},
	)
	if err != nil {
		t.Fatalf("selectOperationTargets() error = %v", err)
	}
	if submitter.Name != "BankA" {
		t.Fatalf("submitter.Name = %q, want BankA", submitter.Name)
	}
	if len(targets) != 2 {
		t.Fatalf("len(targets) = %d, want 2", len(targets))
	}
	if targets[0].endpoint.Address != "peer0.fo-test-banka.svc.cluster.local:7051" {
		t.Fatalf("targets[0].endpoint.Address = %q", targets[0].endpoint.Address)
	}
	if targets[1].endpoint.Address != "peer0.fo-test-bankb.svc.cluster.local:7051" {
		t.Fatalf("targets[1].endpoint.Address = %q", targets[1].endpoint.Address)
	}
}

func TestValidateSelectedOperationTargetsRejectsCrossOrgQuery(t *testing.T) {
	statuses := operationTestOrgStatuses()
	targets, submitter, err := selectOperationTargets(statuses, "BankA", []string{"BankB/peer0", "BankA/peer0"})
	if err != nil {
		t.Fatalf("selectOperationTargets() error = %v", err)
	}

	if err := validateSelectedOperationTargets(chaincodeOperationQuery, submitter, targets); err == nil {
		t.Fatal("validateSelectedOperationTargets() error = nil, want multi-peer query rejection")
	}
}

func TestBuildOperationJobUsesAdminSecretsAndPeerFlags(t *testing.T) {
	network := operationTestNetwork()
	statuses := operationTestOrgStatuses()
	targets, submitter, err := selectOperationTargets(
		statuses,
		"BankA",
		[]string{"BankA/peer0", "BankB/peer0"},
	)
	if err != nil {
		t.Fatalf("selectOperationTargets() error = %v", err)
	}
	mspID, err := mspIDForOrg(network, submitter.Name)
	if err != nil {
		t.Fatalf("mspIDForOrg() error = %v", err)
	}

	job := buildOperationJob(
		network,
		chaincodeOperationInvoke,
		chaincodeOperationOptions{
			channel:      "settlement",
			chaincode:    "settlement",
			function:     "createSettlement",
			waitForEvent: true,
		},
		`{"Args":["createSettlement","id1"]}`,
		"sample-invoke",
		"sample-invoke-tls-roots",
		mspID,
		submitter,
		statuses[0].OrdererEndpoints[0],
		targets,
	)

	if job.Namespace != "fo-test-banka" {
		t.Fatalf("job.Namespace = %q, want fo-test-banka", job.Namespace)
	}
	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != "hyperledger/fabric-tools:2.5.14" {
		t.Fatalf("container.Image = %q, want Fabric v3 tools fallback", container.Image)
	}

	env := operationEnvMap(container.Env)
	if env["FABRICOPS_MSP_ID"] != "BankAMSP" {
		t.Fatalf("FABRICOPS_MSP_ID = %q, want BankAMSP", env["FABRICOPS_MSP_ID"])
	}
	if env["FABRICOPS_CORE_PEER_ADDRESS"] != "peer0.fo-test-banka.svc.cluster.local:7051" {
		t.Fatalf("FABRICOPS_CORE_PEER_ADDRESS = %q", env["FABRICOPS_CORE_PEER_ADDRESS"])
	}
	if env["FABRICOPS_CORE_PEER_TLS_ROOT"] != operationTLSRootPath+"/peer-0-ca.crt" {
		t.Fatalf("FABRICOPS_CORE_PEER_TLS_ROOT = %q", env["FABRICOPS_CORE_PEER_TLS_ROOT"])
	}
	if env["FABRICOPS_PEER_ADDRESS_1"] != "peer0.fo-test-bankb.svc.cluster.local:7051" {
		t.Fatalf("FABRICOPS_PEER_ADDRESS_1 = %q", env["FABRICOPS_PEER_ADDRESS_1"])
	}

	volumes := operationVolumeSecrets(job)
	if volumes["admin-msp"] != "banka-admin-msp" {
		t.Fatalf("admin-msp SecretName = %q, want banka-admin-msp", volumes["admin-msp"])
	}
	if volumes["admin-tls"] != "banka-admin-tls" {
		t.Fatalf("admin-tls SecretName = %q, want banka-admin-tls", volumes["admin-tls"])
	}
	if volumes["tls-roots"] != "sample-invoke-tls-roots" {
		t.Fatalf("tls-roots SecretName = %q, want sample-invoke-tls-roots", volumes["tls-roots"])
	}

	script := container.Command[2]
	for _, want := range []string{
		`set -- "$@" --peerAddresses "$FABRICOPS_PEER_ADDRESS_0"`,
		`set -- "$@" --peerAddresses "$FABRICOPS_PEER_ADDRESS_1"`,
		`set -- "$@" --tlsRootCertFiles "` + operationTLSRootPath + `/peer-0-ca.crt"`,
		`set -- "$@" --tlsRootCertFiles "` + operationTLSRootPath + `/peer-1-ca.crt"`,
		`set -- "$@" --waitForEvent`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("operation script does not contain %q\nscript:\n%s", want, script)
		}
	}
}

func TestParseChaincodeArgsRequiresStringArray(t *testing.T) {
	if _, err := parseChaincodeArgs(`["id1","alice"]`); err != nil {
		t.Fatalf("parseChaincodeArgs() error = %v", err)
	}
	if _, err := parseChaincodeArgs(`["id1",100]`); err == nil {
		t.Fatal("parseChaincodeArgs() error = nil, want non-string rejection")
	}
}

func TestWriteOperationResultSupportsJSONOutput(t *testing.T) {
	var out bytes.Buffer
	result := operationResult{
		Operation: chaincodeOperationQuery,
		Network:   "sample",
		Namespace: "default",
		Channel:   "settlement",
		Chaincode: "settlement",
		Function:  "readSettlement",
		Org:       "BankA",
		Job: operationResultJob{
			Namespace: "fo-test-banka",
			Name:      "sample-query",
		},
		Peers: []operationResultPeer{
			{Org: "BankA", Name: "peer0", Address: "peer0.fo-test-banka.svc.cluster.local:7051"},
		},
		Succeeded:   true,
		JobRetained: false,
		Logs:        "query response\n",
	}

	if err := writeOperationResult(&out, operationOutputJSON, result); err != nil {
		t.Fatalf("writeOperationResult() error = %v", err)
	}

	var decoded operationResult
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; output = %s", err, out.String())
	}
	if decoded.Operation != chaincodeOperationQuery {
		t.Fatalf("decoded.Operation = %q, want %q", decoded.Operation, chaincodeOperationQuery)
	}
	if decoded.Job.Name != "sample-query" {
		t.Fatalf("decoded.Job.Name = %q, want sample-query", decoded.Job.Name)
	}
	if decoded.Logs != "query response\n" {
		t.Fatalf("decoded.Logs = %q, want query response newline", decoded.Logs)
	}
	if decoded.JobRetained {
		t.Fatal("decoded.JobRetained = true, want false")
	}
}

func TestBuildOperationResultMarksFailedJobRetained(t *testing.T) {
	network := operationTestNetwork()
	statuses := operationTestOrgStatuses()
	targets, submitter, err := selectOperationTargets(statuses, "BankA", []string{"peer0"})
	if err != nil {
		t.Fatalf("selectOperationTargets() error = %v", err)
	}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "sample-query", Namespace: "fo-test-banka"}}

	result := buildOperationResult(
		chaincodeOperationQuery,
		network,
		chaincodeOperationOptions{
			channel:   "settlement",
			chaincode: "settlement",
			function:  "readSettlement",
		},
		job,
		submitter,
		targets,
		false,
		[]byte("failed\n"),
	)

	if result.Succeeded {
		t.Fatal("result.Succeeded = true, want false")
	}
	if !result.JobRetained {
		t.Fatal("result.JobRetained = false, want true for failed operation")
	}
	if result.Peers[0].Name != "peer0" {
		t.Fatalf("result.Peers[0].Name = %q, want peer0", result.Peers[0].Name)
	}
}

func TestValidateOperationOutputRejectsUnsupportedFormat(t *testing.T) {
	if err := validateOperationOutput(operationOutputJSON); err != nil {
		t.Fatalf("validateOperationOutput(json) error = %v", err)
	}
	if err := validateOperationOutput("yaml"); err == nil {
		t.Fatal("validateOperationOutput(yaml) error = nil, want rejection")
	}
}

func operationTestNetwork() *fabricopsv1alpha1.FabricNetwork {
	return &fabricopsv1alpha1.FabricNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "sample", Namespace: "default"},
		Spec: fabricopsv1alpha1.FabricNetworkSpec{
			Global: fabricopsv1alpha1.GlobalConfig{
				FabricVersion: "3.0.0",
				TLS:           true,
			},
			Orgs: []fabricopsv1alpha1.Org{
				{Organization: fabricopsv1alpha1.OrgMeta{Name: "OrdererOrg", MSPName: "OrdererMSP"}},
				{Organization: fabricopsv1alpha1.OrgMeta{Name: "BankA", MSPName: "BankAMSP"}},
				{Organization: fabricopsv1alpha1.OrgMeta{Name: "BankB", MSPName: "BankBMSP"}},
			},
		},
	}
}

func operationTestOrgStatuses() []fabricopsv1alpha1.OrgStatus {
	return []fabricopsv1alpha1.OrgStatus{
		{
			Name:      "OrdererOrg",
			Namespace: "fo-test-orderer",
			OrdererEndpoints: []fabricopsv1alpha1.OrdererEndpointStatus{
				{Name: "orderer0", ClientAddress: "orderer0.fo-test-orderer.svc.cluster.local:7050"},
			},
		},
		{
			Name:      "BankA",
			Namespace: "fo-test-banka",
			PeerEndpoints: []fabricopsv1alpha1.PeerEndpointStatus{
				{Name: "peer0", Address: "peer0.fo-test-banka.svc.cluster.local:7051"},
				{Name: "peer1", Address: "peer1.fo-test-banka.svc.cluster.local:7051"},
			},
		},
		{
			Name:      "BankB",
			Namespace: "fo-test-bankb",
			PeerEndpoints: []fabricopsv1alpha1.PeerEndpointStatus{
				{Name: "peer0", Address: "peer0.fo-test-bankb.svc.cluster.local:7051"},
			},
		},
	}
}

func operationEnvMap(env []corev1.EnvVar) map[string]string {
	values := map[string]string{}
	for _, item := range env {
		values[item.Name] = item.Value
	}
	return values
}

func operationVolumeSecrets(job *batchv1.Job) map[string]string {
	values := map[string]string{}
	for _, volume := range job.Spec.Template.Spec.Volumes {
		if volume.Secret != nil {
			values[volume.Name] = volume.Secret.SecretName
		}
	}
	return values
}
