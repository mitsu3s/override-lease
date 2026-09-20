package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func pointerSegments(path string) ([]string, error) {
	if path == "" || path[0] != '/' {
		return nil, fmt.Errorf("path %q must be an RFC 6901 JSON pointer", path)
	}
	raw := strings.Split(path[1:], "/")
	segments := make([]string, len(raw))
	for i, segment := range raw {
		var b strings.Builder
		for j := 0; j < len(segment); j++ {
			if segment[j] != '~' {
				b.WriteByte(segment[j])
				continue
			}
			if j+1 >= len(segment) {
				return nil, fmt.Errorf("path %q contains an invalid escape", path)
			}
			j++
			switch segment[j] {
			case '0':
				b.WriteByte('~')
			case '1':
				b.WriteByte('/')
			default:
				return nil, fmt.Errorf("path %q contains an invalid escape", path)
			}
		}
		segments[i] = b.String()
		if segments[i] == "" {
			return nil, fmt.Errorf("path %q contains an empty segment", path)
		}
	}
	return segments, nil
}

func getPointer(object map[string]any, path string) (any, bool, error) {
	segments, err := pointerSegments(path)
	if err != nil {
		return nil, false, err
	}
	current := object
	for i, segment := range segments {
		value, found := current[segment]
		if !found {
			return nil, false, nil
		}
		if i == len(segments)-1 {
			return value, true, nil
		}
		next, ok := value.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("path %q traverses a non-object at %q", path, segment)
		}
		current = next
	}
	return nil, false, nil
}

func setPointer(object map[string]any, path string, value any) error {
	segments, err := pointerSegments(path)
	if err != nil {
		return err
	}
	current := object
	for i, segment := range segments {
		if i == len(segments)-1 {
			current[segment] = value
			return nil
		}
		nextValue, found := current[segment]
		if !found {
			next := map[string]any{}
			current[segment] = next
			current = next
			continue
		}
		next, ok := nextValue.(map[string]any)
		if !ok {
			return fmt.Errorf("path %q traverses a non-object at %q", path, segment)
		}
		current = next
	}
	return nil
}

func removePointer(object map[string]any, path string) error {
	segments, err := pointerSegments(path)
	if err != nil {
		return err
	}
	current := object
	for i, segment := range segments {
		if i == len(segments)-1 {
			delete(current, segment)
			return nil
		}
		nextValue, found := current[segment]
		if !found {
			return nil
		}
		next, ok := nextValue.(map[string]any)
		if !ok {
			return fmt.Errorf("path %q traverses a non-object at %q", path, segment)
		}
		current = next
	}
	return nil
}

func decodeScalar(value apiextensionsv1.JSON) (any, error) {
	if len(value.Raw) == 0 {
		return nil, fmt.Errorf("value must not be empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(value.Raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode value: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, err
	}
	switch typed := decoded.(type) {
	case nil, bool, string:
		return typed, nil
	case json.Number:
		integer, err := strconv.ParseInt(string(typed), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("only integer numeric values are supported")
		}
		return integer, nil
	default:
		return nil, fmt.Errorf("only null, boolean, string, and integer values are supported")
	}
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing value: %w", err)
	}
	return fmt.Errorf("value must contain exactly one JSON value")
}

func encodeJSON(value any) (apiextensionsv1.JSON, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return apiextensionsv1.JSON{}, fmt.Errorf("encode value: %w", err)
	}
	return apiextensionsv1.JSON{Raw: raw}, nil
}

func valuesEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
