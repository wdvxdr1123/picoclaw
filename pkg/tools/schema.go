package tools

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type Schema struct {
	schema     *jsonschema.Schema
	resolved   *jsonschema.Resolved
	normalized map[string]any
}

func schema(raw any) *Schema {
	schema, err := NewSchema(raw)
	if err != nil {
		panic(err)
	}
	return schema
}

func NewSchema(raw any) (*Schema, error) {
	internal, resolved, normalized, err := prepareSchema(raw)
	if err != nil {
		return nil, err
	}
	return &Schema{
		schema:     internal,
		resolved:   resolved,
		normalized: normalized,
	}, nil
}

func schemaForParams[T any](mutate ...func(*jsonschema.Schema)) *Schema {
	schema, err := newSchemaForParams[T](mutate...)
	if err != nil {
		panic(err)
	}
	return schema
}

func schemaForType[T any]() (*Schema, error) {
	rt := reflect.TypeFor[T]()
	if rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if rt.Kind() != reflect.Struct && rt.Kind() != reflect.Map && rt != reflect.TypeFor[any]() {
		return nil, fmt.Errorf("tool parameters must infer from struct, map, or any, got %s", rt)
	}

	internal, err := jsonschema.ForType(rt, &jsonschema.ForOptions{})
	if err != nil {
		return nil, err
	}

	internal, resolved, normalized, err := prepareSchema(internal)
	if err != nil {
		return nil, err
	}

	return &Schema{
		schema:     internal,
		resolved:   resolved,
		normalized: normalized,
	}, nil
}

func newSchemaForParams[T any](mutate ...func(*jsonschema.Schema)) (*Schema, error) {
	schema, err := schemaForType[T]()
	if err != nil {
		return nil, err
	}
	if len(mutate) == 0 {
		return schema, nil
	}

	cloned := schema.JSONSchema().CloneSchemas()
	for _, fn := range mutate {
		if fn != nil {
			fn(cloned)
		}
	}
	return NewSchema(cloned)
}

func (s *Schema) JSONSchema() *jsonschema.Schema {
	if s == nil || s.schema == nil {
		return &jsonschema.Schema{Type: "object"}
	}
	return s.schema
}

func (s *Schema) Map() map[string]any {
	if s == nil || s.normalized == nil {
		return map[string]any{
			"type":       "object",
			"properties": map[string]any{},
			"required":   []any{},
		}
	}
	return s.normalized
}

func (s *Schema) Apply(args map[string]any) (map[string]any, error) {
	if s == nil || s.resolved == nil {
		if args == nil {
			return map[string]any{}, nil
		}
		return args, nil
	}

	payload := map[string]any{}
	if len(args) > 0 {
		data, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("marshal tool arguments: %w", err)
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, fmt.Errorf("unmarshal tool arguments: %w", err)
		}
	}

	if err := s.resolved.ApplyDefaults(&payload); err != nil {
		return nil, fmt.Errorf("applying schema defaults: %w", err)
	}
	if err := s.resolved.Validate(&payload); err != nil {
		return nil, err
	}

	return payload, nil
}

func prepareSchema(raw any) (*jsonschema.Schema, *jsonschema.Resolved, map[string]any, error) {
	if raw == nil {
		raw = &jsonschema.Schema{Type: "object"}
	}

	var internal *jsonschema.Schema
	switch provided := raw.(type) {
	case *jsonschema.Schema:
		internal = provided
	case json.RawMessage:
		if err := json.Unmarshal(provided, &internal); err != nil {
			return nil, nil, nil, err
		}
	case []byte:
		if err := json.Unmarshal(provided, &internal); err != nil {
			return nil, nil, nil, err
		}
	default:
		if err := remarshal(raw, &internal); err != nil {
			return nil, nil, nil, err
		}
	}

	if internal == nil {
		internal = &jsonschema.Schema{Type: "object"}
	}

	if err := validateToolSchemaObject(internal); err != nil {
		return nil, nil, nil, err
	}

	resolved, err := internal.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		return nil, nil, nil, err
	}

	normalized, err := schemaToMap(internal)
	if err != nil {
		return nil, nil, nil, err
	}

	return internal, resolved, normalized, nil
}

func validateToolSchemaObject(schema *jsonschema.Schema) error {
	switch {
	case schema.Type != "" && schema.Type != "object":
		return fmt.Errorf(`tool parameter schema must have type "object"`)
	case len(schema.Types) > 0:
		if len(schema.Types) != 1 || schema.Types[0] != "object" {
			return fmt.Errorf(`tool parameter schema must have type "object"`)
		}
	case schema.Type == "" && len(schema.Types) == 0:
		schema.Type = "object"
	}
	return nil
}

func schemaToMap(schema *jsonschema.Schema) (map[string]any, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}

	var normalized map[string]any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}

	if normalized["properties"] == nil {
		normalized["properties"] = map[string]any{}
	}
	if normalized["required"] == nil {
		normalized["required"] = []any{}
	}

	return normalized, nil
}

func remarshal(src any, dst any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
