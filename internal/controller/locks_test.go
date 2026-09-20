package controller

import (
	"context"
	"testing"
	"time"

	overridev1alpha1 "github.com/mitsu3s/override-lease/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestFieldLockPreventsOverlappingOverride(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	clock := start
	reconciler, kubeClient, firstRequest := newTestReconciler(t, &clock)
	reconcileTimes(t, reconciler, firstRequest, 2)

	second := &overridev1alpha1.OverrideLease{
		TypeMeta: metav1.TypeMeta{
			APIVersion: overridev1alpha1.GroupVersion.String(),
			Kind:       "OverrideLease",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "second-incident",
			Namespace:         "default",
			UID:               types.UID("second-lease-uid"),
			CreationTimestamp: metav1.NewTime(start),
		},
		Spec: overridev1alpha1.OverrideLeaseSpec{
			TargetRef: overridev1alpha1.TargetReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "api",
			},
			Overrides: []overridev1alpha1.FieldOverride{{
				Path:  "/spec/replicas",
				Value: apiextensionsv1.JSON{Raw: []byte("20")},
			}},
			Duration: "1h",
			Reason:   "second incident",
		},
	}
	if err := kubeClient.Create(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	secondRequest := ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "default", Name: "second-incident"},
	}
	reconcileTimes(t, reconciler, secondRequest, 2)

	var observed overridev1alpha1.OverrideLease
	if err := kubeClient.Get(context.Background(), secondRequest.NamespacedName, &observed); err != nil {
		t.Fatal(err)
	}
	if observed.Status.Phase != overridev1alpha1.PhasePending {
		t.Fatalf("phase=%q, want %q", observed.Status.Phase, overridev1alpha1.PhasePending)
	}
	if observed.Status.Message == "" {
		t.Fatal("lock contention did not produce a status message")
	}
	assertReplicas(t, kubeClient, 3)
}
