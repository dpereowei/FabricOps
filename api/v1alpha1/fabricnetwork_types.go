package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FabricNetworkSpec struct {
	OrgName string `json:"orgName"`
	Domain  string `json:"domain"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type FabricNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FabricNetworkSpec `json:"spec,omitempty"`
	Status struct{}          `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type FabricNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FabricNetwork `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FabricNetwork{}, &FabricNetworkList{})
}