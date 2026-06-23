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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fabricopsv1alpha1 "github.com/dpereowei/fabricops/api/v1alpha1"
)

const (
	chaincodeMetadataKey       = "metadata.json"
	chaincodeConnectionKey     = "connection.json"
	chaincodePackageLabelKey   = "packageLabel"
	chaincodePackageFileKey    = "packageFile"
	chaincodeConnectionAddrKey = "address"
)

type chaincodePackageMetadata struct {
	Type  string `json:"type"`
	Label string `json:"label"`
}

type chaincodeConnection struct {
	Address     string `json:"address"`
	DialTimeout string `json:"dial_timeout"`
	TLSRequired bool   `json:"tls_required"`
}

func (r *FabricNetworkReconciler) reconcileChaincodes(
	ctx context.Context,
	net *fabricopsv1alpha1.FabricNetwork,
	channelStatuses []fabricopsv1alpha1.ChannelStatus,
) ([]fabricopsv1alpha1.ChaincodeStatus, error) {
	statuses := make([]fabricopsv1alpha1.ChaincodeStatus, 0, len(net.Spec.Chaincodes))
	channels := channelsByName(net)
	orgs := orgsByName(net)
	seen := map[string]struct{}{}

	for _, chaincode := range net.Spec.Chaincodes {
		status := fabricopsv1alpha1.ChaincodeStatus{
			Name:         chaincode.Name,
			Channel:      chaincode.Channel,
			Version:      chaincode.Version,
			PackageLabel: chaincodePackageLabel(chaincode),
			Sequence:     chaincodeSequence(chaincode),
		}
		messages := []string{}
		channelReady := false

		key := chaincode.Channel + "/" + chaincode.Name
		if _, ok := seen[key]; ok {
			messages = append(messages, "Chaincode name must be unique within a channel")
		}
		seen[key] = struct{}{}

		if strings.TrimSpace(chaincode.Name) == "" {
			messages = append(messages, "Chaincode name is required")
		}
		if strings.TrimSpace(chaincode.Version) == "" {
			messages = append(messages, "Chaincode version is required")
		}
		if strings.TrimSpace(chaincode.Image) == "" {
			messages = append(messages, "Chaincode image is required for CCaaS")
		}

		channel, ok := channels[chaincode.Channel]
		if strings.TrimSpace(chaincode.Channel) == "" {
			messages = append(messages, "Chaincode channel is required")
		} else if !ok {
			messages = append(messages, "Unknown channel")
		} else {
			channelReady = channelReadyForChaincode(channelStatuses, chaincode.Channel)
		}

		if ok {
			for _, channelOrg := range channel.Orgs {
				org, orgKnown := orgs[channelOrg.Name]
				for _, peerName := range channelOrg.Peers {
					target := fabricopsv1alpha1.ChaincodeTargetStatus{
						OrgName:  channelOrg.Name,
						PeerName: peerName,
					}
					status.PackageMetadata.Desired++

					if !orgKnown {
						target.Message = "Unknown org"
						status.Targets = append(status.Targets, target)
						continue
					}

					target.Namespace = orgNamespaceName(net, org)
					target.ServiceName = chaincodeServiceName(chaincode, org, peerName)
					target.Address = chaincodeConnectionAddress(target.ServiceName, target.Namespace, chaincode)
					target.PackageConfigMapName = chaincodePackageConfigMapName(chaincode, org, peerName)

					if !peerDeclared(org, peerName) {
						target.Message = "Unknown peer"
						status.Targets = append(status.Targets, target)
						continue
					}
					if len(messages) > 0 {
						target.Message = "Waiting for valid chaincode configuration"
						status.Targets = append(status.Targets, target)
						continue
					}

					configMap, err := buildChaincodePackageConfigMap(net, chaincode, org, peerName)
					if err != nil {
						return statuses, err
					}
					if err := r.ensureConfigMap(ctx, configMap); err != nil {
						return statuses, err
					}

					target.PackageMetadataReady = true
					target.Message = "Package metadata generated"
					status.PackageMetadata.Ready++
					status.Targets = append(status.Targets, target)
				}
			}
		}

		status.PackageMetadataReady = status.PackageMetadata.Desired > 0 &&
			status.PackageMetadata.Ready >= status.PackageMetadata.Desired
		status.Message = chaincodeStatusMessage(status, messages, channelReady)
		statuses = append(statuses, status)
	}

	return statuses, nil
}

func buildChaincodePackageConfigMap(
	net *fabricopsv1alpha1.FabricNetwork,
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
) (*corev1.ConfigMap, error) {
	label := chaincodePackageLabel(chaincode)
	address := chaincodeConnectionAddress(chaincodeServiceName(chaincode, org, peerName), orgNamespaceName(net, org), chaincode)

	metadataJSON, err := marshalChaincodeJSON(chaincodePackageMetadata{
		Type:  "ccaas",
		Label: label,
	})
	if err != nil {
		return nil, err
	}

	connectionJSON, err := marshalChaincodeJSON(chaincodeConnection{
		Address:     address,
		DialTimeout: chaincodeDialTimeout(chaincode),
		TLSRequired: false,
	})
	if err != nil {
		return nil, err
	}

	labels := chaincodeLabels(net, org, chaincode.Channel, chaincode.Name)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:        chaincodePackageConfigMapName(chaincode, org, peerName),
			Namespace:   orgNamespaceName(net, org),
			Labels:      labels,
			Annotations: resourceAnnotations(net, org),
		},
		Data: map[string]string{
			chaincodeMetadataKey:       metadataJSON,
			chaincodeConnectionKey:     connectionJSON,
			chaincodePackageLabelKey:   label,
			chaincodePackageFileKey:    label + ".tar.gz",
			chaincodeConnectionAddrKey: address,
		},
	}, nil
}

func marshalChaincodeJSON(value any) (string, error) {
	bytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(bytes) + "\n", nil
}

func chaincodeStatusMessage(status fabricopsv1alpha1.ChaincodeStatus, messages []string, channelReady bool) string {
	if len(messages) > 0 {
		return strings.Join(messages, "; ")
	}
	if status.PackageMetadata.Desired == 0 {
		return "No target peers found"
	}
	if status.PackageMetadataReady && !channelReady {
		return "Package metadata generated; waiting for channel bootstrap before lifecycle install"
	}
	if status.PackageMetadataReady {
		return "Package metadata generated; waiting for chaincode lifecycle install"
	}
	return "Waiting for package metadata generation"
}

func chaincodeStatusesEqual(a, b []fabricopsv1alpha1.ChaincodeStatus) bool {
	return reflect.DeepEqual(a, b)
}

func channelsByName(net *fabricopsv1alpha1.FabricNetwork) map[string]fabricopsv1alpha1.Channel {
	channels := map[string]fabricopsv1alpha1.Channel{}
	for _, channel := range net.Spec.Channels {
		channels[channel.Name] = channel
	}
	return channels
}

func channelReadyForChaincode(statuses []fabricopsv1alpha1.ChannelStatus, channelName string) bool {
	for _, status := range statuses {
		if status.Name == channelName {
			return status.Ready
		}
	}
	return false
}

func peerDeclared(org fabricopsv1alpha1.Org, peerName string) bool {
	if org.Peer == nil {
		return false
	}

	for i := 0; i < org.Peer.Instances; i++ {
		if sanitizeName(fmt.Sprintf("%s%d", org.Peer.Prefix, i)) == peerName {
			return true
		}
	}
	return false
}

func chaincodePackageLabel(chaincode fabricopsv1alpha1.Chaincode) string {
	if strings.TrimSpace(chaincode.PackageLabel) != "" {
		return chaincode.PackageLabel
	}
	return fmt.Sprintf("%s_%s_%s", chaincode.Channel, chaincode.Name, chaincode.Version)
}

func chaincodeSequence(chaincode fabricopsv1alpha1.Chaincode) int32 {
	if chaincode.Sequence > 0 {
		return chaincode.Sequence
	}
	return 1
}

func chaincodeDialTimeout(chaincode fabricopsv1alpha1.Chaincode) string {
	if chaincode.CCAAS != nil && strings.TrimSpace(chaincode.CCAAS.DialTimeout) != "" {
		return chaincode.CCAAS.DialTimeout
	}
	return "10s"
}

func chaincodeServicePort(chaincode fabricopsv1alpha1.Chaincode) int32 {
	if chaincode.CCAAS != nil && chaincode.CCAAS.ServicePort > 0 {
		return chaincode.CCAAS.ServicePort
	}
	return peerChaincodePort
}

func chaincodeServiceName(chaincode fabricopsv1alpha1.Chaincode, org fabricopsv1alpha1.Org, peerName string) string {
	return sanitizeName(fmt.Sprintf("%s-%s-%s-%s-ccaas", chaincode.Channel, chaincode.Name, org.Organization.Name, peerName))
}

func chaincodeConnectionAddress(serviceName string, namespace string, chaincode fabricopsv1alpha1.Chaincode) string {
	return serviceDNS(serviceName, namespace, chaincodeServicePort(chaincode))
}

func chaincodePackageConfigMapName(
	chaincode fabricopsv1alpha1.Chaincode,
	org fabricopsv1alpha1.Org,
	peerName string,
) string {
	return sanitizeName(fmt.Sprintf("%s-%s-%s-%s-package", chaincode.Channel, chaincode.Name, org.Organization.Name, peerName))
}

func chaincodeLabels(
	net *fabricopsv1alpha1.FabricNetwork,
	org fabricopsv1alpha1.Org,
	channelName string,
	chaincodeName string,
) map[string]string {
	return mergeMap(orgLabels(net, org, componentChaincode), map[string]string{
		labelChannel:   sanitizeName(channelName),
		labelChaincode: sanitizeName(chaincodeName),
	})
}
