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
)

// ClusterAccessReconciler reconciles a ClusterAccess object
type ClusterAccessReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	StatusNeedUpdate bool
}

// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=clusteraccesses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=clusteraccesses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=clusteraccesses/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClusterAccessReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger.Logger.Debug("Starting reconciliation for ClusterAccess object", "name", req.NamespacedName)
	access := &vtkiov1alpha1.ClusterAccess{}
	var deployedNetpols []vtkiov1alpha1.Netpol

	if err := r.Get(ctx, req.NamespacedName, access); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Logger.Debug("ClusterAccess resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Logger.Error("Failed to get ClusterAccess resource.", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	// Check for expiration and handle deletion if necessary.
	if r.isExpired(access) {
		logger.Logger.Info("ClusterAccess resource has expired, deleting it now.", "name", access.Name)
		if err := r.Delete(ctx, access); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete expired ClusterAccess resource: %w", err)
		}
		// A deletion request was sent, no need to do further work in this reconcile loop.
		return ctrl.Result{}, nil
	}

	// At each reconciliation, check for any NetworkPolicies owned by this ClusterAccess
	// whose corresponding Service has been deleted.
	if err := r.cleanupOrphanedClusterAccessNetworkPolicies(ctx, access); err != nil {
		logger.Logger.Error("Error during cleanup of orphaned NetworkPolicies.", slog.Any("error", err))
		return ctrl.Result{}, err // Requeue on cleanup failure
	}

	// Convert the LabelSelector from the spec into a selector object that the client can use.
	selector, err := metav1.LabelSelectorAsSelector(access.Spec.ServiceSelector)
	if err != nil {
		logger.Logger.Error("Error converting label selector.", slog.Any("error", err), "selector", access.Spec.ServiceSelector)
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
		annotationTargets, hasTargetsAnnotation := annotations[ClusterAccessTargetsAnnotation]
		annotationDirection, hasDirectionAnnotation := annotations[ClusterAccessDirectionAnnotation]

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

			var specTargets []string
			for _, target := range access.Spec.Targets {
				if target.ServiceName != "" && target.Namespace != "" {
					specTargets = append(specTargets, fmt.Sprintf("%s,%s", target.ServiceName, target.Namespace))
				}
			}
			targetsStr = strings.Join(specTargets, ";")
			logger.Logger.Debug(
				"Annotations targets not found on service, falling back to ClusterAccess spec",
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
			direction = access.Spec.Direction

			logger.Logger.Debug(
				"Annotations direction not found on service, falling back to ClusterAccess spec",
				"service",
				service.Name,
				"namespace",
				service.Namespace,
			)
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
				"Invalid direction value in service annotation, skipping service",
				"service", service.Name,
				"namespace", service.Namespace,
				"annotation", ClusterAccessDirectionAnnotation,
				"invalid_value", annotations[ClusterAccessDirectionAnnotation],
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
		targets := strings.Split(targetsStr, ";")
		for _, targetPair := range targets {
			trimmedPair := strings.TrimSpace(targetPair)
			if trimmedPair == "" {
				continue
			}

			parts := strings.Split(trimmedPair, ",")
			if len(parts) != 2 {
				logger.Logger.Error(
					"Invalid target format in service annotation",
					"service", service.Name, "namespace", service.Namespace,
					"annotation_value", trimmedPair, "expected_format", "name,namespace",
				)
				continue // Skip malformed target
			}
			targetName := strings.TrimSpace(parts[0])
			targetNs := strings.TrimSpace(parts[1])

			logger.Logger.Debug("Processing target for source service", "target_name", targetName, "target_namespace", targetNs)

			targetSvcSelector, _, err := r.getServiceDetails(ctx, targetName, targetNs)
			if err != nil {
				logger.Logger.Error("Error getting target service details.", slog.Any("error", err), "target_service", targetName)
				continue // Skip this target if its details can't be fetched
			}

			// Define common labels for both network policies
			netpolLabels := map[string]string{
				ClusterAccessOwnerLabel:                 access.Name,
				ClusterAccessServiceOwnerNameLabel:      service.Name,
				ClusterAccessServiceOwnerNamespaceLabel: service.Namespace,
			}
			var directionSuffix string
			switch direction {
			case "ingress":
				directionSuffix = "ing"
			case "egress":
				directionSuffix = "egr"
			case "all":
				directionSuffix = "all"
			}

			// This policy controls traffic leaving (egress) or entering (ingress) the source service.
			sourceNetpolName := fmt.Sprintf(
				"cluster-%s--%s-%s---%s-%s-%s",
				access.Name,
				service.Namespace,
				service.Name,
				targetName,
				targetNs,
				directionSuffix,
			)

			sourceNetpol := networkpolicy.DefineAccessNetworkPolicy(
				sourceNetpolName,
				service.Namespace,
				sourceSvcSelector,
				targetSvcSelector,
				targetNs,
				sourcePorts,
				direction,
			)
			sourceNetpol.Labels = netpolLabels

			if err := r.deployResource(ctx, access, sourceNetpol, &networkingv1.NetworkPolicy{}, networkpolicy.ExtractNetpolSpec, false); err != nil {
				logger.Logger.Error("Error deploying source NetworkPolicy.", slog.Any("error", err), "netpol_name", sourceNetpolName)
			} else {
				logger.Logger.Debug("Successfully deployed source NetworkPolicy", "name", sourceNetpolName, "namespace", service.Namespace)
				deployedNetpols = append(deployedNetpols, vtkiov1alpha1.Netpol{
					Name:      sourceNetpolName,
					Namespace: targetNs,
				})

			}

			// Create the corresponding NetworkPolicy for the TARGET namespace ONLY IF Mirrored is true
			if access.Spec.Mirrored {
				logger.Logger.Debug(
					"Mirrored flag is set to true, creating corresponding NetworkPolicy in target namespace",
					"target_namespace",
					targetNs,
				)
				var mirroredDirection string
				var mirroredDirectionSuffix string
				switch direction {
				case "egress":
					mirroredDirection = "ingress" // If source has egress, target needs ingress
					mirroredDirectionSuffix = "ing"
				case "ingress":
					mirroredDirection = "egress" // If source has ingress, target needs egress
					mirroredDirectionSuffix = "egr"
				case "all":
					mirroredDirection = "all" // If both are allowed at source, allow both at target
					mirroredDirectionSuffix = "all"
				}

				targetNetpolName := fmt.Sprintf(
					"cluster-%s--%s-%s---%s-%s-%s-mirror",
					access.Name,
					targetNs,
					targetName,
					service.Namespace,
					service.Name,
					mirroredDirectionSuffix,
				)

				targetNetpol := networkpolicy.DefineAccessNetworkPolicy(
					targetNetpolName,
					targetNs,          // Policy is created in the TARGET service's namespace
					targetSvcSelector, // Policy applies to pods of the TARGET service
					sourceSvcSelector,
					service.Namespace,
					sourcePorts,
					mirroredDirection,
				)
				targetNetpol.Labels = netpolLabels
				targetNetpol.Labels["maxtac.vtk.io.access/mirror"] = "true"

				if err := r.deployResource(ctx, access, targetNetpol, &networkingv1.NetworkPolicy{}, networkpolicy.ExtractNetpolSpec, false); err != nil {
					logger.Logger.Error("Error deploying target NetworkPolicy.", slog.Any("error", err), "netpol_name", targetNetpolName)
				} else {
					logger.Logger.Debug("Successfully deployed target NetworkPolicy", "name", targetNetpolName, "namespace", targetNs)
					deployedNetpols = append(deployedNetpols, vtkiov1alpha1.Netpol{
						Name:      targetNetpolName,
						Namespace: targetNs,
					})
				}
			}
		}
	}

	// Set access ready status
	// Update netpols in status
	finalNetpols, err := r.reconcileNetpolStatus(ctx, access, deployedNetpols)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Update services in status
	finalServices, err := r.reconcileServiceStatus(ctx, access, matchedServices)
	if err != nil {
		return ctrl.Result{}, err
	}

	access.Status.Netpols = finalNetpols
	access.Status.Services = finalServices
	if err := r.updateStatus(ctx, access); err != nil {
		logger.Logger.Error("Error updating status.", slog.Any("error", err))
	}

	// Calculate and return a targeted requeue duration if the resource has an expiration.
	if access.Status.ExpirationTimestamp != nil {
		timeRemaining := time.Until(access.Status.ExpirationTimestamp.Time)
		if timeRemaining > 0 {
			logger.Logger.Debug("Requeuing to check for expiration.", "name", access.Name, "duration", timeRemaining)
			return ctrl.Result{RequeueAfter: timeRemaining}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterAccessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate to filter for services with the specific annotations.

	return ctrl.NewControllerManagedBy(mgr).
		// Watch for changes to primary resource ClusterAccess
		For(&vtkiov1alpha1.ClusterAccess{}).
		// Watch for changes to secondary resource NetworkPolicy and requeue the owner ClusterAccess
		Owns(&networkingv1.NetworkPolicy{}).
		// Watch for changes to Services and enqueue reconcile requests for ClusterAccess resources
		// that are configured to watch the Service's namespace.
		Watches(
			&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(r.findClusterAccessForService),
		).
		Named("clusteraccess").
		Complete(r)
}

// findClusterAccessForService is a handler.MapFunc that finds all ClusterAccess resources.
// we will trigger the reconciliation of all clusteraccesses here on service event.
func (r *ClusterAccessReconciler) findClusterAccessForService(ctx context.Context, obj client.Object) []reconcile.Request {
	service, ok := obj.(*corev1.Service)
	if !ok {
		logger.Logger.Error("Unexpected type received in map function for Service watch", "type", fmt.Sprintf("%T", obj))
		return []reconcile.Request{}
	}

	logger.Logger.Debug(
		"Service change detected, triggering reconciliation for all ClusterAccess resources",
		"service", service.Name,
		"namespace", service.GetNamespace(),
	)

	// List all ClusterAccess resources cluster-wide.
	clusterAccessList := &vtkiov1alpha1.ClusterAccessList{}
	if err := r.List(ctx, clusterAccessList); err != nil {
		logger.Logger.Error("Failed to list ClusterAccess resources in map function", slog.Any("error", err))
		return []reconcile.Request{}
	}

	// Create a reconciliation request for each ClusterAccess resource.
	requests := make([]reconcile.Request, len(clusterAccessList.Items))
	for i, ea := range clusterAccessList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      ea.Name,
				Namespace: ea.Namespace,
			},
		}
	}
	return requests
}
