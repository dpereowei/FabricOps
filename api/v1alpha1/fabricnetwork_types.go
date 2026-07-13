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

	// +kubebuilder:validation:MinItems=1
	Orgs []Org `json:"orgs"`

	Channels []Channel `json:"channels"`

	Chaincodes []Chaincode `json:"chaincodes"`
}

type GlobalConfig struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9][A-Za-z0-9._-]*$"
	FabricVersion string `json:"fabricVersion"`
	TLS           bool   `json:"tls"`
	// +optional
	Jobs *JobCleanupConfig `json:"jobs,omitempty"`
	// +optional
	Storage *StorageConfig `json:"storage,omitempty"`
	// +optional
	Observability *ObservabilityConfig `json:"observability,omitempty"`
	// +optional
	NetworkPolicy *NetworkPolicyConfig `json:"networkPolicy,omitempty"`
}

type JobCleanupConfig struct {
	// SucceededHistoryTTLSeconds deletes eligible successful FabricOps helper
	// Jobs after this many seconds. Failed Jobs are retained for diagnostics.
	// Only Jobs whose result is represented by durable FabricOps resources are
	// eligible.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=604800
	SucceededHistoryTTLSeconds *int32 `json:"succeededHistoryTTLSeconds,omitempty"`
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
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:Pattern="^[0-9]+(Ei|Pi|Ti|Gi|Mi|Ki|E|P|T|G|M|K)?$"
	Size string `json:"size,omitempty"`
	// StorageClassName selects the Kubernetes StorageClass for component PVCs.
	// Omit this field to use the cluster default StorageClass. Set it to an
	// empty string to disable dynamic provisioning for clusters that rely on
	// pre-bound persistent volumes.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

type ObservabilityConfig struct {
	// ServiceMonitor controls optional Prometheus Operator ServiceMonitor
	// output for Fabric operations endpoint Services.
	// +optional
	ServiceMonitor *ServiceMonitorConfig `json:"serviceMonitor,omitempty"`
}

type ServiceMonitorConfig struct {
	// Enabled creates one ServiceMonitor per generated org namespace. The
	// monitoring.coreos.com/v1 ServiceMonitor CRD must already exist.
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
	// Interval configures how often Prometheus scrapes Fabric operations
	// endpoints.
	// +optional
	// +kubebuilder:default="30s"
	// +kubebuilder:validation:Pattern="^([0-9]+(ms|s|m|h))+$"
	Interval string `json:"interval,omitempty"`
	// ScrapeTimeout configures the Prometheus scrape timeout.
	// +optional
	// +kubebuilder:default="10s"
	// +kubebuilder:validation:Pattern="^([0-9]+(ms|s|m|h))+$"
	ScrapeTimeout string `json:"scrapeTimeout,omitempty"`
	// Labels adds metadata labels to generated ServiceMonitors. Use this for
	// Prometheus release selectors such as release=<name>.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

type NetworkPolicyConfig struct {
	// Enabled creates org boundary NetworkPolicies for FabricOps-managed pods.
	// The cluster CNI must support Kubernetes NetworkPolicy enforcement.
	// +optional
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`
}

type Org struct {
	Organization OrgMeta        `json:"organization"`
	CA           CAConfig       `json:"ca"`
	Orderers     []OrdererGroup `json:"orderers,omitempty"`
	Peer         *PeerConfig    `json:"peer,omitempty"`
}

type OrgMeta struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$"
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=3
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?\\.)+[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	Domain string `json:"domain"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern="^[A-Za-z][A-Za-z0-9]*$"
	MSPName string `json:"mspName"`
}

type CAConfig struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9_-]+$"
	DB string `json:"db"`
}

type OrdererGroup struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9]([A-Za-z0-9_.-]*[A-Za-z0-9])?$"
	GroupName string `json:"groupName"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9_-]+$"
	Type string `json:"type"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=50
	Instances int `json:"instances"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=50
	// +kubebuilder:validation:Pattern="^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	Prefix string `json:"prefix"`
}

type PeerConfig struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=50
	Instances int `json:"instances"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9_-]+$"
	DB string `json:"db"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=50
	// +kubebuilder:validation:Pattern="^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
	Prefix string `json:"prefix"`
}

type Channel struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=249
	// +kubebuilder:validation:Pattern="^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$"
	Name string `json:"name"`

	// +kubebuilder:validation:MinItems=1
	Orgs []ChannelOrg `json:"orgs"`
}

type ChannelOrg struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$"
	Name string `json:"name"`
	// +kubebuilder:validation:MinItems=1
	Peers []string `json:"peers"`
}

type Chaincode struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9]([A-Za-z0-9_.-]*[A-Za-z0-9])?$"
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	Version string `json:"version"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=249
	// +kubebuilder:validation:Pattern="^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$"
	Channel string `json:"channel"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=512
	Image string `json:"image"`
	// Sequence is the Fabric lifecycle definition sequence to use when
	// approving and committing the chaincode.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Sequence int32 `json:"sequence,omitempty"`
	// PackageLabel overrides the default lifecycle package label. When empty,
	// the operator uses <channel>_<name>_<version>.
	// +optional
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9]([A-Za-z0-9_.-]*[A-Za-z0-9])?$"
	PackageLabel string `json:"packageLabel,omitempty"`
	// EndorsementPolicy is passed as a Fabric signature policy during approve
	// and commit. When empty, the operator will derive a channel-org policy.
	// +optional
	// +kubebuilder:validation:MaxLength=512
	EndorsementPolicy string `json:"endorsementPolicy,omitempty"`
	// InitRequired controls the Fabric lifecycle --init-required flag.
	// +optional
	InitRequired bool `json:"initRequired,omitempty"`
	// PrivateData declares explicit Fabric private data collections for this
	// chaincode definition. The operator renders these entries to the Fabric
	// collection config JSON used during approve and commit.
	// +optional
	PrivateData []PrivateDataCollection `json:"privateData,omitempty"`
	// CouchDBIndexes declares JSON CouchDB indexes that should be packaged
	// with this chaincode. Public indexes are rendered under
	// metadata/META-INF/statedb/couchdb/indexes; collection-scoped indexes are
	// rendered under metadata/META-INF/statedb/couchdb/collections/<collection>/indexes.
	// +optional
	CouchDBIndexes []CouchDBIndex `json:"couchdbIndexes,omitempty"`
	// CCAAS describes the Kubernetes Chaincode-as-a-Service workload and
	// connection package settings for this chaincode.
	// +optional
	CCAAS *ChaincodeAsAService `json:"ccaas,omitempty"`
}

type CouchDBIndex struct {
	// Name is the CouchDB index name and the basis for the packaged file name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9][A-Za-z0-9_.-]*$"
	Name string `json:"name"`
	// Fields is the ordered list of JSON document fields indexed by CouchDB.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	Fields []string `json:"fields"`
	// DesignDocument optionally sets the CouchDB design document (`ddoc`).
	// +optional
	// +kubebuilder:validation:MaxLength=128
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9][A-Za-z0-9_.-]*$"
	DesignDocument string `json:"designDocument,omitempty"`
	// Collection optionally scopes the index to a private data collection.
	// When empty, the index is packaged for the chaincode public state.
	// +optional
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9][A-Za-z0-9_-]*$"
	Collection string `json:"collection,omitempty"`
}

type PrivateDataCollection struct {
	// Name is the Fabric private data collection name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern="^[A-Za-z0-9][A-Za-z0-9_-]*$"
	Name string `json:"name"`
	// OrgNames lists the channel organizations allowed to store collection data.
	// When Policy is empty, the operator derives an OR('<MSP>.member', ...)
	// policy from this list.
	// +kubebuilder:validation:MinItems=1
	OrgNames []string `json:"orgNames"`
	// Policy overrides the derived Fabric collection distribution policy.
	// +optional
	// +kubebuilder:validation:MaxLength=512
	Policy string `json:"policy,omitempty"`
	// RequiredPeerCount is the minimum number of authorized peers that must
	// receive private data before endorsement succeeds.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=50
	RequiredPeerCount *int32 `json:"requiredPeerCount,omitempty"`
	// MaxPeerCount is the maximum number of authorized peers each endorsing peer
	// attempts to disseminate private data to.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=50
	MaxPeerCount *int32 `json:"maxPeerCount,omitempty"`
	// BlockToLive controls private data purging by block age. Zero keeps data
	// indefinitely.
	// +optional
	// +kubebuilder:validation:Minimum=0
	BlockToLive *int64 `json:"blockToLive,omitempty"`
	// MemberOnlyRead asks peers to enforce that only collection member
	// organizations can read collection data.
	// +optional
	MemberOnlyRead *bool `json:"memberOnlyRead,omitempty"`
	// MemberOnlyWrite asks peers to enforce that only collection member
	// organizations can write collection data.
	// +optional
	MemberOnlyWrite *bool `json:"memberOnlyWrite,omitempty"`
	// EndorsementPolicy optionally overrides the chaincode endorsement policy for
	// this collection.
	// +optional
	EndorsementPolicy *PrivateDataEndorsementPolicy `json:"endorsementPolicy,omitempty"`
}

type PrivateDataEndorsementPolicy struct {
	// SignaturePolicy is a Fabric signature policy, for example
	// AND('BankAMSP.member','BankBMSP.member').
	// +optional
	// +kubebuilder:validation:MaxLength=512
	SignaturePolicy string `json:"signaturePolicy,omitempty"`
	// ChannelConfigPolicy references a policy from the channel configuration.
	// +optional
	// +kubebuilder:validation:MaxLength=256
	ChannelConfigPolicy string `json:"channelConfigPolicy,omitempty"`
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
	// +kubebuilder:validation:Pattern="^([0-9]+(ms|s|m|h))+$"
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

type OrdererEndpointStatus struct {
	Name              string `json:"name"`
	ClientAddress     string `json:"clientAddress,omitempty"`
	AdminAddress      string `json:"adminAddress,omitempty"`
	OperationsAddress string `json:"operationsAddress,omitempty"`
}

type PeerEndpointStatus struct {
	Name              string `json:"name"`
	Address           string `json:"address,omitempty"`
	ChaincodeAddress  string `json:"chaincodeAddress,omitempty"`
	OperationsAddress string `json:"operationsAddress,omitempty"`
}

type OrgStatus struct {
	Name          string `json:"name"`
	Namespace     string `json:"namespace,omitempty"`
	IdentityReady bool   `json:"identityReady"`
	IdentityError string `json:"identityError,omitempty"`
	CAReady       bool   `json:"caReady"`
	// CAEndpoint is the in-cluster Fabric CA endpoint for this org.
	// +optional
	CAEndpoint string `json:"caEndpoint,omitempty"`
	// OrdererEndpoints lists in-cluster client, admin, and operations
	// endpoints for desired orderer workloads in this org.
	// +optional
	OrdererEndpoints []OrdererEndpointStatus `json:"ordererEndpoints,omitempty"`
	Orderers         WorkloadStatus          `json:"orderers,omitempty"`
	OrderersReady    bool                    `json:"orderersReady"`
	// PeerEndpoints lists in-cluster client, chaincode, and operations
	// endpoints for desired peer workloads in this org.
	// +optional
	PeerEndpoints []PeerEndpointStatus `json:"peerEndpoints,omitempty"`
	Peers         WorkloadStatus       `json:"peers,omitempty"`
	PeersReady    bool                 `json:"peersReady"`
	// ConnectionProfileConfigMapName points application clients at the
	// generated Fabric connection profile ConfigMap for this org.
	// +optional
	ConnectionProfileConfigMapName string `json:"connectionProfileConfigMapName,omitempty"`
	Ready                          bool   `json:"ready"`
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
	OrgName                string         `json:"orgName"`
	Namespace              string         `json:"namespace,omitempty"`
	PeerName               string         `json:"peerName"`
	WorkloadName           string         `json:"workloadName,omitempty"`
	Workload               WorkloadStatus `json:"workload,omitempty"`
	WorkloadReady          bool           `json:"workloadReady"`
	ServiceName            string         `json:"serviceName,omitempty"`
	Address                string         `json:"address,omitempty"`
	PackageConfigMapName   string         `json:"packageConfigMapName,omitempty"`
	PackageIDConfigMapName string         `json:"packageIDConfigMapName,omitempty"`
	InstallJobName         string         `json:"installJobName,omitempty"`
	ApproveJobName         string         `json:"approveJobName,omitempty"`
	PackageMetadataReady   bool           `json:"packageMetadataReady"`
	PackageID              string         `json:"packageID,omitempty"`
	ChaincodeID            string         `json:"chaincodeID,omitempty"`
	Installed              bool           `json:"installed"`
	Approved               bool           `json:"approved"`
	Message                string         `json:"message,omitempty"`
}

type ChaincodeStatus struct {
	Name                 string                  `json:"name"`
	Channel              string                  `json:"channel"`
	Version              string                  `json:"version"`
	PackageLabel         string                  `json:"packageLabel,omitempty"`
	Sequence             int32                   `json:"sequence,omitempty"`
	CollectionConfigMap  string                  `json:"collectionConfigMap,omitempty"`
	CollectionConfigHash string                  `json:"collectionConfigHash,omitempty"`
	PackageMetadata      WorkloadStatus          `json:"packageMetadata,omitempty"`
	PackageMetadataReady bool                    `json:"packageMetadataReady"`
	Installed            WorkloadStatus          `json:"installed,omitempty"`
	InstalledReady       bool                    `json:"installedReady"`
	Workloads            WorkloadStatus          `json:"workloads,omitempty"`
	WorkloadsReady       bool                    `json:"workloadsReady"`
	Approved             WorkloadStatus          `json:"approved,omitempty"`
	ApprovedReady        bool                    `json:"approvedReady"`
	CommitJobName        string                  `json:"commitJobName,omitempty"`
	Committed            bool                    `json:"committed"`
	Ready                bool                    `json:"ready"`
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
