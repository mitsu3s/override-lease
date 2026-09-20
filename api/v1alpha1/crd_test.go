package v1alpha1

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsvalidation "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/validation"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestGeneratedCRDIsValid(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test file")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "..", "config", "crd", "bases", "mitsu3s.dev_overrideleases.yaml")
	yamlData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated CRD: %v", err)
	}
	jsonData, err := utilyaml.ToJSON(yamlData)
	if err != nil {
		t.Fatalf("convert generated CRD to JSON: %v", err)
	}

	var versioned apiextensionsv1.CustomResourceDefinition
	if err := json.Unmarshal(jsonData, &versioned); err != nil {
		t.Fatalf("decode generated CRD: %v", err)
	}
	var internal apiextensions.CustomResourceDefinition
	if err := apiextensionsv1.Convert_v1_CustomResourceDefinition_To_apiextensions_CustomResourceDefinition(&versioned, &internal, nil); err != nil {
		t.Fatalf("convert generated CRD: %v", err)
	}
	// The API server populates storedVersions after creation. Supply that status
	// value so this test can exercise the schema and CEL validation locally.
	internal.Status.StoredVersions = []string{"v1alpha1"}
	if validationErrors := apiextensionsvalidation.ValidateCustomResourceDefinition(context.Background(), &internal); len(validationErrors) > 0 {
		t.Fatalf("generated CRD is invalid: %v", validationErrors.ToAggregate())
	}

	version := versioned.Spec.Versions[0]
	rootSchema := version.Schema.OpenAPIV3Schema
	specSchema, ok := rootSchema.Properties["spec"]
	if !ok {
		t.Fatal("generated CRD does not define spec")
	}
	durationSchema, ok := specSchema.Properties["duration"]
	if !ok {
		t.Fatal("generated CRD does not define spec.duration")
	}
	if durationSchema.Type != "string" || durationSchema.Pattern == "" || len(durationSchema.XValidations) == 0 {
		t.Fatalf("spec.duration schema=%#v, want a validated duration string", durationSchema)
	}
	if !slices.Contains(specSchema.Required, "duration") {
		t.Fatalf("spec required fields=%v, want duration", specSchema.Required)
	}
	if _, exists := specSchema.Properties["expiresAt"]; exists {
		t.Fatal("generated CRD still defines removed spec.expiresAt")
	}

	statusSchema, ok := rootSchema.Properties["status"]
	if !ok {
		t.Fatal("generated CRD does not define status")
	}
	expiresAtSchema, ok := statusSchema.Properties["expiresAt"]
	if !ok || expiresAtSchema.Type != "string" || expiresAtSchema.Format != "date-time" {
		t.Fatalf("status.expiresAt schema=%#v, want a date-time string", expiresAtSchema)
	}

	foundExpiresColumn := false
	for _, column := range version.AdditionalPrinterColumns {
		if column.Name == "Expires" && column.Type == "string" && column.JSONPath == ".status.expiresAt" {
			foundExpiresColumn = true
			break
		}
	}
	if !foundExpiresColumn {
		t.Fatalf("additional printer columns=%v, want string Expires from .status.expiresAt", version.AdditionalPrinterColumns)
	}
}
