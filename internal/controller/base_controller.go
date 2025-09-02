package controller

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vtkiov1alpha1 "github.com/Banh-Canh/maxtac/api/v1alpha1"
	"github.com/Banh-Canh/maxtac/internal/utils/logger"
	"github.com/Banh-Canh/maxtac/internal/utils/utils"
)

const (
	apiGroup        = "vtkiov1alpha1"
	accessFinalizer = apiGroup + "/finalizer"
)

// BaseController provides common functionality for all access controllers
type BaseController struct {
	client.Client
	Scheme           *runtime.Scheme
	StatusNeedUpdate bool
}

// ResourceWithExpiration interface for resources that support expiration
type ResourceWithExpiration interface {
	client.Object
	GetDuration() string
	GetExpirationTimestamp() *metav1.Time
	SetExpirationTimestamp(*metav1.Time)
	GetCreationTimestamp() metav1.Time
}

// ResourceWithStatus interface for resources that have status fields
type ResourceWithStatus interface {
	client.Object
	GetNetpols() []vtkiov1alpha1.Netpol
	SetNetpols([]vtkiov1alpha1.Netpol)
	GetServices() []vtkiov1alpha1.SvcRef
	SetServices([]vtkiov1alpha1.SvcRef)
}

// ServiceSelector interface for resources with service selectors
type ServiceSelector interface {
	GetServiceSelector() *metav1.LabelSelector
}

// TargetProvider interface for getting targets from resources
type TargetProvider interface {
	GetTargets() []string
	GetDirection() string
}

// AnnotationConfig holds configuration for annotations and labels
type AnnotationConfig struct {
	TargetsAnnotation          string
	DirectionAnnotation        string
	OwnerLabel                 string
	ServiceOwnerNameLabel      string
	ServiceOwnerNamespaceLabel string
}

// ReconcileConfig holds configuration for the reconciliation process
type ReconcileConfig struct {
	ResourceName string
	IsCluster    bool
	AnnotationConfig
}

// CommonReconcileLogic performs the common reconciliation logic
func (r *BaseController) CommonReconcileLogic(
	ctx context.Context,
	req ctrl.Request,
	resource ResourceWithExpiration,
	config ReconcileConfig,
	processTargetsFunc func(context.Context, corev1.Service, string, string, client.Object) ([]vtkiov1alpha1.Netpol, error),
) (ctrl.Result, error) {
	logger.Logger.Debug(fmt.Sprintf("Starting reconciliation for %s object", config.ResourceName), "name", req.NamespacedName)
	var deployedNetpols []vtkiov1alpha1.Netpol

	if err := r.Get(ctx, req.NamespacedName, resource); err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Logger.Debug(fmt.Sprintf("%s resource not found. Ignoring since object must be deleted.", config.ResourceName))
			return ctrl.Result{}, nil
		}
		logger.Logger.Error(fmt.Sprintf("Failed to get %s resource.", config.ResourceName), slog.Any("error", err))
		return ctrl.Result{}, err
	}

	// Check for expiration and handle deletion if necessary
	if r.isExpired(resource) {
		logger.Logger.Info(fmt.Sprintf("%s resource has expired, deleting it now.", config.ResourceName), "name", resource.GetName())
		if err := r.Delete(ctx, resource); err != nil {
			if k8serrors.IsNotFound(err) {
				// Resource already deleted by another reconciliation loop - this is fine
				logger.Logger.Debug("Expired resource already deleted", "name", resource.GetName())
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, fmt.Errorf("failed to delete expired %s resource: %w", config.ResourceName, err)
		}
		return ctrl.Result{}, nil
	}

	// Cleanup orphaned NetworkPolicies
	if err := r.cleanupOrphanedNetworkPolicies(ctx, resource, config); err != nil {
		logger.Logger.Error("Error during cleanup of orphaned NetworkPolicies.", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	// Convert the LabelSelector from the spec into a selector object
	serviceSelector := resource.(ServiceSelector)
	selector, err := metav1.LabelSelectorAsSelector(serviceSelector.GetServiceSelector())
	if err != nil {
		logger.Logger.Error("Error converting label selector.", slog.Any("error", err), "selector", serviceSelector.GetServiceSelector())
		return ctrl.Result{}, fmt.Errorf("failed to convert label selector: %w", err)
	}

	// List services based on whether it's cluster-scoped or namespace-scoped
	serviceList := &corev1.ServiceList{}
	listOptions := &client.ListOptions{LabelSelector: selector}
	if !config.IsCluster {
		listOptions.Namespace = resource.GetNamespace()
	}
	if err := r.List(ctx, serviceList, listOptions); err != nil {
		logger.Logger.Error("Failed to list services with selector.", slog.Any("error", err), "selector", selector.String())
		return ctrl.Result{}, err
	}

	// Process each service
	var matchedServices []vtkiov1alpha1.SvcRef
	for _, service := range serviceList.Items {
		matchedServices = append(matchedServices, vtkiov1alpha1.SvcRef{
			Name:      service.Name,
			Namespace: service.Namespace,
		})

		targetsStr, direction := r.getTargetsAndDirection(service, resource, config.AnnotationConfig)
		if targetsStr == "" || direction == "" {
			logger.Logger.Debug(
				"No targets or direction defined in either annotations or spec; skipping service",
				"service", service.Name,
				"namespace", service.Namespace,
				"targets_empty", targetsStr == "",
				"direction_empty", direction == "",
			)
			continue
		}

		// Validate direction
		if !r.isValidDirection(direction) {
			logger.Logger.Error(
				"Invalid direction value",
				"service", service.Name,
				"namespace", service.Namespace,
				"value", direction,
				"expected_values", "ingress, egress, or all",
			)
			continue
		}

		logger.Logger.Debug(
			"Found matched service",
			"source_service", service.Name,
			"namespace", service.Namespace,
		)

		// Process targets for this service
		serviceNetpols, err := processTargetsFunc(ctx, service, targetsStr, direction, resource)
		if err != nil {
			logger.Logger.Error("Error processing targets for service", slog.Any("error", err), "service", service.Name)
			continue
		}
		deployedNetpols = append(deployedNetpols, serviceNetpols...)
	}

	// Update status if resource supports it
	if statusResource, ok := resource.(ResourceWithStatus); ok {
		finalNetpols, err := r.reconcileNetpolStatus(ctx, resource, deployedNetpols, config)
		if err != nil {
			return ctrl.Result{}, err
		}

		finalServices, err := r.reconcileServiceStatus(ctx, resource, matchedServices, config)
		if err != nil {
			return ctrl.Result{}, err
		}

		// Only update status if there are actual changes
		needsStatusUpdate := false
		currentNetpols := statusResource.GetNetpols()
		currentServices := statusResource.GetServices()

		if !r.netpolsEqual(currentNetpols, finalNetpols) || !r.servicesEqual(currentServices, finalServices) {
			needsStatusUpdate = true
		}

		if needsStatusUpdate {
			statusResource.SetNetpols(finalNetpols)
			statusResource.SetServices(finalServices)
			if err := r.updateStatus(ctx, resource); err != nil {
				logger.Logger.Debug("Error updating status (will retry).", slog.Any("error", err))
				// Don't return error, let it retry on next reconciliation
			}
		}
	}

	// Handle requeue for expiration
	if resource.GetExpirationTimestamp() != nil {
		timeRemaining := time.Until(resource.GetExpirationTimestamp().Time)
		if timeRemaining > 0 {
			logger.Logger.Debug("Requeuing to check for expiration.", "name", resource.GetName(), "duration", timeRemaining)
			return ctrl.Result{RequeueAfter: timeRemaining}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *BaseController) getTargetsAndDirection(service corev1.Service, resource client.Object, config AnnotationConfig) (string, string) {
	var targetsStr, direction string

	annotations := service.GetAnnotations()
	annotationTargets, hasTargetsAnnotation := annotations[config.TargetsAnnotation]
	annotationDirection, hasDirectionAnnotation := annotations[config.DirectionAnnotation]

	// Get targets - prefer annotations over spec
	if hasTargetsAnnotation {
		logger.Logger.Debug(
			"Using targets from service annotations",
			"service", service.Name,
			"namespace", service.Namespace,
		)
		targetsStr = annotationTargets
	} else {
		// Fallback to spec based on resource type
		if targetProvider, ok := resource.(TargetProvider); ok {
			targets := targetProvider.GetTargets()
			if len(targets) > 0 && targets[0] != "" {
				targetsStr = targets[0] // Already formatted as needed by each resource type
				logger.Logger.Debug(
					"Annotations targets not found on service, falling back to spec",
					"service", service.Name,
					"namespace", service.Namespace,
				)
			}
		}
	}

	// Get direction - prefer annotations over spec
	if hasDirectionAnnotation {
		logger.Logger.Debug(
			"Using direction from service annotations",
			"service", service.Name,
			"namespace", service.Namespace,
		)
		direction = annotationDirection
	} else {
		if targetProvider, ok := resource.(TargetProvider); ok {
			specDirection := targetProvider.GetDirection()
			if specDirection != "" {
				direction = specDirection
				logger.Logger.Debug(
					"Annotations direction not found on service, falling back to spec",
					"service", service.Name,
					"namespace", service.Namespace,
				)
			}
		}
	}

	return targetsStr, direction
}

func (r *BaseController) isValidDirection(direction string) bool {
	direction = strings.ToLower(direction)
	validDirections := map[string]struct{}{
		"ingress": {},
		"egress":  {},
		"all":     {},
	}
	_, isValid := validDirections[direction]
	return isValid
}

func (r *BaseController) deployResource(
	ctx context.Context,
	owner client.Object,
	resource, resourceType client.Object,
	extractSpecFunc func(client.Object) any,
	disableOwnerRef bool,
) error {
	existingResource := resourceType.DeepCopyObject().(client.Object)
	resourceKey := client.ObjectKey{Name: resource.GetName(), Namespace: resource.GetNamespace()}

	err := r.Get(ctx, resourceKey, existingResource)
	if err != nil {
		if err := r.createResource(ctx, owner, resource, disableOwnerRef); err != nil {
			return err
		}
	} else {
		if err := r.reconcileResource(ctx, owner, resource, existingResource, extractSpecFunc, disableOwnerRef); err != nil {
			return err
		}
	}
	if !owner.GetDeletionTimestamp().IsZero() {
		if err := r.deleteResource(ctx, owner, resource, resourceType); err != nil {
			return err
		}
	}
	return nil
}

func (r *BaseController) createResource(
	ctx context.Context,
	owner client.Object,
	resource client.Object,
	disableOwnerRef bool,
) error {
	if !disableOwnerRef {
		if err := controllerutil.SetControllerReference(owner, resource, r.Scheme); err != nil {
			logger.Logger.Error("Failed to set deployment controller reference", slog.Any("error", err))
			return err
		}
	}
	if err := r.Create(ctx, resource); err != nil {
		logger.Logger.Error("Error creating resource.", slog.Any("error", err))
		return err
	}

	controllerutil.AddFinalizer(resource, accessFinalizer)
	logger.Logger.Debug("Created resource.", slog.String("resource", resource.GetName()))
	return nil
}

func (r *BaseController) deleteResource(
	ctx context.Context,
	owner client.Object,
	resource client.Object,
	resourceType client.Object,
) error {
	existingResource := resourceType.DeepCopyObject().(client.Object)
	resourceKey := client.ObjectKey{Name: resource.GetName(), Namespace: resource.GetNamespace()}

	if err := r.Get(ctx, resourceKey, existingResource); err == nil {
		controllerutil.RemoveFinalizer(resource, accessFinalizer)
		if err := r.Delete(ctx, resource); err != nil {
			logger.Logger.Error(
				"Error deleting resource.",
				slog.Any("error", err),
				slog.String("kind", existingResource.GetObjectKind().GroupVersionKind().Kind),
				slog.String("name", existingResource.GetName()),
			)
			return nil
		}

		logger.Logger.Info(
			"Deleted resource.",
			slog.String("kind", existingResource.GetObjectKind().GroupVersionKind().Kind),
			slog.String("name", existingResource.GetName()),
		)
	}
	return nil
}

func (r *BaseController) reconcileResource(
	ctx context.Context,
	owner client.Object,
	resource, existingResource client.Object,
	specExtractor func(client.Object) any,
	disableOwnerRef bool,
) error {
	resourceKey := client.ObjectKey{Name: resource.GetName(), Namespace: resource.GetNamespace()}
	if err := r.Get(ctx, resourceKey, existingResource); err != nil {
		logger.Logger.Error("Error getting existing resource for comparison.", slog.Any("error", err))
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
		resource.SetResourceVersion(existingResource.GetResourceVersion())
		if !disableOwnerRef {
			if err := controllerutil.SetControllerReference(owner, resource, r.Scheme); err != nil {
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
			logger.Logger.Error(
				"Error updating resource.",
				slog.Any("error", err),
				slog.String("kind", existingResource.GetObjectKind().GroupVersionKind().Kind),
				slog.String("name", existingResource.GetName()),
			)
			return nil
		}
		logger.Logger.Debug(
			"The resource has been reconciled.",
			slog.String("kind", existingResource.GetObjectKind().GroupVersionKind().Kind),
			slog.String("name", existingResource.GetName()),
		)
	}

	return nil
}

func (r *BaseController) cleanupOrphanedNetworkPolicies(
	ctx context.Context,
	resource client.Object,
	config ReconcileConfig,
) error {
	logger.Logger.Debug("Starting cleanup of orphaned NetworkPolicies", "resource", resource.GetName())

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			config.OwnerLabel: resource.GetName(),
		},
	}
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return fmt.Errorf("failed to create selector for cleanup: %w", err)
	}

	ownedNetPols := &networkingv1.NetworkPolicyList{}
	if err := r.List(ctx, ownedNetPols, &client.ListOptions{LabelSelector: selector}); err != nil {
		return fmt.Errorf("failed to list owned NetworkPolicies for cleanup: %w", err)
	}

	for _, netpol := range ownedNetPols.Items {
		labels := netpol.GetLabels()
		serviceName, okName := labels[config.ServiceOwnerNameLabel]
		serviceNamespace, okNamespace := labels[config.ServiceOwnerNamespaceLabel]

		if !okName || !okNamespace {
			logger.Logger.Warn("Found owned NetworkPolicy with missing service owner labels, skipping cleanup check", "netpol", netpol.Name)
			continue
		}

		service := &corev1.Service{}
		err := r.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: serviceNamespace}, service)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				logger.Logger.Debug(
					"Deleting orphaned NetworkPolicy as its owner service is not found",
					"netpol", netpol.Name,
					"service", serviceName,
					"namespace", serviceNamespace,
				)
				if deleteErr := r.Delete(ctx, &netpol); deleteErr != nil {
					logger.Logger.Debug("Failed to delete orphaned NetworkPolicy", slog.Any("error", deleteErr), "netpol", netpol.Name)
				} else {
					logger.Logger.Debug("Deleted orphaned NetworkPolicy", "netpol", netpol.Name, "namespace", netpol.Namespace)
				}
			} else {
				logger.Logger.Error("Error checking for owner service existence during cleanup", slog.Any("error", err), "netpol", netpol.Name)
			}
		}
	}
	return nil
}

func (r *BaseController) getServiceDetails(
	ctx context.Context,
	serviceName string,
	namespace string,
) (map[string]string, []corev1.ServicePort, error) {
	serviceKey := types.NamespacedName{Name: serviceName, Namespace: namespace}
	service := &corev1.Service{}

	if err := r.Get(ctx, serviceKey, service); err != nil {
		if k8serrors.IsNotFound(err) {
			// Service not found - this is expected during cleanup, so use debug level
			logger.Logger.Debug("Service not found during processing", "service", serviceName, "namespace", namespace)
			return nil, nil, fmt.Errorf("service '%s' in namespace '%s' not found", serviceName, namespace)
		}
		return nil, nil, fmt.Errorf("failed to get service '%s': %w", serviceName, err)
	}

	if len(service.Spec.Selector) == 0 {
		return nil, nil, fmt.Errorf("service '%s' does not have a selector", serviceName)
	}

	if len(service.Spec.Ports) == 0 {
		return nil, nil, fmt.Errorf("service '%s' does not have any ports defined", serviceName)
	}

	return service.Spec.Selector, service.Spec.Ports, nil
}

func (r *BaseController) reconcileNetpolStatus(
	ctx context.Context,
	resource client.Object,
	deployedNetpols []vtkiov1alpha1.Netpol,
	config ReconcileConfig,
) ([]vtkiov1alpha1.Netpol, error) {
	statusResource := resource.(ResourceWithStatus)
	var existingNetpols []vtkiov1alpha1.Netpol
	for _, np := range statusResource.GetNetpols() {
		netpol := &networkingv1.NetworkPolicy{}
		err := r.Get(ctx, types.NamespacedName{Name: np.Name, Namespace: np.Namespace}, netpol)
		if err == nil {
			existingNetpols = append(existingNetpols, np)
		} else if !k8serrors.IsNotFound(err) {
			logger.Logger.Error("Error fetching existing netpol", slog.Any("error", err),
				"name", np.Name, "namespace", np.Namespace)
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

	return finalNetpols, nil
}

func (r *BaseController) reconcileServiceStatus(
	ctx context.Context,
	resource client.Object,
	matchedServices []vtkiov1alpha1.SvcRef,
	config ReconcileConfig,
) ([]vtkiov1alpha1.SvcRef, error) {
	statusResource := resource.(ResourceWithStatus)
	var existingServices []vtkiov1alpha1.SvcRef
	for _, s := range statusResource.GetServices() {
		svc := &corev1.Service{}
		err := r.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: s.Namespace}, svc)
		if err == nil {
			existingServices = append(existingServices, s)
		} else if !k8serrors.IsNotFound(err) {
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

	return finalServices, nil
}

func (r *BaseController) isExpired(resource ResourceWithExpiration) bool {
	// If duration is set but no expiration timestamp, calculate it
	if resource.GetDuration() != "" && resource.GetExpirationTimestamp() == nil {
		duration, err := utils.ParseDuration(resource.GetDuration())
		if err != nil {
			logger.Logger.Error("Invalid duration format.", "duration", resource.GetDuration(), "error", err)
			return false
		}

		expirationTime := resource.GetCreationTimestamp().Add(duration)
		newTime := metav1.NewTime(expirationTime)
		resource.SetExpirationTimestamp(&newTime)

		logger.Logger.Info("Expiration timestamp set.", "name", resource.GetName(), "expiresAt", resource.GetExpirationTimestamp().Time)
		// Don't check for expiration in the same call where we set the timestamp
		// Let the next reconciliation check if it's expired
		return false
	}

	// Check if expired
	if resource.GetExpirationTimestamp() != nil && time.Now().After(resource.GetExpirationTimestamp().Time) {
		return true
	}

	return false
}

func (r *BaseController) updateStatus(ctx context.Context, resource client.Object) error {
	// Use retry logic for status updates to handle conflicts
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Fetch the latest version of the resource before updating
		key := client.ObjectKeyFromObject(resource)
		latest := resource.DeepCopyObject().(client.Object)
		if err := r.Get(ctx, key, latest); err != nil {
			return err
		}

		// Copy the status from our resource to the latest version
		if statusResource, ok := resource.(ResourceWithStatus); ok {
			if latestStatusResource, ok := latest.(ResourceWithStatus); ok {
				latestStatusResource.SetNetpols(statusResource.GetNetpols())
				latestStatusResource.SetServices(statusResource.GetServices())
			}
		}

		// Copy expiration timestamp if it's set
		if expResource, ok := resource.(ResourceWithExpiration); ok {
			if latestExpResource, ok := latest.(ResourceWithExpiration); ok {
				if expResource.GetExpirationTimestamp() != nil {
					latestExpResource.SetExpirationTimestamp(expResource.GetExpirationTimestamp())
				}
			}
		}

		return r.Status().Update(ctx, latest)
	})
}

func (r *BaseController) getDirectionSuffix(direction string) string {
	switch strings.ToLower(direction) {
	case "ingress":
		return "ing"
	case "egress":
		return "egr"
	case "all":
		return "all"
	default:
		return "unknown"
	}
}

func (r *BaseController) netpolsEqual(a, b []vtkiov1alpha1.Netpol) bool {
	if len(a) != len(b) {
		return false
	}

	// Create maps for comparison
	aMap := make(map[string]vtkiov1alpha1.Netpol)
	bMap := make(map[string]vtkiov1alpha1.Netpol)

	for _, np := range a {
		key := fmt.Sprintf("%s/%s", np.Namespace, np.Name)
		aMap[key] = np
	}

	for _, np := range b {
		key := fmt.Sprintf("%s/%s", np.Namespace, np.Name)
		bMap[key] = np
	}

	return reflect.DeepEqual(aMap, bMap)
}

func (r *BaseController) servicesEqual(a, b []vtkiov1alpha1.SvcRef) bool {
	if len(a) != len(b) {
		return false
	}

	// Create maps for comparison
	aMap := make(map[string]vtkiov1alpha1.SvcRef)
	bMap := make(map[string]vtkiov1alpha1.SvcRef)

	for _, svc := range a {
		key := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
		aMap[key] = svc
	}

	for _, svc := range b {
		key := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
		bMap[key] = svc
	}

	return reflect.DeepEqual(aMap, bMap)
}
