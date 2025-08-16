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

// ExternalAccessReconciler reconciles a ExternalAccess object
type ExternalAccessReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	StatusNeedUpdate bool
}

const (
	ExternalAccessDirectionAnnotation        = "maxtac.vtk.io.externalaccess/direction"
	ExternalAccessOwnerLabel                 = "maxtac.vtk.io.externalaccess/owner"
	ExternalAccessServiceOwnerNameLabel      = "maxtac.vtk.io.externalaccess/serviceOwnerName"
	ExternalAccessServiceOwnerNamespaceLabel = "maxtac.vtk.io.externalaccess/serviceOwnerNamespace"
	ExternalAccessTargetsAnnotation          = "maxtac.vtk.io.externalaccess/targets"
)

// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=externalaccesses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=externalaccesses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=externalaccesses/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ExternalAccessReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger.Logger.Debug("Starting reconciliation for ExternalAccess object", "name", req.NamespacedName)
	externalaccess := &vtkiov1alpha1.ExternalAccess{}

	if err := r.Get(ctx, req.NamespacedName, externalaccess); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Logger.Info("ExternalAccess resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Logger.Error("Failed to get ExternalAccess resource.", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	// Finalizers
	if err := r.addFinalizer(ctx, externalaccess); err != nil {
		logger.Logger.Error("Error adding finalizer.", slog.Any("error", err))
	}

	// At each reconciliation, check for any NetworkPolicies owned by this ExternalAccess
	// whose corresponding Service has been deleted.
	if err := r.cleanupOrphanedNetworkPolicies(ctx, externalaccess); err != nil {
		logger.Logger.Error("Error during cleanup of orphaned NetworkPolicies.", slog.Any("error", err))
		return ctrl.Result{}, err // Requeue on cleanup failure
	}

	// Convert the LabelSelector from the spec into a selector object that the client can use.
	selector, err := metav1.LabelSelectorAsSelector(externalaccess.Spec.ServiceSelector)
	if err != nil {
		logger.Logger.Error("Error converting label selector.", slog.Any("error", err), "selector", externalaccess.Spec.ServiceSelector)
		return ctrl.Result{}, fmt.Errorf("failed to convert label selector: %w", err)
	}

	// List all services in all namespaces that match the label selector.
	serviceList := &corev1.ServiceList{}
	if err := r.List(ctx, serviceList, &client.ListOptions{LabelSelector: selector}); err != nil {
		logger.Logger.Error("Failed to list services with selector.", slog.Any("error", err), "selector", selector.String())
		return ctrl.Result{}, err
	}

	// Iterate over each service found by the selector
	for _, service := range serviceList.Items {
		var targetsStr string
		var direction string

		annotations := service.GetAnnotations()
		annotationTargets, hasTargetsAnnotation := annotations[ExternalAccessTargetsAnnotation]
		annotationDirection, hasDirectionAnnotation := annotations[ExternalAccessDirectionAnnotation]

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
			targetsStr = strings.Join(externalaccess.Spec.TargetCIDRs, ",")
			logger.Logger.Info(
				"Annotations targets not found on service, falling back to ExternalAccess spec",
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
				"Annotations direction not found on service, falling back to ExternalAccess spec",
				"service",
				service.Name,
				"namespace",
				service.Namespace,
			)
			direction = externalaccess.Spec.Direction

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
			"Found matched and annotated service",
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
				"%s--%s-%s---%s-%s",
				externalaccess.Name,
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
				ExternalAccessOwnerLabel:                 externalaccess.Name,
				ExternalAccessServiceOwnerNameLabel:      service.Name,
				ExternalAccessServiceOwnerNamespaceLabel: service.Namespace,
			}

			netpol.Labels = netpolLabels

			if err := r.deployResource(ctx, externalaccess, netpol, &networkingv1.NetworkPolicy{}, networkpolicy.ExtractNetpolSpec, false); err != nil {
				logger.Logger.Error("Error deploying NetworkPolicy.", slog.Any("error", err), "netpol_name", netpolName)
				// Continue to the next CIDR even if this one fails
			}
		}
	}

	// Set access ready status
	if err := r.updateStatus(ctx, externalaccess); err != nil {
		logger.Logger.Error("Error updating status.", slog.Any("error", err))
	}

	if err := r.removeFinalizer(ctx, externalaccess); err != nil {
		logger.Logger.Error("Error removing finalizer.", slog.Any("error", err))
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ExternalAccessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vtkiov1alpha1.ExternalAccess{}).
		Owns(&networkingv1.NetworkPolicy{}).
		// Watch for changes to Services and enqueue reconcile requests for ExternalAccess resources.
		Watches(
			&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(r.findExternalAccessForService),
		).
		Named("externalaccess").
		Complete(r)
}

// findExternalAccessForService is a handler.MapFunc that finds all ExternalAccess resources.
// we will trigger the reconciliation of all accesses here on service event.
func (r *ExternalAccessReconciler) findExternalAccessForService(ctx context.Context, obj client.Object) []reconcile.Request {
	service, ok := obj.(*corev1.Service)
	if !ok {
		logger.Logger.Error("Unexpected type received in map function for Service watch", "type", fmt.Sprintf("%T", obj))
		return []reconcile.Request{}
	}

	logger.Logger.Debug(
		"Service change detected, triggering reconciliation for all ExternalAccess resources",
		"service", service.Name,
		"namespace", service.GetNamespace(),
	)

	// List all ExternalAccess resources cluster-wide.
	externalAccessList := &vtkiov1alpha1.ExternalAccessList{}
	if err := r.List(ctx, externalAccessList); err != nil {
		logger.Logger.Error("Failed to list ExternalAccess resources in map function", slog.Any("error", err))
		return []reconcile.Request{}
	}

	// Create a reconciliation request for each ExternalAccess resource.
	requests := make([]reconcile.Request, len(externalAccessList.Items))
	for i, ea := range externalAccessList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      ea.Name,
				Namespace: ea.Namespace,
			},
		}
	}
	return requests
}
