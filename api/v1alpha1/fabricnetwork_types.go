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
	corev1 "k8s.io/api/core/v1"
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
	// +optional
	Storage *StorageConfig `json:"storage,omitempty"`
}

type StorageConfig struct {
	// +optional
	CA *ComponentStorageConfig `json:"ca,omitempty"`
	// +optional
	Orderer *ComponentStorageConfig `json:"orderer,omitempty"`
	// +optional
	Peer *ComponentStorageConfig `json:"peer,omitempty"`
}

type ComponentStorageConfig struct {
	// Size is the persistent volume request for each component instance.
	// +optional
	Size string `json:"size,omitempty"`
	// StorageClassName selects the Kubernetes StorageClass for component PVCs.
	// Omit this field to use the cluster default StorageClass. Set it to an
	// empty string to disable dynamic provisioning for clusters that rely on
	// pre-bound persistent volumes.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
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
	// Sequence is the Fabric lifecycle definition sequence to use when
	// approving and committing the chaincode.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Sequence int32 `json:"sequence,omitempty"`
	// PackageLabel overrides the default lifecycle package label. When empty,
	// the operator uses <channel>_<name>_<version>.
	// +optional
	PackageLabel string `json:"packageLabel,omitempty"`
	// EndorsementPolicy is passed as a Fabric signature policy during approve
	// and commit. When empty, the operator will derive a channel-org policy.
	// +optional
	EndorsementPolicy string `json:"endorsementPolicy,omitempty"`
	// InitRequired controls the Fabric lifecycle --init-required flag.
	// +optional
	InitRequired bool `json:"initRequired,omitempty"`
	// CCAAS describes the Kubernetes Chaincode-as-a-Service workload and
	// connection package settings for this chaincode.
	// +optional
	CCAAS *ChaincodeAsAService `json:"ccaas,omitempty"`
}

type ChaincodeAsAService struct {
	// Replicas is the number of chaincode server pods behind the Service.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
	// ContainerPort is the port exposed by the chaincode server container.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=7052
	ContainerPort int32 `json:"containerPort,omitempty"`
	// ServicePort is the Kubernetes Service port peers use from connection.json.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=7052
	ServicePort int32 `json:"servicePort,omitempty"`
	// DialTimeout is written into Fabric CCaaS connection.json.
	// +optional
	// +kubebuilder:default="10s"
	DialTimeout string `json:"dialTimeout,omitempty"`
	// ImagePullPolicy controls when Kubernetes pulls the chaincode image.
	// +optional
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	// +kubebuilder:default=IfNotPresent
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// Env adds extra environment variables to the chaincode server container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`
	// Resources controls chaincode server container resource requests/limits.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
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

type ChannelOrgStatus struct {
	Name      string         `json:"name"`
	Namespace string         `json:"namespace,omitempty"`
	MSPName   string         `json:"mspName,omitempty"`
	PeerNames []string       `json:"peerNames,omitempty"`
	Peers     WorkloadStatus `json:"peers,omitempty"`
	Ready     bool           `json:"ready"`
	Message   string         `json:"message,omitempty"`
}

type ChannelStatus struct {
	Name               string             `json:"name"`
	ConfigMapName      string             `json:"configMapName,omitempty"`
	BlockConfigMapName string             `json:"blockConfigMapName,omitempty"`
	ConfigReady        bool               `json:"configReady"`
	BlockReady         bool               `json:"blockReady"`
	Orderers           WorkloadStatus     `json:"orderers,omitempty"`
	Peers              WorkloadStatus     `json:"peers,omitempty"`
	Orgs               []ChannelOrgStatus `json:"orgs,omitempty"`
	Ready              bool               `json:"ready"`
	Message            string             `json:"message,omitempty"`
}

type ChaincodeTargetStatus struct {
	OrgName              string `json:"orgName"`
	Namespace            string `json:"namespace,omitempty"`
	PeerName             string `json:"peerName"`
	ServiceName          string `json:"serviceName,omitempty"`
	Address              string `json:"address,omitempty"`
	PackageConfigMapName string `json:"packageConfigMapName,omitempty"`
	PackageMetadataReady bool   `json:"packageMetadataReady"`
	Message              string `json:"message,omitempty"`
}

type ChaincodeStatus struct {
	Name                 string                  `json:"name"`
	Channel              string                  `json:"channel"`
	Version              string                  `json:"version"`
	PackageLabel         string                  `json:"packageLabel,omitempty"`
	Sequence             int32                   `json:"sequence,omitempty"`
	PackageMetadata      WorkloadStatus          `json:"packageMetadata,omitempty"`
	PackageMetadataReady bool                    `json:"packageMetadataReady"`
	Targets              []ChaincodeTargetStatus `json:"targets,omitempty"`
	Message              string                  `json:"message,omitempty"`
}

// FabricNetworkStatus defines the observed state of FabricNetwork.
type FabricNetworkStatus struct {
	// +optional
	Phase           Phase             `json:"phase,omitempty"`
	Message         string            `json:"message,omitempty"`
	OrgStatus       []OrgStatus       `json:"orgStatus,omitempty"`
	ChannelStatus   []ChannelStatus   `json:"channelStatus,omitempty"`
	ChaincodeStatus []ChaincodeStatus `json:"chaincodeStatus,omitempty"`

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
