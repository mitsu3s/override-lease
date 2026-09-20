package controller

import (
	"context"
	"testing"
	"time"

	overridev1alpha1 "github.com/mitsu3s/override-lease/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestLifecycleAppliesAndRollsBack(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	clock := start
	reconciler, kubeClient, request := newTestReconciler(t, &clock)

	reconcileTimes(t, reconciler, request, 3)
	assertReplicas(t, kubeClient, 10)

	var lease overridev1alpha1.OverrideLease
	if err := kubeClient.Get(context.Background(), request.NamespacedName, &lease); err != nil {
		t.Fatal(err)
	}
	if lease.Status.Phase != overridev1alpha1.PhaseApplied {
		t.Fatalf("phase=%q, want %q", lease.Status.Phase, overridev1alpha1.PhaseApplied)
	}
	wantExpiry := start.Add(30 * time.Minute)
	if lease.Status.ExpiresAt == nil || !lease.Status.ExpiresAt.Time.Equal(wantExpiry) {
		t.Fatalf("expiresAt=%v, want %s", lease.Status.ExpiresAt, wantExpiry)
	}
	if len(lease.Status.Fields) != 1 || string(lease.Status.Fields[0].Original.Raw) != "3" {
		t.Fatalf("original field was not stored before apply: %#v", lease.Status.Fields)
	}

	clock = start.Add(31 * time.Minute)
	reconcileTimes(t, reconciler, request, 1)
	assertReplicas(t, kubeClient, 3)

	if err := kubeClient.Get(context.Background(), request.NamespacedName, &lease); err != nil {
		t.Fatal(err)
	}
	if lease.Status.Phase != overridev1alpha1.PhaseCompleted {
		t.Fatalf("phase=%q, want %q", lease.Status.Phase, overridev1alpha1.PhaseCompleted)
	}

	reconcileTimes(t, reconciler, request, 1)
	if err := kubeClient.Get(context.Background(), request.NamespacedName, &lease); err != nil {
		t.Fatal(err)
	}
	if len(lease.Finalizers) != 0 {
		t.Fatalf("terminal lease still has finalizers: %v", lease.Finalizers)
	}

	var locks coordinationv1.LeaseList
	if err := kubeClient.List(context.Background(), &locks, client.InNamespace("default")); err != nil {
		t.Fatal(err)
	}
	if len(locks.Items) != 0 {
		t.Fatalf("terminal lease left %d field locks", len(locks.Items))
	}
}

func TestCompareAndSwapPreservesExternalChange(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	clock := start
	reconciler, kubeClient, request := newTestReconciler(t, &clock)
	reconcileTimes(t, reconciler, request, 3)

	target := deploymentObject()
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "api"}, target); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(target.Object, int64(7), "spec", "replicas"); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Update(context.Background(), target); err != nil {
		t.Fatal(err)
	}

	reconcileTimes(t, reconciler, request, 1)
	assertReplicas(t, kubeClient, 7)

	var lease overridev1alpha1.OverrideLease
	if err := kubeClient.Get(context.Background(), request.NamespacedName, &lease); err != nil {
		t.Fatal(err)
	}
	if lease.Status.Phase != overridev1alpha1.PhaseConflict {
		t.Fatalf("phase=%q, want %q", lease.Status.Phase, overridev1alpha1.PhaseConflict)
	}
	if len(lease.Status.ConflictingPaths) != 1 || lease.Status.ConflictingPaths[0] != "/spec/replicas" {
		t.Fatalf("unexpected conflicts: %v", lease.Status.ConflictingPaths)
	}
}

func newTestReconciler(
	t *testing.T,
	clock *time.Time,
) (*OverrideLeaseReconciler, client.Client, ctrl.Request) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := coordinationv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := overridev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	target := deploymentObject()
	target.SetUID(types.UID("target-uid"))
	lease := &overridev1alpha1.OverrideLease{
		TypeMeta: metav1.TypeMeta{
			APIVersion: overridev1alpha1.GroupVersion.String(),
			Kind:       "OverrideLease",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "incident-capacity",
			Namespace:         "default",
			UID:               types.UID("lease-uid"),
			CreationTimestamp: metav1.NewTime(*clock),
		},
		Spec: overridev1alpha1.OverrideLeaseSpec{
			TargetRef: overridev1alpha1.TargetReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "api",
			},
			Overrides: []overridev1alpha1.FieldOverride{{
				Path:  "/spec/replicas",
				Value: apiextensionsv1.JSON{Raw: []byte("10")},
			}},
			Duration: "30m",
			Reason:   "incident test",
		},
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(target, lease).
		WithStatusSubresource(&overridev1alpha1.OverrideLease{}).
		Build()
	reconciler := &OverrideLeaseReconciler{
		Client: kubeClient,
		Scheme: scheme,
		Now:    func() time.Time { return *clock },
	}
	return reconciler, kubeClient, ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "incident-capacity"},
	}
}

func deploymentObject() *unstructured.Unstructured {
	target := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      "api",
			"namespace": "default",
		},
		"spec": map[string]any{
			"replicas": int64(3),
			"selector": map[string]any{
				"matchLabels": map[string]any{"app": "api"},
			},
			"template": map[string]any{
				"metadata": map[string]any{
					"labels": map[string]any{"app": "api"},
				},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "api", "image": "example.invalid/api:test"},
					},
				},
			},
		},
	}}
	target.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind("Deployment"))
	return target
}

func reconcileTimes(t *testing.T, reconciler *OverrideLeaseReconciler, request ctrl.Request, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		if _, err := reconciler.Reconcile(context.Background(), request); err != nil {
			t.Fatalf("reconcile %d failed: %v", i+1, err)
		}
	}
}

func assertReplicas(t *testing.T, kubeClient client.Client, want int64) {
	t.Helper()
	target := deploymentObject()
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "api"}, target); err != nil {
		t.Fatal(err)
	}
	got, found, err := unstructured.NestedInt64(target.Object, "spec", "replicas")
	if err != nil || !found || got != want {
		t.Fatalf("replicas=%d found=%v err=%v, want %d", got, found, err, want)
	}
}
