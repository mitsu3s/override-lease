package controller

import (
	"fmt"
	"strings"

	overridev1alpha1 "github.com/mitsu3s/override-lease/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const markerPrefix = "mitsu3s.dev/override-"

func markerIdentity(lease *overridev1alpha1.OverrideLease) (string, string) {
	token := strings.ReplaceAll(string(lease.UID), "-", "")
	if token == "" {
		token = "unknown"
	}
	key := markerPrefix + token
	value := fmt.Sprintf("%s/%s@%s", lease.Namespace, lease.Name, lease.UID)
	return key, value
}

func markerState(target *unstructured.Unstructured, lease *overridev1alpha1.OverrideLease) (owned, exists bool) {
	key, expected := markerIdentity(lease)
	actual, exists := target.GetAnnotations()[key]
	return exists && actual == expected, exists
}

func setOwnershipMarker(target *unstructured.Unstructured, lease *overridev1alpha1.OverrideLease) {
	key, value := markerIdentity(lease)
	annotations := target.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[key] = value
	target.SetAnnotations(annotations)
}

func removeOwnershipMarker(target *unstructured.Unstructured, lease *overridev1alpha1.OverrideLease) {
	key, _ := markerIdentity(lease)
	annotations := target.GetAnnotations()
	delete(annotations, key)
	target.SetAnnotations(annotations)
}
