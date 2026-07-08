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
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
)

const (
	chaincodeOperationInvoke = "invoke"
	chaincodeOperationQuery  = "query"

	operationAdminMSPPath = "/fabricops/operation/admin-msp"
	operationAdminTLSPath = "/fabricops/operation/admin-tls"
	operationTLSRootPath  = "/fabricops/operation/tls-roots"

	mspConfigKey    = "config.yaml"
	mspCACertKey    = "cacert.pem"
	mspTLSCACertKey = "tlscacert.pem"
	mspSignCertKey  = "signcert.pem"
	mspKeyStoreKey  = "keystore.pem"

	tlsCACertKey     = "ca.crt"
	tlsClientCertKey = "client.crt"
	tlsClientKeyKey  = "client.key"
)

type chaincodeOperationOptions struct {
	kube         kubeOptions
	channel      string
	chaincode    string
	org          string
	peers        stringListFlag
	function     string
	argsJSON     string
	transient    string
	timeout      string
	waitForEvent bool
	keepJob      bool
}

type operationPeerTarget struct {
	orgName  string
	status   fabricopsv1alpha1.OrgStatus
	endpoint fabricopsv1alpha1.PeerEndpointStatus
}

func runChaincodeOperation(args []string, stdout, stderr io.Writer, operation string) error {
	var options chaincodeOperationOptions
	flags := flag.NewFlagSet(operation, flag.ContinueOnError)
	flags.SetOutput(stderr)
	bindKubeFlags(flags, &options.kube)
	flags.StringVar(&options.channel, "channel", "", "Channel name")
	flags.StringVar(&options.chaincode, "chaincode", "", "Chaincode name")
	flags.StringVar(&options.org, "org", "", "Submitting peer organization")
	flags.Var(&options.peers, "peer", "Target peer as Org/peer or peer; repeat for multi-org endorsement")
	flags.StringVar(&options.function, "function", "", "Chaincode function")
	flags.StringVar(&options.argsJSON, "args", "[]", "JSON array of string arguments")
	flags.StringVar(&options.transient, "transient", "", "Raw transient JSON for invoke operations")
	flags.StringVar(&options.timeout, "timeout", "180s", "How long to wait for the operation Job")
	flags.BoolVar(&options.waitForEvent, "wait-for-event", true, "Pass --waitForEvent to invoke")
	flags.BoolVar(&options.keepJob, "keep-job", false, "Keep the operation Job and temporary Secret")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		printLine(stderr, "Usage: fabricopsctl "+operation+" [flags] <fabricnetwork>")
		return errUsage
	}
	if err := validateOperationOptions(operation, options); err != nil {
		return err
	}

	return runChaincodeOperationWithOptions(context.Background(), flags.Arg(0), operation, options, stdout)
}

func validateOperationOptions(operation string, options chaincodeOperationOptions) error {
	missing := []string{}
	if options.channel == "" {
		missing = append(missing, "--channel")
	}
	if options.chaincode == "" {
		missing = append(missing, "--chaincode")
	}
	if options.function == "" {
		missing = append(missing, "--function")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	if operation == chaincodeOperationQuery && options.transient != "" {
		return fmt.Errorf("--transient is only supported for invoke")
	}
	return nil
}

func runChaincodeOperationWithOptions(
	ctx context.Context,
	networkName string,
	operation string,
	options chaincodeOperationOptions,
	stdout io.Writer,
) error {
	timeout, err := time.ParseDuration(options.timeout)
	if err != nil {
		return err
	}
	network, err := getFabricNetwork(ctx, options.kube, networkName)
	if err != nil {
		return err
	}
	args, err := parseChaincodeArgs(options.argsJSON)
	if err != nil {
		return err
	}
	payload, err := chaincodePayload(options.function, args)
	if err != nil {
		return err
	}
	targets, submitter, err := selectOperationTargets(network.Status.OrgStatus, options.org, options.peers)
	if err != nil {
		return err
	}
	if err := validateSelectedOperationTargets(operation, submitter, targets); err != nil {
		return err
	}
	mspID, err := mspIDForOrg(network, submitter.Name)
	if err != nil {
		return err
	}
	orderer, err := selectOperationOrderer(network.Status.OrgStatus)
	if err != nil {
		return err
	}

	ctrlClient, err := newClient(options.kube)
	if err != nil {
		return err
	}
	restConfig, err := newRESTConfig(options.kube)
	if err != nil {
		return err
	}
	kubeClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	jobName := operationJobName(network.Name, operation)
	tlsSecretName := jobName + "-tls-roots"
	tlsEnabled := network.Spec.Global.TLS
	if tlsEnabled {
		if err := ensureOperationTLSSecret(ctx, ctrlClient, tlsSecretName, submitter, orderer, targets); err != nil {
			return err
		}
	}

	job := buildOperationJob(
		network,
		operation,
		options,
		payload,
		jobName,
		tlsSecretName,
		mspID,
		submitter,
		orderer,
		targets,
	)
	if err := ctrlClient.Create(ctx, job); err != nil {
		return err
	}

	completed, err := waitForOperationJob(ctx, ctrlClient, job.Namespace, job.Name, timeout)
	logErr := printOperationLogs(ctx, kubeClient, stdout, job.Namespace, job.Name)
	if err != nil {
		return err
	}
	if logErr != nil {
		return logErr
	}
	if !completed {
		return fmt.Errorf("operation job %s/%s failed", job.Namespace, job.Name)
	}
	if !options.keepJob {
		cleanupOperationObjects(ctx, ctrlClient, job, tlsEnabled, tlsSecretName)
	}

	return nil
}

func parseChaincodeArgs(argsJSON string) ([]string, error) {
	var args []string
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, fmt.Errorf("--args must be a JSON array of strings: %w", err)
	}
	return args, nil
}

func chaincodePayload(function string, args []string) (string, error) {
	payloadArgs := append([]string{function}, args...)
	payload := map[string][]string{"Args": payloadArgs}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func selectOperationTargets(
	statuses []fabricopsv1alpha1.OrgStatus,
	orgName string,
	peerSelectors []string,
) ([]operationPeerTarget, fabricopsv1alpha1.OrgStatus, error) {
	submitter, err := selectOperationOrg(statuses, orgName)
	if err != nil {
		return nil, fabricopsv1alpha1.OrgStatus{}, err
	}
	if len(peerSelectors) == 0 {
		if len(submitter.PeerEndpoints) != 1 {
			return nil, submitter, fmt.Errorf("org %q has %d peers; pass --peer", submitter.Name, len(submitter.PeerEndpoints))
		}
		return []operationPeerTarget{{
			orgName:  submitter.Name,
			status:   submitter,
			endpoint: submitter.PeerEndpoints[0],
		}}, submitter, nil
	}

	targets := make([]operationPeerTarget, 0, len(peerSelectors))
	for _, selector := range peerSelectors {
		target, err := selectOperationPeer(statuses, submitter.Name, selector)
		if err != nil {
			return nil, submitter, err
		}
		targets = append(targets, target)
	}
	if !targetsIncludeOrg(targets, submitter.Name) {
		return nil, submitter, fmt.Errorf("at least one --peer must belong to submitting org %q", submitter.Name)
	}
	return targets, submitter, nil
}

func validateSelectedOperationTargets(
	operation string,
	submitter fabricopsv1alpha1.OrgStatus,
	targets []operationPeerTarget,
) error {
	if len(targets) == 0 {
		return errors.New("no peer targets were selected")
	}
	if operation == chaincodeOperationQuery {
		if len(targets) != 1 {
			return errors.New("query supports exactly one --peer")
		}
		if !strings.EqualFold(targets[0].orgName, submitter.Name) {
			return fmt.Errorf("query peer %q must belong to submitting org %q", targets[0].endpoint.Name, submitter.Name)
		}
	}
	return nil
}

func selectOperationOrg(statuses []fabricopsv1alpha1.OrgStatus, orgName string) (fabricopsv1alpha1.OrgStatus, error) {
	if orgName != "" {
		for _, status := range statuses {
			if strings.EqualFold(status.Name, orgName) {
				if len(status.PeerEndpoints) == 0 {
					return fabricopsv1alpha1.OrgStatus{}, fmt.Errorf("org %q has no peer endpoints", status.Name)
				}
				return status, nil
			}
		}
		return fabricopsv1alpha1.OrgStatus{}, fmt.Errorf("org %q was not found in FabricNetwork status", orgName)
	}

	matches := []fabricopsv1alpha1.OrgStatus{}
	for _, status := range statuses {
		if len(status.PeerEndpoints) > 0 {
			matches = append(matches, status)
		}
	}
	if len(matches) != 1 {
		return fabricopsv1alpha1.OrgStatus{}, fmt.Errorf("found %d peer orgs; pass --org", len(matches))
	}
	return matches[0], nil
}

func selectOperationPeer(
	statuses []fabricopsv1alpha1.OrgStatus,
	defaultOrg string,
	selector string,
) (operationPeerTarget, error) {
	orgName := defaultOrg
	peerName := selector
	if strings.Contains(selector, "/") {
		parts := strings.Split(selector, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return operationPeerTarget{}, fmt.Errorf("peer selector %q must use Org/peer", selector)
		}
		orgName = parts[0]
		peerName = parts[1]
	}

	for _, status := range statuses {
		if !strings.EqualFold(status.Name, orgName) {
			continue
		}
		for _, endpoint := range status.PeerEndpoints {
			if endpoint.Name == peerName {
				return operationPeerTarget{orgName: status.Name, status: status, endpoint: endpoint}, nil
			}
		}
		return operationPeerTarget{}, fmt.Errorf("peer %q was not found in org %q", peerName, status.Name)
	}
	return operationPeerTarget{}, fmt.Errorf("org %q was not found in FabricNetwork status", orgName)
}

func targetsIncludeOrg(targets []operationPeerTarget, orgName string) bool {
	for _, target := range targets {
		if strings.EqualFold(target.orgName, orgName) {
			return true
		}
	}
	return false
}

func selectOperationOrderer(
	statuses []fabricopsv1alpha1.OrgStatus,
) (fabricopsv1alpha1.OrdererEndpointStatus, error) {
	for _, status := range statuses {
		if len(status.OrdererEndpoints) > 0 {
			return status.OrdererEndpoints[0], nil
		}
	}
	return fabricopsv1alpha1.OrdererEndpointStatus{}, fmt.Errorf("no orderer endpoint found in FabricNetwork status")
}

func ensureOperationTLSSecret(
	ctx context.Context,
	client ctrlclient.Client,
	name string,
	submitter fabricopsv1alpha1.OrgStatus,
	orderer fabricopsv1alpha1.OrdererEndpointStatus,
	targets []operationPeerTarget,
) error {
	data := map[string][]byte{}
	ordererNamespace, err := endpointNamespace(orderer.ClientAddress)
	if err != nil {
		return err
	}
	ordererRoot, err := loadSecretKey(ctx, client, ordererNamespace, orderer.Name+"-tls", tlsCACertKey)
	if err != nil {
		return err
	}
	data["orderer-ca.crt"] = ordererRoot

	for i, target := range targets {
		root, err := loadSecretKey(ctx, client, target.status.Namespace, target.endpoint.Name+"-tls", tlsCACertKey)
		if err != nil {
			return err
		}
		data[fmt.Sprintf("peer-%d-ca.crt", i)] = root
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: submitter.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":      "fabricops",
				"app.kubernetes.io/component": "operation",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
	return client.Create(ctx, secret)
}

func loadSecretKey(ctx context.Context, client ctrlclient.Client, namespace, name, key string) ([]byte, error) {
	var secret corev1.Secret
	if err := client.Get(ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: name}, &secret); err != nil {
		return nil, err
	}
	value := secret.Data[key]
	if len(value) == 0 {
		return nil, fmt.Errorf("secret %s/%s is missing %s", namespace, name, key)
	}
	return value, nil
}

func buildOperationJob(
	network *fabricopsv1alpha1.FabricNetwork,
	operation string,
	options chaincodeOperationOptions,
	payload string,
	jobName string,
	tlsSecretName string,
	mspID string,
	submitter fabricopsv1alpha1.OrgStatus,
	orderer fabricopsv1alpha1.OrdererEndpointStatus,
	targets []operationPeerTarget,
) *batchv1.Job {
	backoffLimit := int32(0)
	script := operationScript(network.Spec.Global.TLS, options.waitForEvent, len(targets))
	labels := map[string]string{
		"app.kubernetes.io/name":      "fabricops",
		"app.kubernetes.io/component": "operation",
		"fabricops.io/fabricnetwork":  network.Name,
	}
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: submitter.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:         operation,
							Image:        fabricToolsImage(network.Spec.Global.FabricVersion),
							Env:          operationEnv(operation, options, payload, mspID, submitter, orderer, targets),
							Command:      []string{"sh", "-ec", script},
							VolumeMounts: operationVolumeMounts(network.Spec.Global.TLS, len(targets)),
						},
					},
					Volumes: operationVolumes(network.Spec.Global.TLS, submitter, tlsSecretName, len(targets)),
				},
			},
		},
	}
}

func operationEnv(
	operation string,
	options chaincodeOperationOptions,
	payload string,
	mspID string,
	submitter fabricopsv1alpha1.OrgStatus,
	orderer fabricopsv1alpha1.OrdererEndpointStatus,
	targets []operationPeerTarget,
) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "FABRICOPS_OPERATION", Value: operation},
		{Name: "FABRICOPS_CHANNEL", Value: options.channel},
		{Name: "FABRICOPS_CHAINCODE", Value: options.chaincode},
		{Name: "FABRICOPS_PAYLOAD", Value: payload},
		{Name: "FABRICOPS_TRANSIENT", Value: options.transient},
		{Name: "FABRICOPS_MSP_ID", Value: mspID},
		{Name: "FABRICOPS_ORDERER_ADDRESS", Value: orderer.ClientAddress},
		{Name: "FABRICOPS_CORE_PEER_ADDRESS", Value: submitterPeerAddress(submitter.Name, targets)},
		{Name: "FABRICOPS_CORE_PEER_TLS_ROOT", Value: submitterPeerTLSRoot(submitter.Name, targets)},
	}
	for i, target := range targets {
		env = append(env, corev1.EnvVar{
			Name:  fmt.Sprintf("FABRICOPS_PEER_ADDRESS_%d", i),
			Value: target.endpoint.Address,
		})
	}
	return env
}

func operationScript(tlsEnabled bool, waitForEvent bool, peerCount int) string {
	tlsSetup := "export CORE_PEER_TLS_ENABLED=false"
	invokeTLSArgs := ""
	if tlsEnabled {
		tlsSetup = fmt.Sprintf(`export CORE_PEER_TLS_ENABLED=true
export CORE_PEER_TLS_CERT_FILE=%s/client.crt
export CORE_PEER_TLS_KEY_FILE=%s/client.key
export CORE_PEER_TLS_ROOTCERT_FILE="$FABRICOPS_CORE_PEER_TLS_ROOT"`, operationAdminTLSPath, operationAdminTLSPath)
		invokeTLSArgs = fmt.Sprintf(`set -- "$@" --tls --cafile %s/orderer-ca.crt`, operationTLSRootPath)
	}

	waitArg := ""
	if waitForEvent {
		waitArg = `set -- "$@" --waitForEvent`
	}

	peerArgs := operationPeerArgs(tlsEnabled, peerCount)

	return fmt.Sprintf(`set -eu

export CORE_PEER_LOCALMSPID="$FABRICOPS_MSP_ID"
export CORE_PEER_ADDRESS="$FABRICOPS_CORE_PEER_ADDRESS"
export CORE_PEER_MSPCONFIGPATH=%s
%s

if [ "$FABRICOPS_OPERATION" = "query" ]; then
  peer chaincode query \
    -C "$FABRICOPS_CHANNEL" \
    -n "$FABRICOPS_CHAINCODE" \
    -c "$FABRICOPS_PAYLOAD"
  exit 0
fi

set -- peer chaincode invoke \
  -o "$FABRICOPS_ORDERER_ADDRESS" \
  -C "$FABRICOPS_CHANNEL" \
  -n "$FABRICOPS_CHAINCODE" \
  -c "$FABRICOPS_PAYLOAD"
%s
%s

if [ -n "$FABRICOPS_TRANSIENT" ]; then
  set -- "$@" --transient "$FABRICOPS_TRANSIENT"
fi

%s

"$@"
`, operationAdminMSPPath, tlsSetup, invokeTLSArgs, waitArg, peerArgs)
}

func operationPeerArgs(tlsEnabled bool, peerCount int) string {
	var builder strings.Builder
	for i := range peerCount {
		fmt.Fprintf(&builder, "set -- \"$@\" --peerAddresses \"$FABRICOPS_PEER_ADDRESS_%d\"\n", i)
		if tlsEnabled {
			fmt.Fprintf(
				&builder,
				"set -- \"$@\" --tlsRootCertFiles \"%s/peer-%d-ca.crt\"\n",
				operationTLSRootPath,
				i,
			)
		}
	}
	return builder.String()
}

func operationVolumeMounts(tlsEnabled bool, peerCount int) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{Name: "admin-msp", MountPath: operationAdminMSPPath, ReadOnly: true},
	}
	if tlsEnabled {
		mounts = append(mounts,
			corev1.VolumeMount{Name: "admin-tls", MountPath: operationAdminTLSPath, ReadOnly: true},
			corev1.VolumeMount{Name: "tls-roots", MountPath: operationTLSRootPath, ReadOnly: true},
		)
	}
	_ = peerCount
	return mounts
}

func operationVolumes(
	tlsEnabled bool,
	submitter fabricopsv1alpha1.OrgStatus,
	tlsSecretName string,
	peerCount int,
) []corev1.Volume {
	adminName := sanitizeName(submitter.Name + "-admin")
	mspItems := []corev1.KeyToPath{
		{Key: mspConfigKey, Path: mspConfigKey},
		{Key: mspCACertKey, Path: "cacerts/ca.pem"},
		{Key: mspSignCertKey, Path: "signcerts/cert.pem"},
		{Key: mspKeyStoreKey, Path: "keystore/key.pem"},
	}
	if tlsEnabled {
		mspItems = append(mspItems, corev1.KeyToPath{Key: mspTLSCACertKey, Path: "tlscacerts/tlsca.pem"})
	}
	volumes := []corev1.Volume{
		{
			Name: "admin-msp",
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
				SecretName: adminName + "-msp",
				Items:      mspItems,
			}},
		},
	}
	if tlsEnabled {
		volumes = append(volumes,
			corev1.Volume{
				Name: "admin-tls",
				VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
					SecretName: adminName + "-tls",
					Items: []corev1.KeyToPath{
						{Key: tlsClientCertKey, Path: tlsClientCertKey},
						{Key: tlsClientKeyKey, Path: tlsClientKeyKey},
						{Key: tlsCACertKey, Path: tlsCACertKey},
					},
				}},
			},
			corev1.Volume{
				Name: "tls-roots",
				VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
					SecretName: tlsSecretName,
					Items:      tlsRootItems(peerCount),
				}},
			},
		)
	}
	return volumes
}

func tlsRootItems(peerCount int) []corev1.KeyToPath {
	items := []corev1.KeyToPath{{Key: "orderer-ca.crt", Path: "orderer-ca.crt"}}
	for i := range peerCount {
		items = append(items, corev1.KeyToPath{
			Key:  fmt.Sprintf("peer-%d-ca.crt", i),
			Path: fmt.Sprintf("peer-%d-ca.crt", i),
		})
	}
	return items
}

func waitForOperationJob(
	ctx context.Context,
	client ctrlclient.Client,
	namespace string,
	name string,
	timeout time.Duration,
) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		var job batchv1.Job
		if err := client.Get(ctx, ctrlclient.ObjectKey{Namespace: namespace, Name: name}, &job); err != nil {
			return false, err
		}
		if job.Status.Succeeded > 0 {
			return true, nil
		}
		if job.Status.Failed > 0 {
			return false, nil
		}
		if time.Now().After(deadline) {
			return false, fmt.Errorf("timed out waiting for operation job %s/%s", namespace, name)
		}
		time.Sleep(2 * time.Second)
	}
}

func printOperationLogs(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	out io.Writer,
	namespace string,
	jobName string,
) error {
	pods, err := kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found for operation job %s/%s", namespace, jobName)
	}
	logs, err := kubeClient.CoreV1().Pods(namespace).GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{}).Do(ctx).Raw()
	if err != nil {
		return err
	}
	_, err = out.Write(logs)
	if err != nil {
		return err
	}
	if len(logs) > 0 && logs[len(logs)-1] != '\n' {
		printLine(out)
	}
	return nil
}

func cleanupOperationObjects(
	ctx context.Context,
	client ctrlclient.Client,
	job *batchv1.Job,
	tlsEnabled bool,
	tlsSecretName string,
) {
	propagation := metav1.DeletePropagationBackground
	_ = client.Delete(ctx, job, &ctrlclient.DeleteOptions{PropagationPolicy: &propagation})
	if !tlsEnabled {
		return
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tlsSecretName, Namespace: job.Namespace}}
	err := client.Delete(ctx, secret)
	if err != nil && !apierrors.IsNotFound(err) {
		printLine(os.Stderr, "Warning: failed to delete temporary Secret:", err)
	}
}

func submitterPeerAddress(submitterOrg string, targets []operationPeerTarget) string {
	for _, target := range targets {
		if strings.EqualFold(target.orgName, submitterOrg) {
			return target.endpoint.Address
		}
	}
	return targets[0].endpoint.Address
}

func submitterPeerTLSRoot(submitterOrg string, targets []operationPeerTarget) string {
	for i, target := range targets {
		if strings.EqualFold(target.orgName, submitterOrg) {
			return fmt.Sprintf("%s/peer-%d-ca.crt", operationTLSRootPath, i)
		}
	}
	return operationTLSRootPath + "/peer-0-ca.crt"
}

func mspIDForOrg(network *fabricopsv1alpha1.FabricNetwork, orgName string) (string, error) {
	for _, org := range network.Spec.Orgs {
		if strings.EqualFold(org.Organization.Name, orgName) {
			return org.Organization.MSPName, nil
		}
	}
	return "", fmt.Errorf("org %q was not found in FabricNetwork spec", orgName)
}

func endpointNamespace(address string) (string, error) {
	hostPort := strings.Split(address, ":")[0]
	parts := strings.Split(hostPort, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("could not infer namespace from endpoint %q", address)
	}
	return parts[1], nil
}

func operationJobName(networkName string, operation string) string {
	return sanitizeName(fmt.Sprintf("%s-%s-%d", networkName, operation, time.Now().UnixNano()))
}

func fabricToolsImage(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "2.5.12"
	}
	if strings.HasPrefix(strings.TrimPrefix(version, "v"), "3.") {
		version = "2.5.14"
	}

	return fmt.Sprintf("hyperledger/fabric-tools:%s", version)
}

func sanitizeName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}

	value := strings.Trim(b.String(), "-")
	if value == "" {
		value = "resource"
	}
	if len(value) <= 63 {
		return value
	}

	hash := fnv.New32a()
	_, _ = hash.Write([]byte(value))
	suffix := fmt.Sprintf("%08x", hash.Sum32())
	prefix := strings.TrimRight(value[:63-len(suffix)-1], "-")
	if prefix == "" {
		prefix = value[:63-len(suffix)-1]
	}
	return prefix + "-" + suffix
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

var _ flag.Value = (*stringListFlag)(nil)
