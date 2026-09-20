package controller

import (
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestPointerRoundTrip(t *testing.T) {
	object := map[string]any{
		"spec": map[string]any{
			"maxReplicas": int64(5),
		},
	}

	value, found, err := getPointer(object, "/spec/maxReplicas")
	if err != nil || !found || !valuesEqual(value, int64(5)) {
		t.Fatalf("unexpected get result: value=%v found=%v err=%v", value, found, err)
	}
	if err := setPointer(object, "/spec/maxReplicas", int64(12)); err != nil {
		t.Fatalf("setPointer failed: %v", err)
	}
	value, found, err = getPointer(object, "/spec/maxReplicas")
	if err != nil || !found || !valuesEqual(value, int64(12)) {
		t.Fatalf("unexpected updated value: value=%v found=%v err=%v", value, found, err)
	}
	if err := removePointer(object, "/spec/maxReplicas"); err != nil {
		t.Fatalf("removePointer failed: %v", err)
	}
	if _, found, err := getPointer(object, "/spec/maxReplicas"); err != nil || found {
		t.Fatalf("expected field to be absent: found=%v err=%v", found, err)
	}
}

func TestEscapedPointer(t *testing.T) {
	object := map[string]any{"data": map[string]any{"a/b~c": "ok"}}
	value, found, err := getPointer(object, "/data/a~1b~0c")
	if err != nil || !found || value != "ok" {
		t.Fatalf("escaped pointer failed: value=%v found=%v err=%v", value, found, err)
	}
}

func TestDecodeScalar(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    any
		wantErr bool
	}{
		{name: "integer", raw: "42", want: int64(42)},
		{name: "boolean", raw: "true", want: true},
		{name: "string", raw: `"debug"`, want: "debug"},
		{name: "object rejected", raw: `{"unsafe":true}`, wantErr: true},
		{name: "float rejected", raw: "1.5", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeScalar(apiextensionsv1.JSON{Raw: []byte(tt.raw)})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil || !valuesEqual(got, tt.want) {
				t.Fatalf("got=%v want=%v err=%v", got, tt.want, err)
			}
		})
	}
}
