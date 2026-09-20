package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	overridev1alpha1 "github.com/mitsu3s/override-lease/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *OverrideLeaseReconciler) prepare(ctx context.Context, lease *overridev1alpha1.OverrideLease) (ctrl.Result, error) {
	target, err := r.getTarget(ctx, lease)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.setPendingStatus(ctx, lease, "TargetNotFound", err.Error(), 15*time.Second)
		}
		return ctrl.Result{}, err
	}

	blockedBy, err := r.acquireLocks(ctx, lease)
	if err != nil {
		return ctrl.Result{}, err
	}
	if blockedBy != "" {
		message := fmt.Sprintf("field lock is held by OverrideLease UID %s", blockedBy)
		setCondition(lease, "Locked", metav1.ConditionFalse, "LockContended", message)
		return r.setPendingStatus(ctx, lease, "LockContended", message, 10*time.Second)
	}

	fields := make([]overridev1alpha1.FieldState, 0, len(lease.Spec.Overrides))
	for _, override := range lease.Spec.Overrides {
		original, present, err := getPointer(target.Object, override.Path)
		if err != nil {
			_ = r.releaseLocks(ctx, lease)
			return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseInvalid, "UnreadableTargetField", err.Error(), nil)
		}
		originalJSON, err := encodeJSON(original)
		if err != nil {
			_ = r.releaseLocks(ctx, lease)
			return ctrl.Result{}, err
		}
		fields = append(fields, overridev1alpha1.FieldState{
			Path:            override.Path,
			OriginalPresent: present,
			Original:        originalJSON,
			Applied:         *override.Value.DeepCopy(),
		})
	}

	setObservedExpiry(lease)
	lease.Status.Phase = overridev1alpha1.PhasePrepared
	lease.Status.Message = "original field values are durable; override is ready to apply"
	lease.Status.TargetUID = target.GetUID()
	lease.Status.Fields = fields
	lease.Status.ConflictingPaths = nil
	setCondition(lease, "Locked", metav1.ConditionTrue, "LocksAcquired", "all field locks are held")
	setCondition(lease, "Applied", metav1.ConditionFalse, "Prepared", "override has not been applied yet")
	if err := r.Status().Update(ctx, lease); err != nil {
		return ctrl.Result{}, err
	}
	r.event(lease, corev1.EventTypeNormal, "Prepared", "Captured original values and acquired field locks")
	return ctrl.Result{RequeueAfter: time.Millisecond}, nil
}

func (r *OverrideLeaseReconciler) apply(ctx context.Context, lease *overridev1alpha1.OverrideLease) (ctrl.Result, error) {
	target, err := r.getTarget(ctx, lease)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "TargetDisappeared", "target disappeared before the override was applied", nil)
		}
		return ctrl.Result{}, err
	}
	if target.GetUID() != lease.Status.TargetUID {
		return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "TargetRecreated", "target UID changed after the original values were captured", nil)
	}
	ownedMarker, markerExists := markerState(target, lease)
	if markerExists && !ownedMarker {
		return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "OwnershipMarkerCollision", "target contains a conflicting ownership marker", allPaths(lease.Status.Fields))
	}

	before := target.DeepCopy()
	for _, state := range lease.Status.Fields {
		current, present, err := getPointer(target.Object, state.Path)
		if err != nil {
			return ctrl.Result{}, err
		}
		original, err := decodeStored(state.Original)
		if err != nil {
			return ctrl.Result{}, err
		}
		applied, err := decodeStored(state.Applied)
		if err != nil {
			return ctrl.Result{}, err
		}
		if fieldMatches(present, current, state.OriginalPresent, original) {
			if err := setPointer(target.Object, state.Path, applied); err != nil {
				return ctrl.Result{}, err
			}
			continue
		}
		if fieldMatches(present, current, true, applied) {
			if !ownedMarker {
				return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "UnownedDesiredValue", "target reached the requested value before this controller could claim it", []string{state.Path})
			}
			continue
		}
		if !ownedMarker {
			return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "DriftBeforeApply", "target changed before the override could be applied", []string{state.Path})
		}
		return r.rollback(ctx, lease, "DriftBeforeApply")
	}

	setOwnershipMarker(target, lease)
	if err := r.Patch(ctx, target, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	now := metav1.NewTime(r.now())
	setObservedExpiry(lease)
	lease.Status.Phase = overridev1alpha1.PhaseApplied
	lease.Status.Message = "override is active"
	lease.Status.AppliedAt = &now
	setCondition(lease, "Applied", metav1.ConditionTrue, "OverrideApplied", "all requested fields were applied")
	if err := r.Status().Update(ctx, lease); err != nil {
		return ctrl.Result{}, err
	}
	r.event(lease, corev1.EventTypeNormal, "OverrideApplied", "Applied temporary override")
	return ctrl.Result{RequeueAfter: r.nextCheck(lease)}, nil
}

func (r *OverrideLeaseReconciler) monitor(ctx context.Context, lease *overridev1alpha1.OverrideLease) (ctrl.Result, error) {
	target, err := r.getTarget(ctx, lease)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "TargetDisappeared", "target disappeared while the override was active", nil)
		}
		return ctrl.Result{}, err
	}
	if target.GetUID() != lease.Status.TargetUID {
		return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "TargetRecreated", "target UID changed while the override was active", nil)
	}
	if owned, _ := markerState(target, lease); !owned {
		return r.rollback(ctx, lease, "OwnershipMarkerLost")
	}

	resolvedExpiry := metav1.NewTime(leaseExpiry(lease))
	if lease.Status.ObservedGeneration != lease.Generation ||
		lease.Status.ExpiresAt == nil || !lease.Status.ExpiresAt.Equal(&resolvedExpiry) {
		setObservedExpiry(lease)
		if err := r.Status().Update(ctx, lease); err != nil {
			return ctrl.Result{}, err
		}
	}

	if effectiveDriftPolicy(lease) == overridev1alpha1.DriftPolicyFailOnDrift {
		for _, state := range lease.Status.Fields {
			current, present, err := getPointer(target.Object, state.Path)
			if err != nil {
				return ctrl.Result{}, err
			}
			applied, err := decodeStored(state.Applied)
			if err != nil {
				return ctrl.Result{}, err
			}
			if !fieldMatches(present, current, true, applied) {
				return r.rollback(ctx, lease, "DriftDetected")
			}
		}
	}
	return ctrl.Result{RequeueAfter: r.nextCheck(lease)}, nil
}

func (r *OverrideLeaseReconciler) rollback(ctx context.Context, lease *overridev1alpha1.OverrideLease, reason string) (ctrl.Result, error) {
	if len(lease.Status.Fields) == 0 {
		return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseCompleted, reason, "no target fields were changed", nil)
	}

	target, err := r.getTarget(ctx, lease)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "TargetMissing", "target no longer exists; rollback cannot be verified", allPaths(lease.Status.Fields))
		}
		return ctrl.Result{}, err
	}
	if target.GetUID() != lease.Status.TargetUID {
		return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "TargetRecreated", "refusing to modify a replacement object with a different UID", allPaths(lease.Status.Fields))
	}
	if owned, _ := markerState(target, lease); !owned {
		conflicts := make([]string, 0)
		for _, state := range lease.Status.Fields {
			current, present, err := getPointer(target.Object, state.Path)
			if err != nil {
				return ctrl.Result{}, err
			}
			original, err := decodeStored(state.Original)
			if err != nil {
				return ctrl.Result{}, err
			}
			if !fieldMatches(present, current, state.OriginalPresent, original) {
				conflicts = append(conflicts, state.Path)
			}
		}
		if len(conflicts) > 0 {
			message := fmt.Sprintf("ownership marker is absent; refusing to restore fields that may belong to another actor: %s", strings.Join(conflicts, ", "))
			return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "OwnershipMarkerLost", message, conflicts)
		}
		now := metav1.NewTime(r.now())
		lease.Status.RolledBackAt = &now
		return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseCompleted, reason, "target already contains the original values", nil)
	}

	before := target.DeepCopy()
	conflicts := make([]string, 0)
	for _, state := range lease.Status.Fields {
		current, present, err := getPointer(target.Object, state.Path)
		if err != nil {
			return ctrl.Result{}, err
		}
		original, err := decodeStored(state.Original)
		if err != nil {
			return ctrl.Result{}, err
		}
		applied, err := decodeStored(state.Applied)
		if err != nil {
			return ctrl.Result{}, err
		}

		switch {
		case fieldMatches(present, current, state.OriginalPresent, original):
			continue
		case fieldMatches(present, current, true, applied):
			if state.OriginalPresent {
				if err := setPointer(target.Object, state.Path, original); err != nil {
					return ctrl.Result{}, err
				}
			} else if err := removePointer(target.Object, state.Path); err != nil {
				return ctrl.Result{}, err
			}
		default:
			conflicts = append(conflicts, state.Path)
		}
	}
	removeOwnershipMarker(target, lease)

	if err := r.Patch(ctx, target, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	if len(conflicts) > 0 {
		message := fmt.Sprintf("restored safe fields; refused to overwrite externally changed fields: %s", strings.Join(conflicts, ", "))
		return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseConflict, "CompareAndSwapConflict", message, conflicts)
	}

	now := metav1.NewTime(r.now())
	lease.Status.RolledBackAt = &now
	return r.setTerminalStatus(ctx, lease, overridev1alpha1.PhaseCompleted, reason, "original field values were restored", nil)
}

func (r *OverrideLeaseReconciler) setPendingStatus(
	ctx context.Context,
	lease *overridev1alpha1.OverrideLease,
	reason, message string,
	requeueAfter time.Duration,
) (ctrl.Result, error) {
	setObservedExpiry(lease)
	lease.Status.Phase = overridev1alpha1.PhasePending
	lease.Status.Message = message
	setCondition(lease, "Ready", metav1.ConditionFalse, reason, message)
	if err := r.Status().Update(ctx, lease); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

func (r *OverrideLeaseReconciler) setTerminalStatus(
	ctx context.Context,
	lease *overridev1alpha1.OverrideLease,
	phase overridev1alpha1.OverrideLeasePhase,
	reason, message string,
	conflicts []string,
) (ctrl.Result, error) {
	setObservedExpiry(lease)
	lease.Status.Phase = phase
	lease.Status.Message = message
	lease.Status.ConflictingPaths = conflicts
	if phase == overridev1alpha1.PhaseCompleted {
		now := metav1.NewTime(r.now())
		if lease.Status.RolledBackAt == nil && len(lease.Status.Fields) > 0 {
			lease.Status.RolledBackAt = &now
		}
		setCondition(lease, "RolledBack", metav1.ConditionTrue, reason, message)
	} else {
		setCondition(lease, "RolledBack", metav1.ConditionFalse, reason, message)
	}
	if err := r.Status().Update(ctx, lease); err != nil {
		return ctrl.Result{}, err
	}
	eventType := corev1.EventTypeNormal
	if phase == overridev1alpha1.PhaseConflict || phase == overridev1alpha1.PhaseInvalid {
		eventType = corev1.EventTypeWarning
	}
	r.event(lease, eventType, reason, message)
	return ctrl.Result{RequeueAfter: time.Millisecond}, nil
}

func (r *OverrideLeaseReconciler) getTarget(ctx context.Context, lease *overridev1alpha1.OverrideLease) (*unstructured.Unstructured, error) {
	groupVersion, err := schema.ParseGroupVersion(lease.Spec.TargetRef.APIVersion)
	if err != nil {
		return nil, fmt.Errorf("parse target apiVersion: %w", err)
	}
	target := &unstructured.Unstructured{}
	target.SetGroupVersionKind(groupVersion.WithKind(lease.Spec.TargetRef.Kind))
	key := types.NamespacedName{Namespace: lease.Namespace, Name: lease.Spec.TargetRef.Name}
	if err := r.Get(ctx, key, target); err != nil {
		return nil, err
	}
	return target, nil
}

func fieldMatches(actualPresent bool, actual any, expectedPresent bool, expected any) bool {
	if actualPresent != expectedPresent {
		return false
	}
	if !expectedPresent {
		return true
	}
	return valuesEqual(actual, expected)
}

func setCondition(lease *overridev1alpha1.OverrideLease, conditionType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&lease.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: lease.Generation,
	})
}

func setObservedExpiry(lease *overridev1alpha1.OverrideLease) {
	lease.Status.ObservedGeneration = lease.Generation
	if lease.CreationTimestamp.IsZero() {
		lease.Status.ExpiresAt = nil
		return
	}
	if _, err := ParseLeaseDuration(lease.Spec.Duration); err != nil {
		lease.Status.ExpiresAt = nil
		return
	}
	expiresAt := metav1.NewTime(leaseExpiry(lease))
	lease.Status.ExpiresAt = &expiresAt
}

func isTerminal(phase overridev1alpha1.OverrideLeasePhase) bool {
	return phase == overridev1alpha1.PhaseCompleted ||
		phase == overridev1alpha1.PhaseConflict ||
		phase == overridev1alpha1.PhaseInvalid
}
