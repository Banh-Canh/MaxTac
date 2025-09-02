package v1alpha1

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Access interface implementations

// ResourceWithExpiration methods
func (a *Access) GetDuration() string {
	return a.Spec.Duration
}

func (a *Access) GetExpirationTimestamp() *metav1.Time {
	return a.Status.ExpirationTimestamp
}

func (a *Access) SetExpirationTimestamp(t *metav1.Time) {
	a.Status.ExpirationTimestamp = t
}

func (a *Access) GetCreationTimestamp() metav1.Time {
	return a.ObjectMeta.CreationTimestamp
}

// ResourceWithStatus methods
func (a *Access) GetNetpols() []Netpol {
	return a.Status.Netpols
}

func (a *Access) SetNetpols(netpols []Netpol) {
	a.Status.Netpols = netpols
}

func (a *Access) GetServices() []SvcRef {
	return a.Status.Services
}

func (a *Access) SetServices(services []SvcRef) {
	a.Status.Services = services
}

// ServiceSelector methods
func (a *Access) GetServiceSelector() *metav1.LabelSelector {
	return a.Spec.ServiceSelector
}

// TargetProvider methods
func (a *Access) GetTargets() []string {
	var targets []string
	for _, target := range a.Spec.Targets {
		if target.ServiceName != "" && target.Namespace != "" {
			targets = append(targets, fmt.Sprintf("%s,%s", target.ServiceName, target.Namespace))
		}
	}
	if len(targets) == 0 {
		return []string{} // Return empty slice if no targets
	}
	return []string{strings.Join(targets, ";")}
}

func (a *Access) GetDirection() string {
	return a.Spec.Direction
}

// ClusterAccess interface implementations

// ResourceWithExpiration methods
func (ca *ClusterAccess) GetDuration() string {
	return ca.Spec.Duration
}

func (ca *ClusterAccess) GetExpirationTimestamp() *metav1.Time {
	return ca.Status.ExpirationTimestamp
}

func (ca *ClusterAccess) SetExpirationTimestamp(t *metav1.Time) {
	ca.Status.ExpirationTimestamp = t
}

func (ca *ClusterAccess) GetCreationTimestamp() metav1.Time {
	return ca.ObjectMeta.CreationTimestamp
}

// ResourceWithStatus methods
func (ca *ClusterAccess) GetNetpols() []Netpol {
	return ca.Status.Netpols
}

func (ca *ClusterAccess) SetNetpols(netpols []Netpol) {
	ca.Status.Netpols = netpols
}

func (ca *ClusterAccess) GetServices() []SvcRef {
	return ca.Status.Services
}

func (ca *ClusterAccess) SetServices(services []SvcRef) {
	ca.Status.Services = services
}

// ServiceSelector methods
func (ca *ClusterAccess) GetServiceSelector() *metav1.LabelSelector {
	return ca.Spec.ServiceSelector
}

// TargetProvider methods
func (ca *ClusterAccess) GetTargets() []string {
	var targets []string
	for _, target := range ca.Spec.Targets {
		if target.ServiceName != "" && target.Namespace != "" {
			targets = append(targets, fmt.Sprintf("%s,%s", target.ServiceName, target.Namespace))
		}
	}
	if len(targets) == 0 {
		return []string{} // Return empty slice if no targets
	}
	return []string{strings.Join(targets, ";")}
}

func (ca *ClusterAccess) GetDirection() string {
	return ca.Spec.Direction
}

// ExternalAccess interface implementations

// ResourceWithExpiration methods
func (ea *ExternalAccess) GetDuration() string {
	return ea.Spec.Duration
}

func (ea *ExternalAccess) GetExpirationTimestamp() *metav1.Time {
	return ea.Status.ExpirationTimestamp
}

func (ea *ExternalAccess) SetExpirationTimestamp(t *metav1.Time) {
	ea.Status.ExpirationTimestamp = t
}

func (ea *ExternalAccess) GetCreationTimestamp() metav1.Time {
	return ea.ObjectMeta.CreationTimestamp
}

// ResourceWithStatus methods
func (ea *ExternalAccess) GetNetpols() []Netpol {
	return ea.Status.Netpols
}

func (ea *ExternalAccess) SetNetpols(netpols []Netpol) {
	ea.Status.Netpols = netpols
}

func (ea *ExternalAccess) GetServices() []SvcRef {
	return ea.Status.Services
}

func (ea *ExternalAccess) SetServices(services []SvcRef) {
	ea.Status.Services = services
}

// ServiceSelector methods
func (ea *ExternalAccess) GetServiceSelector() *metav1.LabelSelector {
	return ea.Spec.ServiceSelector
}

// TargetProvider methods
func (ea *ExternalAccess) GetTargets() []string {
	if len(ea.Spec.TargetCIDRs) == 0 {
		return []string{} // Return empty slice if no targets
	}
	return []string{strings.Join(ea.Spec.TargetCIDRs, ",")}
}

func (ea *ExternalAccess) GetDirection() string {
	return ea.Spec.Direction
}

// ClusterExternalAccess interface implementations

// ResourceWithExpiration methods
func (cea *ClusterExternalAccess) GetDuration() string {
	return cea.Spec.Duration
}

func (cea *ClusterExternalAccess) GetExpirationTimestamp() *metav1.Time {
	return cea.Status.ExpirationTimestamp
}

func (cea *ClusterExternalAccess) SetExpirationTimestamp(t *metav1.Time) {
	cea.Status.ExpirationTimestamp = t
}

func (cea *ClusterExternalAccess) GetCreationTimestamp() metav1.Time {
	return cea.ObjectMeta.CreationTimestamp
}

// ResourceWithStatus methods
func (cea *ClusterExternalAccess) GetNetpols() []Netpol {
	return cea.Status.Netpols
}

func (cea *ClusterExternalAccess) SetNetpols(netpols []Netpol) {
	cea.Status.Netpols = netpols
}

func (cea *ClusterExternalAccess) GetServices() []SvcRef {
	return cea.Status.Services
}

func (cea *ClusterExternalAccess) SetServices(services []SvcRef) {
	cea.Status.Services = services
}

// ServiceSelector methods
func (cea *ClusterExternalAccess) GetServiceSelector() *metav1.LabelSelector {
	return cea.Spec.ServiceSelector
}

// TargetProvider methods
func (cea *ClusterExternalAccess) GetTargets() []string {
	if len(cea.Spec.TargetCIDRs) == 0 {
		return []string{} // Return empty slice if no targets
	}
	return []string{strings.Join(cea.Spec.TargetCIDRs, ",")}
}

func (cea *ClusterExternalAccess) GetDirection() string {
	return cea.Spec.Direction
}
