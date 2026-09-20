package controller

import (
	"testing"
	"time"

	overridev1alpha1 "github.com/mitsu3s/override-lease/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateLeaseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		duration  string
		maximum   time.Duration
		wantError bool
	}{
		{name: "positive", duration: "1m", maximum: DefaultMaxLeaseDuration},
		{name: "one week", duration: "7d", maximum: DefaultMaxLeaseDuration},
		{name: "configured maximum", duration: "90d", maximum: 90 * 24 * time.Hour},
		{name: "zero", duration: "0s", maximum: DefaultMaxLeaseDuration, wantError: true},
		{name: "invalid", duration: "tomorrow", maximum: DefaultMaxLeaseDuration, wantError: true},
		{name: "over maximum", duration: "30d1s", maximum: DefaultMaxLeaseDuration, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			lease := validLeaseWithDuration(test.duration)
			err := validateLease(lease, test.maximum)
			if test.wantError && err == nil {
				t.Fatal("validateLease() error=nil, want an error")
			}
			if !test.wantError && err != nil {
				t.Fatalf("validateLease() error=%v, want nil", err)
			}
		})
	}
}

func validLeaseWithDuration(duration string) *overridev1alpha1.OverrideLease {
	return &overridev1alpha1.OverrideLease{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "duration-test",
			Namespace:         "default",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)),
		},
		Spec: overridev1alpha1.OverrideLeaseSpec{
			TargetRef: overridev1alpha1.TargetReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "api",
			},
			Overrides: []overridev1alpha1.FieldOverride{{
				Path:  "/spec/replicas",
				Value: apiextensionsv1.JSON{Raw: []byte("3")},
			}},
			Duration: duration,
			Reason:   "duration validation test",
		},
	}
}
