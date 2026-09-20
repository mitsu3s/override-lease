// Package v1alpha1 contains the API types for temporary, reversible overrides.
// +kubebuilder:object:generate=true
// +groupName=mitsu3s.dev
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "mitsu3s.dev", Version: "v1alpha1"}
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme   = SchemeBuilder.AddToScheme
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &OverrideLease{}, &OverrideLeaseList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
