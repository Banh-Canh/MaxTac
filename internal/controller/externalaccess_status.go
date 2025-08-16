package controller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	vtkiov1alpha1 "github.com/Banh-Canh/maxtac/api/v1alpha1"
	"github.com/Banh-Canh/maxtac/internal/utils/logger"
)

func (r *ExternalAccessReconciler) updateStatus(ctx context.Context, access *vtkiov1alpha1.ExternalAccess) error {
	if r.StatusNeedUpdate {
		allConditionsReady := true
		// Check all conditions
		for _, condition := range access.Status.Conditions {
			if condition.Type == "Ready" {
				continue // Skip conditions of type "Ready"
			}
			if condition.Status != metav1.ConditionTrue {
				allConditionsReady = false
				break
			}
		}
		if allConditionsReady {
			if err := r.setCondition(
				access,
				"Ready",
				"AllConditionsReady",
				"All conditions are met.",
				metav1.ConditionTrue,
			); err != nil {
				logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
			}
		} else {
			if err := r.setCondition(
				access,
				"Ready",
				"AllConditionsNotReady",
				"Not all conditions are met.",
				metav1.ConditionFalse,
			); err != nil {
				logger.Logger.Error("Failed to set conditions.", slog.Any("error", err))
			}

			return fmt.Errorf("not all conditions are met yet, rescheduling")
		}
		// Update the ExternalAccess
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			return r.Client.Status().Update(ctx, access)
		})
		if err != nil {
			if k8serrors.IsConflict(err) {
				// r.Log.Info("Error updating access, access already updated.")
				return nil
			}
			// r.Log.Error(err, "Cannot Update ExternalAccess object.")
			return err
		}
		// r.Log.Info("Updated ExternalAccess Status.")
		r.StatusNeedUpdate = false
	}
	return nil
}

// SetCondition sets a condition on the status of the resource
func (r *ExternalAccessReconciler) setCondition(
	access *vtkiov1alpha1.ExternalAccess,
	conditionType, reason, message string,
	status metav1.ConditionStatus,
) error {
	newCondition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}

	// Check if the condition already exists, update it if yes
	for i, existingCondition := range access.Status.Conditions {
		if existingCondition.Type == conditionType {
			if existingCondition.Status != status {
				access.Status.Conditions[i] = newCondition
				r.StatusNeedUpdate = true
			}

			return nil
		}
	}
	// Condition doesn't exist, add it
	access.Status.Conditions = append(access.Status.Conditions, newCondition)
	r.removeDuplicateConditions(access)
	r.StatusNeedUpdate = true
	return nil
}

// RemoveDuplicateConditions removes duplicate conditions from the access status
func (r *ExternalAccessReconciler) removeDuplicateConditions(access *vtkiov1alpha1.ExternalAccess) {
	// Map to track unique condition types
	uniqueConditions := make(map[string]struct{})

	// Slice to hold unique conditions
	uniqueStatus := []metav1.Condition{}

	// Iterate through existing conditions
	for _, condition := range access.Status.Conditions {
		// If the condition type is not in the uniqueConditions map, add it to uniqueStatus
		if _, ok := uniqueConditions[condition.Type]; !ok {
			uniqueStatus = append(uniqueStatus, condition)
			uniqueConditions[condition.Type] = struct{}{}
		}
	}

	// Update the access status with unique conditions
	access.Status.Conditions = uniqueStatus
}
