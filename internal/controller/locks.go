package controller

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	overridev1alpha1 "github.com/mitsu3s/override-lease/api/v1alpha1"
	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const lockPrefix = "override-lock-"

func (r *OverrideLeaseReconciler) acquireLocks(ctx context.Context, lease *overridev1alpha1.OverrideLease) (string, error) {
	paths := overridePaths(lease.Spec.Overrides)
	now := metav1.NewMicroTime(r.now())
	duration := lockDurationSeconds(r.now(), leaseExpiry(lease))
	holder := string(lease.UID)

	for _, path := range paths {
		lock := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      lockName(lease, path),
				Namespace: lease.Namespace,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       &holder,
				LeaseDurationSeconds: &duration,
				AcquireTime:          &now,
				RenewTime:            &now,
			},
		}
		if err := controllerutil.SetControllerReference(lease, lock, r.Scheme); err != nil {
			return "", err
		}
		if err := r.Create(ctx, lock); err == nil {
			continue
		} else if !apierrors.IsAlreadyExists(err) {
			return "", err
		}

		var existing coordinationv1.Lease
		if err := r.Get(ctx, client.ObjectKeyFromObject(lock), &existing); err != nil {
			return "", err
		}
		if existing.Spec.HolderIdentity == nil || *existing.Spec.HolderIdentity != holder {
			_ = r.releaseLocks(ctx, lease)
			if existing.Spec.HolderIdentity == nil {
				return "unknown", nil
			}
			return *existing.Spec.HolderIdentity, nil
		}
		existing.Spec.LeaseDurationSeconds = &duration
		existing.Spec.RenewTime = &now
		if err := r.Update(ctx, &existing); err != nil && !apierrors.IsConflict(err) {
			return "", err
		}
	}
	return "", nil
}

func (r *OverrideLeaseReconciler) releaseLocks(ctx context.Context, lease *overridev1alpha1.OverrideLease) error {
	var errs []error
	holder := string(lease.UID)
	for _, path := range overridePaths(lease.Spec.Overrides) {
		var lock coordinationv1.Lease
		key := types.NamespacedName{Namespace: lease.Namespace, Name: lockName(lease, path)}
		if err := r.Get(ctx, key, &lock); err != nil {
			if !apierrors.IsNotFound(err) {
				errs = append(errs, err)
			}
			continue
		}
		if lock.Spec.HolderIdentity == nil || *lock.Spec.HolderIdentity != holder {
			continue
		}
		if err := r.Delete(ctx, &lock); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func lockName(lease *overridev1alpha1.OverrideLease, path string) string {
	key := strings.Join([]string{
		lease.Spec.TargetRef.APIVersion,
		lease.Spec.TargetRef.Kind,
		lease.Namespace,
		lease.Spec.TargetRef.Name,
		path,
	}, "|")
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s%x", lockPrefix, sum[:10])
}

func lockDurationSeconds(now, expiresAt time.Time) int32 {
	seconds := int64(math.Ceil(expiresAt.Sub(now).Seconds())) + 300
	if seconds < 1 {
		seconds = 1
	}
	if seconds > math.MaxInt32 {
		seconds = math.MaxInt32
	}
	return int32(seconds)
}
