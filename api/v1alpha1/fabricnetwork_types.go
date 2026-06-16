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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// FabricNetworkSpec defines the desired state of FabricNetwork
type FabricNetworkSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html
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
	Organisation OrgMeta        `json:"organization"`
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

// FabricNetworkStatus defines the observed state of FabricNetwork.
type FabricNetworkStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the FabricNetwork resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +optional
	Phase   Phase  `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
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
