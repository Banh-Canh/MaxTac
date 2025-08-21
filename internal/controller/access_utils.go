package controller

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vtkiov1alpha1 "github.com/Banh-Canh/maxtac/api/v1alpha1"
	"github.com/Banh-Canh/maxtac/internal/utils/logger"
	"github.com/Banh-Canh/maxtac/internal/utils/utils"
)

func (r *AccessReconciler) deployResource(
	ctx context.Context,
	access *vtkiov1alpha1.Access,
	resource, resourceType client.Object,
	extractSpecFunc func(client.Object) any,
	disableOwnerRef bool,
) error {
	existingResource := resourceType.DeepCopyObject().(client.Object)
	resourceKey := client.ObjectKey{Name: resource.GetName(), Namespace: resource.GetNamespace()}

	err := r.Get(ctx, resourceKey, existingResource)
	if err != nil {
		if err := r.createResource(ctx, access, resource, disableOwnerRef); err != nil {
			return err
		}
	} else {
		if err := r.reconcileResource(ctx, access, resource, existingResource, extractSpecFunc, disableOwnerRef); err != nil {
			return err
		}
	}
	if !access.DeletionTimestamp.IsZero() {
		if err := r.deleteResource(ctx, access, resource, resourceType); err != nil {
			return err
		}
	}
	return nil
}

func (r *AccessReconciler) createResource(
	ctx context.Context,
	owner *vtkiov1alpha1.Access,
	resource client.Object,
	disableOwnerRef bool,
) error {
	if !disableOwnerRef {
		if err := controllerutil.SetControllerReference(owner, resource, r.Scheme); err != nil { // Set resource ownerRef
			logger.Logger.Error("Failed to set deployment controller reference", slog.Any("error", err))
			return err
		}
	}
	if err := r.Create(ctx, resource); err != nil {
		if err = r.setCondition(
			owner,
			reflect.TypeOf(resource).Elem().Name()+"DeployReady",
			reflect.TypeOf(resource).Elem().Name()+"CreateFail",
			fmt.Sprintf("Failed to created some '%s' child resources.", reflect.TypeOf(resource).Elem().Name()),
			metav1.ConditionFalse,
		); err != nil {
			logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
		}
		logger.Logger.Error("Error creating resource.", slog.Any("error", err))
		return err
	}
	// resource created successfully
	controllerutil.AddFinalizer(resource, accessFinalizer)
	if err := r.setCondition(
		owner,
		reflect.TypeOf(resource).Elem().Name()+"DeployReady",
		reflect.TypeOf(resource).Elem().Name()+"CreateSuccess",
		fmt.Sprintf("Successfully created all '%s' child resources.", reflect.TypeOf(resource).Elem().Name()),
		metav1.ConditionTrue,
	); err != nil {
		logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
	}

	logger.Logger.Debug("Created resource.", slog.String("resource", resource.GetName()))
	return nil
}

func (r *AccessReconciler) deleteResource(
	ctx context.Context,
	owner *vtkiov1alpha1.Access,
	resource client.Object,
	resourceType client.Object,
) error {
	existingResource := resourceType.DeepCopyObject().(client.Object)
	resourceKey := client.ObjectKey{Name: resource.GetName(), Namespace: resource.GetNamespace()}
	// Delete the resource
	if err := r.Get(ctx, resourceKey, existingResource); err == nil {
		controllerutil.RemoveFinalizer(resource, accessFinalizer)
		if err := r.Delete(ctx, resource); err != nil {
			if err = r.setCondition(
				owner,
				reflect.TypeOf(resource).Elem().Name()+"DeployReady",
				reflect.TypeOf(resource).Elem().Name()+"DeleteFail",
				fmt.Sprintf("Failed to clean up some unwanted '%s' child resources.", reflect.TypeOf(resource).Elem().Name()),
				metav1.ConditionFalse,
			); err != nil {
				logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
			}
			logger.Logger.Error(
				"Error deleting resource.",
				slog.Any("error", err),
				slog.String("kind", existingResource.GetObjectKind().GroupVersionKind().Kind),
				slog.String("name", existingResource.GetName()),
			)
			return nil
		}
		// Resource deleted successfully
		if err = r.setCondition(
			owner,
			reflect.TypeOf(resource).Elem().Name()+"DeployReady",
			reflect.TypeOf(resource).Elem().Name()+"DeleteSuccess",
			fmt.Sprintf("Successfully cleaned up unwanted '%s' child resources.", reflect.TypeOf(resource).Elem().Name()),
			metav1.ConditionTrue,
		); err != nil {
			logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
		}

		logger.Logger.Info(
			"Deleted resource.",
			slog.String("kind", existingResource.GetObjectKind().GroupVersionKind().Kind),
			slog.String("name", existingResource.GetName()),
		)
		return nil
	}
	return nil
}

func (r *AccessReconciler) reconcileResource(
	ctx context.Context,
	owner *vtkiov1alpha1.Access,
	resource, existingResource client.Object,
	specExtractor func(client.Object) any,
	disableOwnerRef bool,
) error {
	// resource exists, get existing resource
	resourceKey := client.ObjectKey{Name: resource.GetName(), Namespace: resource.GetNamespace()}
	if err := r.Get(ctx, resourceKey, existingResource); err != nil {
		if err = r.setCondition(
			owner,
			reflect.TypeOf(resource).Elem().Name()+"DeployReady",
			reflect.TypeOf(resource).Elem().Name()+"SyncFail",
			fmt.Sprintf("Failed to get existing '%s' resource for comparison.", reflect.TypeOf(resource).Elem().Name()),
			metav1.ConditionFalse,
		); err != nil {
			logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
		}

		logger.Logger.Error(
			"Error getting existing resource for comparison.",
			slog.Any("error", err),
		)

		return nil
	}
	// Merge labels and annotations
	existingLabels := existingResource.GetLabels()
	if existingLabels == nil {
		existingLabels = make(map[string]string)
	}
	existingAnnotations := existingResource.GetAnnotations()
	newLabels := resource.GetLabels()
	if newLabels == nil {
		newLabels = make(map[string]string)
	}
	newAnnotations := resource.GetAnnotations()

	labelsChanged := false
	for key, value := range newLabels {
		if existingValue, exists := existingLabels[key]; !exists || existingValue != value {
			existingLabels[key] = value
			labelsChanged = true
		}
	}

	maps.Copy(existingLabels, newLabels)
	maps.Copy(existingAnnotations, newAnnotations)
	resource.SetLabels(existingLabels)
	resource.SetAnnotations(existingAnnotations)
	// Compare specs
	currentSpec := specExtractor(resource)
	existingSpec := specExtractor(existingResource)
	if !reflect.DeepEqual(currentSpec, existingSpec) || labelsChanged {
		// Spec has changed, update the resource
		resource.SetResourceVersion(existingResource.GetResourceVersion())
		if !disableOwnerRef {
			if err := controllerutil.SetControllerReference(owner, resource, r.Scheme); err != nil { // Set resource ownerRef
				logger.Logger.Error(
					"Failed to set deployment controller reference.",
					slog.Any("error", err),
					slog.String("kind", existingResource.GetObjectKind().GroupVersionKind().Kind),
					slog.String("name", existingResource.GetName()),
				)
				return err
			}
		}
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			return r.Update(ctx, resource)
		})
		if err != nil {
			if err = r.setCondition(
				owner,
				reflect.TypeOf(resource).Elem().Name()+"DeployReady",
				reflect.TypeOf(resource).Elem().Name()+"SyncFail",
				fmt.Sprintf("Failed to sync some '%s' child resources.", reflect.TypeOf(resource).Elem().Name()),
				metav1.ConditionFalse,
			); err != nil {
				logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
			}

			logger.Logger.Error(
				"Error updating resource.",
				slog.Any("error", err),
				slog.String("kind", existingResource.GetObjectKind().GroupVersionKind().Kind),
				slog.String("name", existingResource.GetName()),
			)

			return nil
		}
		if err = r.setCondition(
			owner,
			reflect.TypeOf(resource).Elem().Name()+"DeployReady",
			reflect.TypeOf(resource).Elem().Name()+"SyncSuccess",
			fmt.Sprintf("Successfully synced all '%s' child resources.", reflect.TypeOf(resource).Elem().Name()),
			metav1.ConditionTrue,
		); err != nil {
			logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
		}
		logger.Logger.Info(
			"The resource has been reconciled.",
			slog.String("kind", existingResource.GetObjectKind().GroupVersionKind().Kind),
			slog.String("name", existingResource.GetName()),
		)

	}
	if err := r.setCondition(
		owner,
		reflect.TypeOf(resource).Elem().Name()+"DeployReady",
		reflect.TypeOf(resource).Elem().Name()+"SyncSuccess",
		fmt.Sprintf("No change in '%s' child resources.", reflect.TypeOf(resource).Elem().Name()),
		metav1.ConditionTrue,
	); err != nil {
		logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
	}

	return nil
}

// cleanupOrphanedNetworkPolicies finds all NetworkPolicies managed by this Access
// and deletes any for which the corresponding Service no longer exists.
func (r *AccessReconciler) cleanupOrphanedAccessNetworkPolicies(
	ctx context.Context,
	access *vtkiov1alpha1.Access,
) error {
	logger.Logger.Info("Starting cleanup of orphaned NetworkPolicies", "access", access.Name)

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			AccessOwnerLabel: access.Name,
		},
	}
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		if err := r.setCondition(
			access,
			"NetpolsGCNotReady",
			"NetpolsGCUnsuccessful",
			"Failed to garbage collect any orphaned netpols resources.",
			metav1.ConditionFalse,
		); err != nil {
			logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
		}
		return fmt.Errorf("failed to create selector for cleanup: %w", err)
	}

	// List all NetworkPolicies matching the selector cluster-wide.
	ownedNetPols := &networkingv1.NetworkPolicyList{}
	if err := r.List(ctx, ownedNetPols, &client.ListOptions{LabelSelector: selector}); err != nil {
		if err := r.setCondition(
			access,
			"NetpolsGCNotReady",
			"NetpolsGCUnsuccessful",
			"Failed to garbage collect any orphaned netpols resources.",
			metav1.ConditionFalse,
		); err != nil {
			logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
		}
		return fmt.Errorf("failed to list owned NetworkPolicies for cleanup: %w", err)
	}

	for _, netpol := range ownedNetPols.Items {
		labels := netpol.GetLabels()
		serviceName, okName := labels[AccessServiceOwnerNameLabel]
		serviceNamespace, okNamespace := labels[AccessServiceOwnerNamespaceLabel]

		if !okName || !okNamespace {
			logger.Logger.Warn("Found owned NetworkPolicy with missing service owner labels, skipping cleanup check", "netpol", netpol.Name)
			continue
		}

		// Check if the owner service still exists.
		service := &corev1.Service{}
		err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, service)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				// The service is gone, so the NetworkPolicy is orphaned. Delete it.
				logger.Logger.Debug(
					"Deleting orphaned NetworkPolicy as its owner service is not found",
					"netpol",
					netpol.Name,
					"service",
					serviceName,
					"namespace",
					serviceNamespace,
				)
				if deleteErr := r.Delete(ctx, &netpol); deleteErr != nil {
					// Log the error but continue trying to clean up others.
					logger.Logger.Error("Failed to delete orphaned NetworkPolicy", slog.Any("error", deleteErr), "netpol", netpol.Name)
					if err := r.setCondition(
						access,
						"NetpolsGCNotReady",
						"NetpolsGCUnsuccessful",
						"Failed to garbage collect any orphaned netpols resources.",
						metav1.ConditionFalse,
					); err != nil {
						logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
					}
				}
				if err := r.setCondition(
					access,
					"NetpolsGCReady",
					"NetpolsGCSuccess",
					"Successfully garbage collected any orphaned netpols resources.",
					metav1.ConditionTrue,
				); err != nil {
					logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
				}

			} else {
				// Another error occurred while trying to get the service.
				logger.Logger.Error("Error checking for owner service existence during cleanup", slog.Any("error", err), "netpol", netpol.Name)
				if err := r.setCondition(
					access,
					"NetpolsGCNotReady",
					"NetpolsGCUnsuccessful",
					"Failed to garbage collect any orphaned netpols resources.",
					metav1.ConditionFalse,
				); err != nil {
					logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
				}
				// Continue to the next netpol, but the main reconcile loop might return this error later.
			}
		}
		// If err is nil, the service exists, so we do nothing to this NetworkPolicy.
	}
	return nil
}

func (r *AccessReconciler) getServiceDetails(
	ctx context.Context,
	serviceName string,
	namespace string,
) (map[string]string, []corev1.ServicePort, error) {
	serviceKey := types.NamespacedName{Name: serviceName, Namespace: namespace}
	service := &corev1.Service{}

	// Fetch the service object from the cluster
	if err := r.Get(ctx, serviceKey, service); err != nil {
		if k8serrors.IsNotFound(err) {
			// Return nil for all values on error
			return nil, nil, fmt.Errorf("service '%s' in namespace '%s' not found", serviceName, namespace)
		}
		return nil, nil, fmt.Errorf("failed to get service '%s': %w", serviceName, err)
	}

	// Ensure the service has a selector defined
	if len(service.Spec.Selector) == 0 {
		return nil, nil, fmt.Errorf("service '%s' does not have a selector", serviceName)
	}

	// Ensure the service has ports defined
	if len(service.Spec.Ports) == 0 {
		return nil, nil, fmt.Errorf("service '%s' does not have any ports defined", serviceName)
	}

	// Return the selector and the ports
	return service.Spec.Selector, service.Spec.Ports, nil
}

func (r *AccessReconciler) reconcileNetpolStatus(
	ctx context.Context,
	access *vtkiov1alpha1.Access,
	deployedNetpols []vtkiov1alpha1.Netpol,
) ([]vtkiov1alpha1.Netpol, error) {
	var existingNetpols []vtkiov1alpha1.Netpol
	for _, np := range access.Status.Netpols {
		netpol := &networkingv1.NetworkPolicy{}
		err := r.Get(ctx, types.NamespacedName{Name: np.Name, Namespace: np.Namespace}, netpol)
		if err == nil {
			existingNetpols = append(existingNetpols, np)
		} else if !k8serrors.IsNotFound(err) {
			logger.Logger.Error("Error fetching existing netpol", slog.Any("error", err),
				"name", np.Name, "namespace", np.Namespace)
			if err = r.setCondition(
				access,
				"NetpolsListNotReady",
				"NetpolsSyncUnSuccessful",
				"Unsuccessfully synced all matching netpols child resources.",
				metav1.ConditionFalse,
			); err != nil {
				logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
			}
			return nil, err
		} else {
			logger.Logger.Debug("Removing non-existent netpol from status",
				"name", np.Name, "namespace", np.Namespace)
			r.StatusNeedUpdate = true
		}
	}

	// Build final list
	finalNetpols := make([]vtkiov1alpha1.Netpol, 0, len(existingNetpols)+len(deployedNetpols))
	finalNetpols = append(finalNetpols, existingNetpols...)
	finalNetpols = append(finalNetpols, deployedNetpols...)

	// Deduplicate
	unique := make(map[string]vtkiov1alpha1.Netpol)
	for _, np := range finalNetpols {
		key := fmt.Sprintf("%s/%s", np.Namespace, np.Name)
		unique[key] = np
	}

	finalNetpols = finalNetpols[:0]
	for _, np := range unique {
		finalNetpols = append(finalNetpols, np)
	}

	if err := r.setCondition(
		access,
		"NetpolsListReady",
		"NetpolsSyncSuccess",
		"Successfully synced all matching netpols child resources.",
		metav1.ConditionTrue,
	); err != nil {
		logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
	}

	return finalNetpols, nil
}

func (r *AccessReconciler) reconcileServiceStatus(
	ctx context.Context,
	access *vtkiov1alpha1.Access,
	matchedServices []vtkiov1alpha1.SvcRef,
) ([]vtkiov1alpha1.SvcRef, error) {
	var existingServices []vtkiov1alpha1.SvcRef
	for _, s := range access.Status.Services {
		svc := &corev1.Service{}
		err := r.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: s.Namespace}, svc)
		if err == nil {
			existingServices = append(existingServices, s)
		} else if !k8serrors.IsNotFound(err) {
			if err = r.setCondition(
				access,
				"ServicesListNotReady",
				"ServicesSyncUnSuccessful",
				"Unsuccessfully synced all matching services child resources.",
				metav1.ConditionFalse,
			); err != nil {
				logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
			}
			logger.Logger.Error("Error fetching existing service", slog.Any("error", err),
				"name", s.Name, "namespace", s.Namespace)
			return nil, err
		} else {
			logger.Logger.Debug("Removing non-existent service from status",
				"name", s.Name, "namespace", s.Namespace)
			r.StatusNeedUpdate = true
		}
	}

	// Build final list
	finalServices := make([]vtkiov1alpha1.SvcRef, 0, len(existingServices)+len(matchedServices))
	finalServices = append(finalServices, existingServices...)
	finalServices = append(finalServices, matchedServices...)

	// Deduplicate
	unique := make(map[string]vtkiov1alpha1.SvcRef)
	for _, s := range finalServices {
		key := fmt.Sprintf("%s/%s", s.Namespace, s.Name)
		unique[key] = s
	}

	finalServices = finalServices[:0]
	for _, s := range unique {
		finalServices = append(finalServices, s)
	}
	if err := r.setCondition(
		access,
		"ServicesListReady",
		"ServicesSyncSuccess",
		"Successfully synced all matching services child resources.",
		metav1.ConditionTrue,
	); err != nil {
		logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
	}

	return finalServices, nil
}

// isExpired is a helper function that checks if a resource has expired.
func (r *AccessReconciler) isExpired(access *vtkiov1alpha1.Access) bool {
	// If the user has specified a duration and we haven't calculated the expiration timestamp yet...
	if access.Spec.Duration != "" && access.Status.ExpirationTimestamp == nil {
		// Use a helper to parse the duration string, including 'd' for days.
		duration, err := utils.ParseDuration(access.Spec.Duration)
		if err != nil {
			logger.Logger.Error("Invalid duration format.", "duration", access.Spec.Duration, "error", err)
			return false // Do not expire, the user needs to fix the spec.
		}

		// Calculate the absolute expiration time based on the resource's creation timestamp.
		expirationTime := access.CreationTimestamp.Add(duration)

		// Create a variable to hold the value so we can take its address.
		newTime := metav1.NewTime(expirationTime)
		access.Status.ExpirationTimestamp = &newTime
		if err := r.setCondition(
			access,
			"ExpireReady",
			"ExpireSuccess",
			"The resource has an expiration date.",
			metav1.ConditionTrue,
		); err != nil {
			logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
			return false
		}

		logger.Logger.Info("Expiration timestamp set.", "name", access.Name, "expiresAt", access.Status.ExpirationTimestamp.Time)
	}
	// Check if the calculated expiration timestamp has passed.
	if access.Status.ExpirationTimestamp != nil && time.Now().After(access.Status.ExpirationTimestamp.Time) {
		return true
	}

	return false // The resource is not expired.
}
