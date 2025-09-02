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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	vtkiov1alpha1 "github.com/Banh-Canh/maxtac/api/v1alpha1"
	"github.com/Banh-Canh/maxtac/internal/k8s/networkpolicy"
	"github.com/Banh-Canh/maxtac/internal/utils/logger"
)

// AccessReconciler reconciles a Access object
type AccessReconciler struct {
	BaseController
}

// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=accesses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=accesses/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=maxtac.vtk.io,resources=accesses/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AccessReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	access := &vtkiov1alpha1.Access{}

	config := ReconcileConfig{
		ResourceName:     "Access",
		IsCluster:        false,
		AnnotationConfig: GetAnnotationConfig("access"),
	}

	return r.CommonReconcileLogic(ctx, req, access, config, r.processAccessTargets)
}

func (r *AccessReconciler) processAccessTargets(
	ctx context.Context,
	service corev1.Service,
	targetsStr string,
	direction string,
	resource client.Object,
) ([]vtkiov1alpha1.Netpol, error) {
	var deployedNetpols []vtkiov1alpha1.Netpol

	sourceSvcSelector, sourcePorts, err := r.getServiceDetails(ctx, service.Name, service.Namespace)
	if err != nil {
		logger.Logger.Debug("Error getting source service details.", slog.Any("error", err), "service", service.Name)
		return deployedNetpols, nil // Return partial results instead of failing entirely
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
			logger.Logger.Debug("Error getting target service details.", slog.Any("error", err), "target_service", targetName)
			continue // Skip this target if its details can't be fetched
		}

		// Define common labels for both network policies
		netpolLabels := map[string]string{
			AccessOwnerLabel:                 resource.GetName(),
			AccessServiceOwnerNameLabel:      service.Name,
			AccessServiceOwnerNamespaceLabel: service.Namespace,
		}

		directionSuffix := r.getDirectionSuffix(direction)

		// This policy controls traffic leaving (egress) or entering (ingress) the source service.
		sourceNetpolName := fmt.Sprintf(
			"%s--%s-%s---%s-%s-%s",
			resource.GetName(),
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

		if err := r.deployResource(ctx, resource.(*vtkiov1alpha1.Access), sourceNetpol, &networkingv1.NetworkPolicy{}, networkpolicy.ExtractNetpolSpec, false); err != nil {
			logger.Logger.Error("Error deploying source NetworkPolicy.", slog.Any("error", err), "netpol_name", sourceNetpolName)
		} else {
			logger.Logger.Debug("Successfully deployed source NetworkPolicy", "name", sourceNetpolName, "namespace", service.Namespace)
			deployedNetpols = append(deployedNetpols, vtkiov1alpha1.Netpol{
				Name:      sourceNetpolName,
				Namespace: service.Namespace,
			})
		}
	}

	return deployedNetpols, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccessReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Predicate to filter for services with the specific annotations.

	return ctrl.NewControllerManagedBy(mgr).
		// Watch for changes to primary resource Access
		For(&vtkiov1alpha1.Access{}).
		// Watch for changes to secondary resource NetworkPolicy and requeue the owner Access
		Owns(&networkingv1.NetworkPolicy{}).
		// Watch for changes to Services and enqueue reconcile requests for Access resources
		// that are configured to watch the Service's namespace.
		Watches(
			&corev1.Service{},
			handler.EnqueueRequestsFromMapFunc(r.findAccessForService),
		).
		Named("access").
		Complete(r)
}

// findAccessForService is a handler.MapFunc that finds all Access resources.
// we will trigger the reconciliation of all accesses here on service event.
func (r *AccessReconciler) findAccessForService(ctx context.Context, obj client.Object) []reconcile.Request {
	service, ok := obj.(*corev1.Service)
	if !ok {
		logger.Logger.Error("Unexpected type received in map function for Service watch", "type", fmt.Sprintf("%T", obj))
		return []reconcile.Request{}
	}

	logger.Logger.Debug(
		"Service change detected, triggering reconciliation for all Access resources",
		"service", service.Name,
		"namespace", service.GetNamespace(),
	)

	// List all Access resources cluster-wide.
	accessList := &vtkiov1alpha1.AccessList{}
	if err := r.List(ctx, accessList); err != nil {
		logger.Logger.Error("Failed to list Access resources in map function", slog.Any("error", err))
		return []reconcile.Request{}
	}

	// Create a reconciliation request for each Access resource.
	requests := make([]reconcile.Request, len(accessList.Items))
	for i, ea := range accessList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      ea.Name,
				Namespace: ea.Namespace,
			},
		}
	}
	return requests
}
