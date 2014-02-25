package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func Parse(schemaBytes io.Reader) (*Schema, error) {
	var schema *Schema
	if err := json.NewDecoder(schemaBytes).Decode(&schema); err != nil {
		return nil, err
	}
	return schema, nil
}

func (s *Schema) Validate(dataStruct interface{}) []ValidationError {
	data := normalizeType(dataStruct)
	var valErrs []ValidationError
	for _, validator := range s.Validators {
		valErrs = append(valErrs, validator(data)...)
	}
	return valErrs
}

func (s *Schema) UnmarshalJSON(bts []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(bts))
	decoder.UseNumber()
	var store interface{}
	if err := decoder.Decode(&store); err != nil {
		return err
	}
	schemaMap, ok := store.(map[string]interface{})
	if !ok {
		return errors.New("Schema must be of the type `map[string]interface{}`.")
	}
	if min, ok := schemaMap["minimum"]; ok {
		s.Validators = append(s.Validators, Minimum(min))
	}
	if prop, ok := schemaMap["properties"]; ok {
		s.Validators = append(s.Validators, Properties(prop))
	}
	return nil
}

type Schema struct {
	Validators []func(interface{}) []ValidationError
}

type ValidationError struct {
	Description string
}
