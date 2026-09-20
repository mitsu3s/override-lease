package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// OverrideLeasePhase is the controller-observed lifecycle state.
// +kubebuilder:validation:Enum=Pending;Prepared;Applied;Completed;Conflict;Invalid
type OverrideLeasePhase string

const (
	PhasePending   OverrideLeasePhase = "Pending"
	PhasePrepared  OverrideLeasePhase = "Prepared"
	PhaseApplied   OverrideLeasePhase = "Applied"
	PhaseCompleted OverrideLeasePhase = "Completed"
	PhaseConflict  OverrideLeasePhase = "Conflict"
	PhaseInvalid   OverrideLeasePhase = "Invalid"
)

// DriftPolicy controls how an active lease reacts to another actor changing a
// field managed by the lease.
// +kubebuilder:validation:Enum=FailOnDrift;Ignore
type DriftPolicy string

const (
	DriftPolicyFailOnDrift DriftPolicy = "FailOnDrift"
	DriftPolicyIgnore      DriftPolicy = "Ignore"
)

// TargetReference identifies a supported resource in the OverrideLease namespace.
// +kubebuilder:validation:XValidation:rule="(self.apiVersion == 'apps/v1' && self.kind in ['Deployment', 'StatefulSet']) || (self.apiVersion == 'autoscaling/v2' && self.kind == 'HorizontalPodAutoscaler') || (self.apiVersion == 'batch/v1' && self.kind == 'CronJob') || (self.apiVersion == 'policy/v1' && self.kind == 'PodDisruptionBudget')",message="apiVersion and kind must identify a supported target"
type TargetReference struct {
	// +kubebuilder:validation:MinLength=1
	APIVersion string `json:"apiVersion"`

	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
}

type FieldOverride struct {
	// Path is an RFC 6901 JSON pointer to a supported scalar field.
	// +kubebuilder:validation:Pattern=`^/`
	Path string `json:"path"`

	// Value is the temporary value. Objects and arrays are intentionally rejected.
	// +kubebuilder:pruning:PreserveUnknownFields
	Value apiextensionsv1.JSON `json:"value"`
}

// OverrideLeaseSpec defines the requested temporary override.
// +kubebuilder:validation:XValidation:rule="(self.targetRef.apiVersion == 'apps/v1' && self.targetRef.kind in ['Deployment', 'StatefulSet'] && self.overrides.all(o, o.path == '/spec/replicas')) || (self.targetRef.apiVersion == 'autoscaling/v2' && self.targetRef.kind == 'HorizontalPodAutoscaler' && self.overrides.all(o, o.path in ['/spec/minReplicas', '/spec/maxReplicas'])) || (self.targetRef.apiVersion == 'batch/v1' && self.targetRef.kind == 'CronJob' && self.overrides.all(o, o.path == '/spec/suspend')) || (self.targetRef.apiVersion == 'policy/v1' && self.targetRef.kind == 'PodDisruptionBudget' && self.overrides.all(o, o.path in ['/spec/minAvailable', '/spec/maxUnavailable']))",message="override path is not supported for the selected target"
type OverrideLeaseSpec struct {
	// TargetRef is immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="targetRef is immutable"
	TargetRef TargetReference `json:"targetRef"`

	// Overrides is immutable after creation.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=8
	// +listType=map
	// +listMapKey=path
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="overrides are immutable"
	Overrides []FieldOverride `json:"overrides"`

	// Duration is how long the lease may remain active after creation.
	// It accepts compound millisecond, second, minute, hour, day, and week units,
	// for example 30m, 12h, 7d, or 1w2d.
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h|d|w))+$"
	// +kubebuilder:validation:XValidation:rule="self.matches('[1-9]')",message="duration must be greater than zero"
	Duration string `json:"duration"`

	// Reason is required so that every override has an auditable purpose.
	// +kubebuilder:validation:MinLength=8
	// +kubebuilder:validation:MaxLength=512
	Reason string `json:"reason"`

	// Ticket is an optional incident, change, or approval reference.
	// +optional
	// +kubebuilder:validation:MaxLength=256
	Ticket string `json:"ticket,omitempty"`

	// DriftPolicy controls what happens when another actor changes an overridden field.
	// FailOnDrift rolls back all fields that are still safe to restore. Ignore waits
	// until expiry, but rollback still uses compare-and-swap semantics.
	// +optional
	// +kubebuilder:default=FailOnDrift
	DriftPolicy DriftPolicy `json:"driftPolicy,omitempty"`
}

// FieldState records the value required to perform a compare-and-swap rollback.
type FieldState struct {
	Path string `json:"path"`

	OriginalPresent bool `json:"originalPresent"`
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	Original apiextensionsv1.JSON `json:"original,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	Applied apiextensionsv1.JSON `json:"applied"`
}

// OverrideLeaseStatus contains the controller-observed state.
type OverrideLeaseStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	Phase OverrideLeasePhase `json:"phase,omitempty"`

	// +optional
	Message string `json:"message,omitempty"`

	// +optional
	TargetUID types.UID `json:"targetUID,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=path
	Fields []FieldState `json:"fields,omitempty"`

	// +optional
	AppliedAt *metav1.Time `json:"appliedAt,omitempty"`

	// ExpiresAt is the absolute expiry calculated from creation time and spec.duration.
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// +optional
	RolledBackAt *metav1.Time `json:"rolledBackAt,omitempty"`

	// +optional
	// +listType=set
	ConflictingPaths []string `json:"conflictingPaths,omitempty"`

	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// OverrideLease requests a temporary, reversible field override.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=ol,categories=mitsu3s
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=".spec.targetRef.name"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// A string column is intentional: Kubernetes renders future values in date
// columns as <invalid> because date columns represent elapsed time.
// +kubebuilder:printcolumn:name="Expires",type=string,JSONPath=".status.expiresAt"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
type OverrideLease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	Spec OverrideLeaseSpec `json:"spec"`

	// +optional
	Status OverrideLeaseStatus `json:"status,omitempty"`
}

// OverrideLeaseList contains a list of OverrideLease resources.
// +kubebuilder:object:root=true
type OverrideLeaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OverrideLease `json:"items"`
}
