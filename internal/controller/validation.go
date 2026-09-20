package controller

import (
	"fmt"
	"sort"
	"strings"
	"time"

	overridev1alpha1 "github.com/mitsu3s/override-lease/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// DefaultMaxLeaseDuration is the safety limit used unless the manager is
// started with a different --max-lease-duration value.
const DefaultMaxLeaseDuration = 30 * 24 * time.Hour

type valueKind string

const (
	valueInteger      valueKind = "integer"
	valueBoolean      valueKind = "boolean"
	valueIntOrPercent valueKind = "integer-or-percentage"
)

var allowedTargets = map[string]map[string]valueKind{
	"apps/v1/Deployment": {
		"/spec/replicas": valueInteger,
	},
	"apps/v1/StatefulSet": {
		"/spec/replicas": valueInteger,
	},
	"autoscaling/v2/HorizontalPodAutoscaler": {
		"/spec/minReplicas": valueInteger,
		"/spec/maxReplicas": valueInteger,
	},
	"batch/v1/CronJob": {
		"/spec/suspend": valueBoolean,
	},
	"policy/v1/PodDisruptionBudget": {
		"/spec/minAvailable":   valueIntOrPercent,
		"/spec/maxUnavailable": valueIntOrPercent,
	},
}

func validateLease(lease *overridev1alpha1.OverrideLease, maxDuration time.Duration) error {
	if lease.Spec.TargetRef.APIVersion == "" || lease.Spec.TargetRef.Kind == "" || lease.Spec.TargetRef.Name == "" {
		return fmt.Errorf("targetRef apiVersion, kind, and name are required")
	}
	targetKey := lease.Spec.TargetRef.APIVersion + "/" + lease.Spec.TargetRef.Kind
	paths, supported := allowedTargets[targetKey]
	if !supported {
		return fmt.Errorf("target kind %s is not supported", targetKey)
	}
	if len(lease.Spec.Overrides) == 0 || len(lease.Spec.Overrides) > 8 {
		return fmt.Errorf("overrides must contain between 1 and 8 entries")
	}
	reason := strings.TrimSpace(lease.Spec.Reason)
	if len(reason) < 8 || len(reason) > 512 {
		return fmt.Errorf("reason must contain between 8 and 512 characters")
	}
	if len(lease.Spec.Ticket) > 256 {
		return fmt.Errorf("ticket must not exceed 256 characters")
	}
	duration, err := ParseLeaseDuration(lease.Spec.Duration)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}
	if duration > maxDuration {
		return fmt.Errorf("duration must not exceed %s", FormatLeaseDuration(maxDuration))
	}
	if policy := effectiveDriftPolicy(lease); policy != overridev1alpha1.DriftPolicyFailOnDrift && policy != overridev1alpha1.DriftPolicyIgnore {
		return fmt.Errorf("unsupported driftPolicy %q", policy)
	}
	seen := make(map[string]struct{}, len(lease.Spec.Overrides))
	for _, override := range lease.Spec.Overrides {
		kind, allowed := paths[override.Path]
		if !allowed {
			return fmt.Errorf("path %q is not supported for %s", override.Path, targetKey)
		}
		if _, duplicate := seen[override.Path]; duplicate {
			return fmt.Errorf("path %q appears more than once", override.Path)
		}
		seen[override.Path] = struct{}{}
		value, err := decodeScalar(override.Value)
		if err != nil {
			return fmt.Errorf("path %q: %w", override.Path, err)
		}
		if value == nil {
			return fmt.Errorf("path %q: null overrides are not supported", override.Path)
		}
		switch kind {
		case valueInteger:
			integer, ok := value.(int64)
			if !ok || integer < 0 {
				return fmt.Errorf("path %q requires a non-negative integer", override.Path)
			}
		case valueBoolean:
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("path %q requires a boolean", override.Path)
			}
		case valueIntOrPercent:
			switch typed := value.(type) {
			case int64:
				if typed < 0 {
					return fmt.Errorf("path %q requires a non-negative integer or percentage", override.Path)
				}
			case string:
				if !validPercentage(typed) {
					return fmt.Errorf("path %q requires a non-negative integer or percentage", override.Path)
				}
			default:
				return fmt.Errorf("path %q requires a non-negative integer or percentage", override.Path)
			}
		}
	}
	return nil
}

func validPercentage(value string) bool {
	if !strings.HasSuffix(value, "%") || len(value) < 2 {
		return false
	}
	var percentage int
	if _, err := fmt.Sscanf(value, "%d%%", &percentage); err != nil {
		return false
	}
	return percentage >= 0 && percentage <= 100 && value == fmt.Sprintf("%d%%", percentage)
}

func decodeStored(value apiextensionsv1.JSON) (any, error) {
	return decodeScalar(value)
}

func effectiveDriftPolicy(lease *overridev1alpha1.OverrideLease) overridev1alpha1.DriftPolicy {
	if lease.Spec.DriftPolicy == "" {
		return overridev1alpha1.DriftPolicyFailOnDrift
	}
	return lease.Spec.DriftPolicy
}

func leaseExpiry(lease *overridev1alpha1.OverrideLease) time.Time {
	duration, err := ParseLeaseDuration(lease.Spec.Duration)
	if err != nil {
		return lease.CreationTimestamp.Time
	}
	return lease.CreationTimestamp.Time.Add(duration)
}

func overridePaths(overrides []overridev1alpha1.FieldOverride) []string {
	paths := make([]string, 0, len(overrides))
	for _, override := range overrides {
		paths = append(paths, override.Path)
	}
	sort.Strings(paths)
	return paths
}

func allPaths(fields []overridev1alpha1.FieldState) []string {
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		paths = append(paths, field.Path)
	}
	sort.Strings(paths)
	return paths
}
