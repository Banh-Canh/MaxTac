package controller

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	vtkiov1alpha1 "github.com/Banh-Canh/maxtac/api/v1alpha1"
	"github.com/Banh-Canh/maxtac/internal/utils/logger"
)

func (r *ClusterAccessReconciler) addFinalizer(ctx context.Context, access *vtkiov1alpha1.ClusterAccess) error {
	if access.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !controllerutil.ContainsFinalizer(access, accessFinalizer) {
			controllerutil.AddFinalizer(access, accessFinalizer)
			if err := r.Update(ctx, access); err != nil {
				logger.Logger.Error("Failed to add finalizer, retrying...", slog.Any("error", err))
				return err
			}
		}
	}
	return nil
}

func (r *ClusterAccessReconciler) removeFinalizer(ctx context.Context, access *vtkiov1alpha1.ClusterAccess) error {
	if !access.DeletionTimestamp.IsZero() {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(access, accessFinalizer) {
			// our finalizer is present, so lets handle any external dependency
			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(access, accessFinalizer)
			if err := r.Update(ctx, access); err != nil {
				logger.Logger.Error("Failed to remove finalizer, retrying...", slog.Any("error", err))
				return err
			}
		}
		return nil
	}
	return nil
}
