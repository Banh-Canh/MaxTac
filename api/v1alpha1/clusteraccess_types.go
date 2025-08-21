/*
Copyright 2025.

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
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ClusterAccessSpec defines the desired state of ClusterAccess.
type ClusterAccessSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// +kubebuilder:validation:Required
	ServiceSelector *metav1.LabelSelector `json:"serviceSelector"`

	// +kubebuilder:validation:Optional
	Targets []AccessPoint `json:"targets"`

	// +kubebuilder:validation:Optional
	Direction string `json:"direction"`

	// +kubebuilder:validation:Optional
	Mirrored bool `json:"mirrored,omitempty"`
}

type AccessPoint struct {
	ServiceName string `json:"serviceName"`
	Namespace   string `json:"namespace"`
}

// ClusterAccessStatus defines the observed state of ClusterAccess.
type ClusterAccessStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Netpols  []Netpol `json:"netpols,omitempty"`
	Services []SvcRef `json:"services,omitempty"`

	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions"`
}

type Netpol struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type SvcRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ClusterAccess is the Schema for the clusteraccesses API.
type ClusterAccess struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterAccessSpec   `json:"spec,omitempty"`
	Status ClusterAccessStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterAccessList contains a list of ClusterAccess.
type ClusterAccessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterAccess `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterAccess{}, &ClusterAccessList{})
}
