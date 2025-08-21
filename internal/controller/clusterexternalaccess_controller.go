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

package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vtkiov1alpha1 "github.com/Banh-Canh/maxtac/api/v1alpha1"
	"github.com/Banh-Canh/maxtac/internal/k8s/networkpolicy"
	"github.com/Banh-Canh/maxtac/internal/utils/logger"
	"github.com/Banh-Canh/maxtac/internal/utils/network"
)

// ClusterExternalAccessReconciler reconciles a ClusterExternalAccess object
type ClusterExternalAccessReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	StatusNeedUpdate bool
}

const (
	ClusterExternalAccessDirectionAnnotation        = "maxtac.vtk.io.clusterexternalaccess/direction"
	ClusterExternalAccessOwnerLabel                 = "maxtac.vtk.io.clusterexternalaccess/owner"
	ClusterExternalAccessServiceOwnerNameLabel      = "maxtac.vtk.io.clusterexternalaccess/serviceOwnerName"
	ClusterExternalAccessServiceOwnerNamespaceLabel = "maxtac.vtk.io.clusterexternalaccess/serviceOwnerNamespace"
	ClusterExternalAccessTargetsAnnotation          = "maxtac.vtk.io.clusterexternalaccess/targets"
)

// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=clusterexternalaccesses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=clusterexternalaccesses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=clusterexternalaccesses/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClusterExternalAccessReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger.Logger.Debug("Starting reconciliation for ClusterExternalAccess object", "name", req.NamespacedName)
	clusterexternalaccess := &vtkiov1alpha1.ClusterExternalAccess{}
	var deployedNetpols []vtkiov1alpha1.Netpol

	if err := r.Get(ctx, req.NamespacedName, clusterexternalaccess); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Logger.Debug("ClusterExternalAccess resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Logger.Error("Failed to get ClusterExternalAccess resource.", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	// Check for expiration and handle deletion if necessary.
	if r.isExpired(clusterexternalaccess) {
		logger.Logger.Info("ClusterExternalAccess resource has expired, deleting it now.", "name", clusterexternalaccess.Name)
		if err := r.Delete(ctx, clusterexternalaccess); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete expired ClusterExternalAccess resource: %w", err)
		}
		// A deletion request was sent, no need to do further work in this reconcile loop.
		return ctrl.Result{}, nil
	}

	// At each reconciliation, check for any NetworkPolicies owned by this ClusterExternalAccess
	// whose corresponding Service has been deleted.
	if err := r.cleanupOrphanedNetworkPolicies(ctx, clusterexternalaccess); err != nil {
		logger.Logger.Error("Error during cleanup of orphaned NetworkPolicies.", slog.Any("error", err))
		return ctrl.Result{}, err // Requeue on cleanup failure
	}

	// Convert the LabelSelector from the spec into a selector object that the client can use.
	selector, err := metav1.LabelSelectorAsSelector(clusterexternalaccess.Spec.ServiceSelector)
	if err != nil {
		logger.Logger.Error(
			"Error converting label selector.",
			slog.Any("error", err),
			"selector",
			clusterexternalaccess.Spec.ServiceSelector,
		)
		return ctrl.Result{}, fmt.Errorf("failed to convert label selector: %w", err)
	}

	// List all services in all namespaces that match the label selector.
	serviceList := &corev1.ServiceList{}
	if err := r.List(ctx, serviceList, &client.ListOptions{LabelSelector: selector}); err != nil {
		logger.Logger.Error("Failed to list services with selector.", slog.Any("error", err), "selector", selector.String())
		return ctrl.Result{}, err
	}

	// Iterate over each service found by the selector
	var matchedServices []vtkiov1alpha1.SvcRef
	for _, service := range serviceList.Items {
		matchedServices = append(matchedServices, vtkiov1alpha1.SvcRef{
			Name:      service.Name,
			Namespace: service.Namespace,
		})
		var targetsStr string
		var direction string

		annotations := service.GetAnnotations()
		annotationTargets, hasTargetsAnnotation := annotations[ClusterExternalAccessTargetsAnnotation]
		annotationDirection, hasDirectionAnnotation := annotations[ClusterExternalAccessDirectionAnnotation]

		// Prefer annotations if they are both present
		if hasTargetsAnnotation {
			logger.Logger.Debug(
				"Using targets from service annotations",
				"service",
				service.Name,
				"namespace",
				service.Namespace,
			)
			targetsStr = annotationTargets
		} else {
			targetsStr = strings.Join(clusterexternalaccess.Spec.TargetCIDRs, ",")
			logger.Logger.Info(
				"Annotations targets not found on service, falling back to ClusterExternalAccess spec",
				"service",
				service.Name,
				"namespace",
				service.Namespace,
			)

		}
		if hasDirectionAnnotation {
			logger.Logger.Debug(
				"Using direction from service annotations",
				"service",
				service.Name,
				"namespace",
				service.Namespace,
			)
			direction = annotationDirection
		} else {
			logger.Logger.Info(
				"Annotations direction not found on service, falling back to ClusterExternalAccess spec",
				"service",
				service.Name,
				"namespace",
				service.Namespace,
			)
			direction = clusterexternalaccess.Spec.Direction

		}

		// If after checking both sources, we still have no targets or direction, skip this service.
		if targetsStr == "" || direction == "" {
			logger.Logger.Info(
				"No targets or direction defined in either annotations or spec; skipping service",
				"service", service.Name,
				"namespace", service.Namespace,
			)
			continue
		}

		// Normalize and validate the direction annotation value.
		direction = strings.ToLower(direction)
		validDirections := map[string]struct{}{
			"ingress": {},
			"egress":  {},
			"all":     {},
		}
		if _, isValid := validDirections[direction]; !isValid {
			logger.Logger.Error(
				"Invalid direction value",
				"service", service.Name,
				"namespace", service.Namespace,
				"value", direction,
				"expected_values", "ingress, egress, or all",
			)
			continue // Skip processing for this service.
		}

		// This annotated service is our source.
		logger.Logger.Info(
			"Found matched service",
			"source_service",
			service.Name,
			"namespace",
			service.Namespace,
		)

		sourceSvcSelector, sourcePorts, err := r.getServiceDetails(ctx, service.Name, service.Namespace)
		if err != nil {
			logger.Logger.Error("Error getting source service details.", slog.Any("error", err), "service", service.Name)
			continue // An error here should not stop reconciliation for other services
		}

		// Split the targets string by comma and iterate
		targets := strings.Split(targetsStr, ",")
		for _, targetCidr := range targets {
			trimmedCidr := strings.TrimSpace(targetCidr)
			if trimmedCidr == "" {
				continue
			}

			// Variable to hold the final, correctly formatted CIDR.
			finalCidr := trimmedCidr

			// If the target originated from the service annotation, convert the
			// hyphen-separated mask to the standard slash format.
			if hasTargetsAnnotation {
				lastHyphen := strings.LastIndex(trimmedCidr, "-")
				// This handles both IPv4 and IPv6 addresses correctly.
				if lastHyphen > 0 {
					ipPart := trimmedCidr[:lastHyphen]
					maskPart := trimmedCidr[lastHyphen+1:]
					finalCidr = ipPart + "/" + maskPart
					logger.Logger.Debug("Converted annotation CIDR for NetworkPolicy", "original", trimmedCidr, "converted", finalCidr)
				} else {
					logger.Logger.Warn("Annotated CIDR does not appear to have a hyphen-separated mask, using value as-is", "cidr", trimmedCidr)
				}
			}

			// Use the 'finalCidr' variable for validation.
			if err := network.ValidateTargetCIDR(finalCidr); err != nil {
				// Log the error for the invalid CIDR and continue to the next one.
				logger.Logger.Error("Error validating a target CIDR, skipping.", "cidr", finalCidr, slog.Any("error", err))
				continue
			}

			// Sanitize the finalCidr for use in resource names.
			targetCidrName := strings.ReplaceAll(finalCidr, ".", "-")
			targetCidrName = strings.ReplaceAll(targetCidrName, "/", "-")
			targetCidrName = strings.ReplaceAll(targetCidrName, ":", "-")

			var directionSuffix string
			switch direction {
			case "ingress":
				directionSuffix = "ing"
			case "egress":
				directionSuffix = "egr"
			case "all":
				directionSuffix = "all"
			}

			netpolName := fmt.Sprintf(
				"cluster-%s--%s-%s---%s-%s",
				clusterexternalaccess.Name,
				service.Namespace,
				service.Name,
				targetCidrName,
				directionSuffix,
			)

			// Use the 'finalCidr' to define the NetworkPolicy.
			netpol := networkpolicy.DefineExternalAccessNetworkPolicyCIDR(
				netpolName,
				service.Namespace,
				sourceSvcSelector,
				finalCidr,
				sourcePorts,
				direction,
			)

			// Create the labels for the NetworkPolicy so we can track and clean it up.
			netpolLabels := map[string]string{
				ClusterExternalAccessOwnerLabel:                 clusterexternalaccess.Name,
				ClusterExternalAccessServiceOwnerNameLabel:      service.Name,
				ClusterExternalAccessServiceOwnerNamespaceLabel: service.Namespace,
			}

			netpol.Labels = netpolLabels

			if err := r.deployResource(ctx, clusterexternalaccess, netpol, &networkingv1.NetworkPolicy{}, networkpolicy.ExtractNetpolSpec, false); err != nil {
				logger.Logger.Error("Error deploying NetworkPolicy.", slog.Any("error", err), "netpol_name", netpolName)
				// Continue to the next CIDR even if this one fails
			} else {
				logger.Logger.Info("Successfully deployed target NetworkPolicy", "name", netpol.Name, "namespace", netpol.Namespace)
				deployedNetpols = append(deployedNetpols, vtkiov1alpha1.Netpol{
					Name:      netpol.Name,
					Namespace: netpol.Namespace,
				})
			}

		}
	}

	// Set access ready status
	// Update netpols in status
	finalNetpols, err := r.reconcileNetpolStatus(ctx, clusterexternalaccess, deployedNetpols)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Update services in status
	finalServices, err := r.reconcileServiceStatus(ctx, clusterexternalaccess, matchedServices)
	if err != nil {
		return ctrl.Result{}, err
	}

	clusterexternalaccess.Status.Netpols = finalNetpols
	clusterexternalaccess.Status.Services = finalServices
	if err := r.updateStatus(ctx, clusterexternalaccess); err != nil {
		logger.Logger.Error("Error updating status.", slog.Any("error", err))
	}

	// Calculate and return a targeted requeue duration if the resource has an expiration.
	if clusterexternalaccess.Status.ExpirationTimestamp != nil {
		timeRemaining := time.Until(clusterexternalaccess.Status.ExpirationTimestamp.Time)
		if timeRemaining > 0 {
			logger.Logger.Debug("Requeuing to check for expiration.", "name", clusterexternalaccess.Name, "duration", timeRemaining)
			return ctrl.Result{RequeueAfter: timeRemaining}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterExternalAccessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vtkiov1alpha1.ClusterExternalAccess{}).
		Owns(&networkingv1.NetworkPolicy{}).
		// Watch for changes to Services and enqueue reconcile requests for ClusterExternalAccess resources.
		Watches(
			&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(r.findClusterExternalAccessForService),
		).
		Named("clusterexternalaccess").
		Complete(r)
}

// findClusterExternalAccessForService is a handler.MapFunc that finds all ClusterExternalAccess resources.
// we will trigger the reconciliation of all accesses here on service event.
func (r *ClusterExternalAccessReconciler) findClusterExternalAccessForService(ctx context.Context, obj client.Object) []reconcile.Request {
	service, ok := obj.(*corev1.Service)
	if !ok {
		logger.Logger.Error("Unexpected type received in map function for Service watch", "type", fmt.Sprintf("%T", obj))
		return []reconcile.Request{}
	}

	logger.Logger.Debug(
		"Service change detected, triggering reconciliation for all ClusterExternalAccess resources",
		"service", service.Name,
		"namespace", service.GetNamespace(),
	)

	// List all ClusterExternalAccess resources cluster-wide.
	clusterExternalAccessList := &vtkiov1alpha1.ClusterExternalAccessList{}
	if err := r.List(ctx, clusterExternalAccessList); err != nil {
		logger.Logger.Error("Failed to list ClusterExternalAccess resources in map function", slog.Any("error", err))
		return []reconcile.Request{}
	}

	// Create a reconciliation request for each ClusterExternalAccess resource.
	requests := make([]reconcile.Request, len(clusterExternalAccessList.Items))
	for i, ea := range clusterExternalAccessList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      ea.Name,
				Namespace: ea.Namespace,
			},
		}
	}
	return requests
}
