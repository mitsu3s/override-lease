package controller

import (
	"context"
	"testing"
	"time"

	overridev1alpha1 "github.com/mitsu3s/override-lease/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func TestDoesNotAdoptDesiredValueWrittenByAnotherActor(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)
	clock := start
	reconciler, kubeClient, request := newTestReconciler(t, &clock)

	// Add the finalizer and persist the Prepared snapshot, but do not apply yet.
	reconcileTimes(t, reconciler, request, 2)

	target := deploymentObject()
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: "default", Name: "api"}, target); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(target.Object, int64(10), "spec", "replicas"); err != nil {
		t.Fatal(err)
	}
	if err := kubeClient.Update(context.Background(), target); err != nil {
		t.Fatal(err)
	}

	reconcileTimes(t, reconciler, request, 1)
	assertReplicas(t, kubeClient, 10)

	var lease overridev1alpha1.OverrideLease
	if err := kubeClient.Get(context.Background(), request.NamespacedName, &lease); err != nil {
		t.Fatal(err)
	}
	if lease.Status.Phase != overridev1alpha1.PhaseConflict {
		t.Fatalf("phase=%q, want %q", lease.Status.Phase, overridev1alpha1.PhaseConflict)
	}
	if lease.Status.Message != "target reached the requested value before this controller could claim it" {
		t.Fatalf("unexpected status message: %q", lease.Status.Message)
	}
}

func TestOwnershipMarkerIsUniquePerLease(t *testing.T) {
	first := testLeaseWithUID("12345678-1111-2222-3333-444444444444")
	second := testLeaseWithUID("87654321-1111-2222-3333-444444444444")

	firstKey, firstValue := markerIdentity(first)
	secondKey, secondValue := markerIdentity(second)
	if firstKey == secondKey || firstValue == secondValue {
		t.Fatalf("marker identities collided: %q/%q and %q/%q", firstKey, firstValue, secondKey, secondValue)
	}
}

func testLeaseWithUID(uid string) *overridev1alpha1.OverrideLease {
	lease := &overridev1alpha1.OverrideLease{}
	lease.Name = "test"
	lease.Namespace = "default"
	lease.UID = types.UID(uid)
	return lease
}
