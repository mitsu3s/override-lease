package controller

import (
	"context"
	"time"

	overridev1alpha1 "github.com/mitsu3s/override-lease/api/v1alpha1"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	recorder "sigs.k8s.io/controller-runtime/pkg/recorder"
)

const overrideFinalizer = "mitsu3s.dev/rollback"

// OverrideLeaseReconciler applies a bounded override and later restores only
// fields that still contain the value it wrote.
type OverrideLeaseReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Recorder         recorder.EventRecorder
	Now              func() time.Time
	MaxLeaseDuration time.Duration
}

// +kubebuilder:rbac:groups=mitsu3s.dev,resources=overrideleases,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=mitsu3s.dev,resources=overrideleases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mitsu3s.dev,resources=overrideleases/finalizers,verbs=update
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets,verbs=get;patch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;patch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;patch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

func (r *OverrideLeaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var lease overridev1alpha1.OverrideLease
	if err := r.Get(ctx, req.NamespacedName, &lease); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !lease.DeletionTimestamp.IsZero() {
		return r.reconcileDeletion(ctx, &lease)
	}

	if err := validateLease(&lease, r.maxLeaseDuration()); err != nil {
		if controllerutil.ContainsFinalizer(&lease, overrideFinalizer) && len(lease.Status.Fields) > 0 {
			return r.rollback(ctx, &lease, "SpecBecameInvalid")
		}
		return r.setTerminalStatus(ctx, &lease, overridev1alpha1.PhaseInvalid, "InvalidSpec", err.Error(), nil)
	}

	if isTerminal(lease.Status.Phase) {
		return r.cleanupTerminal(ctx, &lease)
	}

	now := r.now()
	if lease.Status.Phase == "" && !leaseExpiry(&lease).After(now) {
		return r.setTerminalStatus(ctx, &lease, overridev1alpha1.PhaseCompleted, "ExpiredBeforeApply", "lease expired before it was applied", nil)
	}

	if !controllerutil.ContainsFinalizer(&lease, overrideFinalizer) {
		controllerutil.AddFinalizer(&lease, overrideFinalizer)
		if err := r.Update(ctx, &lease); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Millisecond}, nil
	}

	if !leaseExpiry(&lease).After(now) {
		return r.rollback(ctx, &lease, "Expired")
	}

	switch lease.Status.Phase {
	case "", overridev1alpha1.PhasePending:
		return r.prepare(ctx, &lease)
	case overridev1alpha1.PhasePrepared:
		return r.apply(ctx, &lease)
	case overridev1alpha1.PhaseApplied:
		return r.monitor(ctx, &lease)
	default:
		return r.setTerminalStatus(ctx, &lease, overridev1alpha1.PhaseInvalid, "UnknownPhase", "controller observed an unknown status phase", nil)
	}
}

func (r *OverrideLeaseReconciler) reconcileDeletion(ctx context.Context, lease *overridev1alpha1.OverrideLease) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(lease, overrideFinalizer) {
		return ctrl.Result{}, nil
	}
	if isTerminal(lease.Status.Phase) || len(lease.Status.Fields) == 0 {
		return r.cleanupTerminal(ctx, lease)
	}
	return r.rollback(ctx, lease, "LeaseDeleted")
}

func (r *OverrideLeaseReconciler) cleanupTerminal(ctx context.Context, lease *overridev1alpha1.OverrideLease) (ctrl.Result, error) {
	if err := r.releaseLocks(ctx, lease); err != nil {
		return ctrl.Result{}, err
	}
	if controllerutil.ContainsFinalizer(lease, overrideFinalizer) {
		controllerutil.RemoveFinalizer(lease, overrideFinalizer)
		if err := r.Update(ctx, lease); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *OverrideLeaseReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *OverrideLeaseReconciler) maxLeaseDuration() time.Duration {
	if r.MaxLeaseDuration > 0 {
		return r.MaxLeaseDuration
	}
	return DefaultMaxLeaseDuration
}

func (r *OverrideLeaseReconciler) nextCheck(lease *overridev1alpha1.OverrideLease) time.Duration {
	remaining := leaseExpiry(lease).Sub(r.now())
	if remaining <= 0 {
		return time.Millisecond
	}
	if remaining > time.Minute {
		return time.Minute
	}
	return remaining
}

func (r *OverrideLeaseReconciler) event(object runtime.Object, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Eventf(object, nil, eventType, reason, reason, "%s", message)
	}
}

func (r *OverrideLeaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&overridev1alpha1.OverrideLease{}).
		Owns(&coordinationv1.Lease{}).
		Complete(r)
}
