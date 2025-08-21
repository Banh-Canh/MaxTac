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

// ExternalAccessSpec defines the desired state of ExternalAccess.
type ExternalAccessSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:validation:Optional
	// +kubebuilder:items:validation:Pattern=`^((([0-9]{1,3}\.){3}[0-9]{1,3})(/(3[0-2]|[12]?[0-9]))?|([0-9A-Fa-f:]*:[0-9A-Fa-f:]*)(/(12[0-8]|1[01][0-9]|[1-9]?[0-9]))?)$`
	TargetCIDRs []string `json:"targetCIDRs,omitempty"`
	// +kubebuilder:validation:Required
	ServiceSelector *metav1.LabelSelector `json:"serviceSelector"`
	// +kubebuilder:validation:Optional
	Direction string `json:"direction"`
}

// ExternalAccessStatus defines the observed state of ExternalAccess.
type ExternalAccessStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	Netpols  []Netpol `json:"netpols,omitempty"`
	Services []SvcRef `json:"services,omitempty"`

	// +kubebuilder:validation:Optional
	Conditions []metav1.Condition `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ExternalAccess is the Schema for the externalaccesses API.
type ExternalAccess struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExternalAccessSpec   `json:"spec,omitempty"`
	Status ExternalAccessStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ExternalAccessList contains a list of ExternalAccess.
type ExternalAccessList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExternalAccess `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ExternalAccess{}, &ExternalAccessList{})
}
