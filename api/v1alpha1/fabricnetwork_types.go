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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// FabricNetworkSpec defines the desired state of FabricNetwork
type FabricNetworkSpec struct {
	Global GlobalConfig `json:"global"`

	Orgs []Org `json:"orgs"`

	Channels []Channel `json:"channels"`

	Chaincodes []Chaincode `json:"chaincodes"`
}

type GlobalConfig struct {
	FabricVersion string `json:"fabricVersion"`
	TLS           bool   `json:"tls"`
}

type Org struct {
	Organization OrgMeta        `json:"organization"`
	CA           CAConfig       `json:"ca"`
	Orderers     []OrdererGroup `json:"orderers,omitempty"`
	Peer         *PeerConfig    `json:"peer,omitempty"`
}

type OrgMeta struct {
	Name    string `json:"name"`
	Domain  string `json:"domain"`
	MSPName string `json:"mspName"`
}

type CAConfig struct {
	DB string `json:"db"`
}

type OrdererGroup struct {
	GroupName string `json:"groupName"`
	Type      string `json:"type"`
	Instances int    `json:"instances"`
	Prefix    string `json:"prefix"`
}

type PeerConfig struct {
	Instances int    `json:"instances"`
	DB        string `json:"db"`
	Prefix    string `json:"prefix"`
}

type Channel struct {
	Name string `json:"name"`

	Orgs []ChannelOrg `json:"orgs"`
}

type ChannelOrg struct {
	Name  string   `json:"name"`
	Peers []string `json:"peers"`
}

type Chaincode struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Channel string `json:"channel"`
	Image   string `json:"image"`
}

type WorkloadStatus struct {
	Desired int32 `json:"desired"`
	Ready   int32 `json:"ready"`
}

type OrgStatus struct {
	Name          string         `json:"name"`
	Namespace     string         `json:"namespace,omitempty"`
	IdentityReady bool           `json:"identityReady"`
	IdentityError string         `json:"identityError,omitempty"`
	CAReady       bool           `json:"caReady"`
	Orderers      WorkloadStatus `json:"orderers,omitempty"`
	OrderersReady bool           `json:"orderersReady"`
	Peers         WorkloadStatus `json:"peers,omitempty"`
	PeersReady    bool           `json:"peersReady"`
	Ready         bool           `json:"ready"`
}

// FabricNetworkStatus defines the observed state of FabricNetwork.
type FabricNetworkStatus struct {
	// +optional
	Phase     Phase       `json:"phase,omitempty"`
	Message   string      `json:"message,omitempty"`
	OrgStatus []OrgStatus `json:"orgStatus,omitempty"`

	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

type Phase string

const (
	PhasePending  Phase = "Pending"
	PhaseCreating Phase = "Creating"
	PhaseReady    Phase = "Ready"
	PhaseFailed   Phase = "Failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// FabricNetwork is the Schema for the fabricnetworks API
type FabricNetwork struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of FabricNetwork
	// +required
	Spec FabricNetworkSpec `json:"spec"`

	// status defines the observed state of FabricNetwork
	// +optional
	Status FabricNetworkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FabricNetworkList contains a list of FabricNetwork
type FabricNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []FabricNetwork `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &FabricNetwork{}, &FabricNetworkList{})
		return nil
	})
}
