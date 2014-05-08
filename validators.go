package jsonschema

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// A dummy schema used if we don't recognize a schema key. We unmarshal the key's contents anyway
// because it might contain embedded schemas referenced elsewhere in the document.
//
// Does this work with additionalProperties??
//
// TODO: should probably be interface{} and handle lists, dicts of schemas
// as well as single schemas.
type other struct {
	EmbeddedSchemas map[string]*Schema
}

func (o other) Validate(v interface{}) []ValidationError {
	return nil
}

func (o *other) UnmarshalJSON(b []byte) error {
	var s Schema
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	o.EmbeddedSchemas[""] = &s
	return nil
}

type maximum struct {
	json.Number
	exclusive bool
}

func (m maximum) Validate(v interface{}) []ValidationError {
	normalized, err := normalizeNumber(v)
	if err != nil {
		return []ValidationError{ValidationError{err.Error()}}
	}
	var isLarger bool
	switch n := normalized.(type) {
	case int64:
		isLarger, err = m.isLargerThanInt(n)
	case float64:
		isLarger, err = m.isLargerThanFloat(n)
	default:
		return nil
	}
	if err != nil {
		return nil
	}
	if !isLarger {
		maxErr := fmt.Sprintf("Value must be smaller than %s.", m)
		return []ValidationError{ValidationError{maxErr}}
	}
	return nil
}

func (m maximum) isLargerThanInt(n int64) (bool, error) {
	if !strings.Contains(m.String(), ".") {
		max, err := m.Int64()
		if err != nil {
			return false, err
		}
		return max > n || !m.exclusive && max == n, nil
	} else {
		return m.isLargerThanFloat(float64(n))
	}
}

func (m maximum) isLargerThanFloat(n float64) (isLarger bool, err error) {
	max, err := m.Float64()
	if err != nil {
		return
	}
	return max > n || !m.exclusive && max == n, nil
}

func (m *maximum) SetSchema(v map[string]json.RawMessage) error {
	value, ok := v["exclusiveMaximum"]
	if ok {
		// Ignore errors from Unmarshal. If exclusiveMaximum is a non boolean JSON
		// value we leave it as false.
		json.Unmarshal(value, &m.exclusive)
	}
	return nil
}

func (m *maximum) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &m.Number)
}

type minimum struct {
	json.Number
	exclusive bool
}

func (m minimum) Validate(v interface{}) []ValidationError {
	normalized, err := normalizeNumber(v)
	if err != nil {
		return []ValidationError{ValidationError{err.Error()}}
	}
	var isLarger bool
	switch n := normalized.(type) {
	case int64:
		isLarger, err = m.isLargerThanInt(n)
	case float64:
		isLarger, err = m.isLargerThanFloat(n)
	default:
		return nil
	}
	if err != nil {
		return nil
	}
	if isLarger {
		minErr := fmt.Sprintf("Value must be larger than %s.", m)
		return []ValidationError{ValidationError{minErr}}
	}
	return nil
}

func (m minimum) isLargerThanInt(n int64) (bool, error) {
	if !strings.Contains(m.String(), ".") {
		min, err := m.Int64()
		if err != nil {
			return false, nil
		}
		return min > n || !m.exclusive && min == n, nil
	} else {
		return m.isLargerThanFloat(float64(n))
	}
}

func (m minimum) isLargerThanFloat(n float64) (isLarger bool, err error) {
	min, err := m.Float64()
	if err != nil {
		return
	}
	return min > n || !m.exclusive && min == n, nil
}

func (m *minimum) SetSchema(v map[string]json.RawMessage) error {
	value, ok := v["exclusiveminimum"]
	if ok {
		// Ignore errors from Unmarshal. If exclusiveminimum is a non boolean JSON
		// value we leave it as false.
		json.Unmarshal(value, &m.exclusive)
	}
	return nil
}

func (m *minimum) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &m.Number)
}

type multipleOf int64

// Contrary to the spec, validation doesn't support floats in the schema
// or the data being validated. This is because of issues with math.Mod,
// e.g. math.Mod(0.0075, 0.0001) != 0.
func (m multipleOf) Validate(v interface{}) []ValidationError {
	normalized, err := normalizeNumber(v)
	if err != nil {
		return []ValidationError{ValidationError{err.Error()}}
	}
	n, ok := normalized.(int64)
	if !ok {
		return nil
	}
	if n%int64(m) != 0 {
		mulErr := ValidationError{fmt.Sprintf("Value must be a multiple of %d.", m)}
		return []ValidationError{mulErr}
	}
	return nil
}

func (m *multipleOf) UnmarshalJSON(b []byte) error {
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*m = multipleOf(n)
	return nil
}

type maxLength int

func (m maxLength) Validate(v interface{}) []ValidationError {
	l, ok := v.(string)
	if !ok {
		return nil
	}
	if utf8.RuneCountInString(l) > int(m) {
		lenErr := ValidationError{fmt.Sprintf("String length must be shorter than %d characters.", m)}
		return []ValidationError{lenErr}
	}
	return nil
}

type minLength int

func (m minLength) Validate(v interface{}) []ValidationError {
	l, ok := v.(string)
	if !ok {
		return nil
	}
	if utf8.RuneCountInString(l) < int(m) {
		lenErr := ValidationError{fmt.Sprintf("String length must be shorter than %d characters.", m)}
		return []ValidationError{lenErr}
	}
	return nil
}

type pattern struct {
	regexp.Regexp
}

func (p pattern) Validate(v interface{}) []ValidationError {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	if !p.MatchString(s) {
		patErr := ValidationError{fmt.Sprintf("String must match the pattern: \"%s\".", p.String())}
		return []ValidationError{patErr}
	}
	return nil
}

func (p *pattern) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	r, err := regexp.Compile(s)
	if err != nil {
		return err
	}
	p.Regexp = *r
	return nil
}

type format string

var dateTimeRegexp = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})-([0-9]{2})([Tt]([0-9]{2}):([0-9]{2}):([0-9]{2})(\.[0-9]+)?)?([Tt]([0-9]{2}):([0-9]{2}):([0-9]{2})(\\.[0-9]+)?)?(([Zz]|([+-])([0-9]{2}):([0-9]{2})))?`)
var mailRegexp = regexp.MustCompile(".+@.+")
var hostnameRegexp = regexp.MustCompile(`^[a-zA-Z](([-0-9a-zA-Z]+)?[0-9a-zA-Z])?(\.[a-zA-Z](([-0-9a-zA-Z]+)?[0-9a-zA-Z])?)*$`)

func (f format) Validate(v interface{}) []ValidationError {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	switch f {
	case "date-time":
		if !dateTimeRegexp.MatchString(s) {
			return []ValidationError{ValidationError{"Value must conform to RFC3339."}}
		}
	case "uri":
		if _, err := url.ParseRequestURI(s); err != nil {
			return []ValidationError{ValidationError{"Value must be a valid URI, according to RFC3986."}}
		}
	case "email":
		if !mailRegexp.MatchString(s) {
			return []ValidationError{ValidationError{"Value must be a valid email address, according to RFC5322."}}
		}
	case "ipv4":
		if net.ParseIP(s).To4() == nil {
			return []ValidationError{ValidationError{"Value must be a valid IPv4 address."}}
		}
	case "ipv6":
		if net.ParseIP(s).To16() == nil {
			return []ValidationError{ValidationError{"Value must be a valid IPv6 address."}}
		}
	case "hostname":
		formatErr := []ValidationError{ValidationError{"Value must be a valid hostname."}}
		if !hostnameRegexp.MatchString(s) || utf8.RuneCountInString(s) > 255 {
			return formatErr
		}
		labels := strings.Split(s, ".")
		for _, label := range labels {
			if utf8.RuneCountInString(label) > 63 {
				return formatErr
			}
		}
	}
	return nil
}

type additionalItems struct {
	EmbeddedSchemas map[string]*Schema
}

func (a additionalItems) Validate(v interface{}) []ValidationError {
	return nil
}

func (a *additionalItems) UnmarshalJSON(b []byte) error {
	var s Schema
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	a.EmbeddedSchemas[""] = &s
	return nil
}

type maxItems int

func (m maxItems) Validate(v interface{}) []ValidationError {
	l, ok := v.([]interface{})
	if !ok {
		return nil
	}
	if len(l) > int(m) {
		maxErr := ValidationError{fmt.Sprintf("Array must have fewer than %d items.", m)}
		return []ValidationError{maxErr}
	}
	return nil
}

type minItems int

func (m minItems) Validate(v interface{}) []ValidationError {
	l, ok := v.([]interface{})
	if !ok {
		return nil
	}
	if len(l) < int(m) {
		minErr := ValidationError{fmt.Sprintf("Array must have more than %d items.", m)}
		return []ValidationError{minErr}
	}
	return nil
}

// The spec[0] is useless for this keyword. The implemention here is based on the tests and this[1] guide.
//
// [0] http://json-schema.org/latest/json-schema-validation.html#anchor37
// [1] http://spacetelescope.github.io/understanding-json-schema/reference/array.html
type items struct {
	EmbeddedSchemas   map[string]*Schema
	schema            *Schema
	schemaSlice       []*Schema
	additionalAllowed bool
	additionalItems   *Schema
}

func (i items) Validate(v interface{}) []ValidationError {
	var valErrs []ValidationError
	instances, ok := v.([]interface{})
	if !ok {
		return nil
	}
	if i.schema != nil {
		for _, value := range instances {
			valErrs = append(valErrs, i.schema.Validate(value)...)
		}
	} else if i.schemaSlice != nil {
		for pos, value := range instances {
			if pos <= len(i.schemaSlice)-1 {
				schema := i.schemaSlice[pos]
				valErrs = append(valErrs, schema.Validate(value)...)
			} else if i.additionalAllowed {
				if i.additionalItems == nil {
					continue
				}
				valErrs = append(valErrs, i.additionalItems.Validate(value)...)
			} else if !i.additionalAllowed {
				return []ValidationError{ValidationError{"Additional items aren't allowed."}}
			}
		}
	}
	return valErrs
}

func (i *items) SetSchema(v map[string]json.RawMessage) error {
	//TODO: ian should fix.
	i.additionalAllowed = true
	value, ok := v["additionalItems"]
	if !ok {
		return nil
	}
	json.Unmarshal(value, &i.additionalAllowed)
	// json.Unmarshal(value, &i.additionalItems)
	return nil
}

func (i *items) GetNeighboringSchemas(nodes map[string]*Node) {
	n, ok := nodes["additionalItems"]
	if !ok {
		return
	}
	s, ok := n.EmbeddedSchemas[""]
	if !ok {
		return
	}
	i.additionalItems = s
}

func (i *items) UnmarshalJSON(b []byte) error {

	// If "items" is a single schema, stop here.
	if err := json.Unmarshal(b, &i.schema); err == nil {
		i.EmbeddedSchemas[""] = i.schema
		return nil
	}
	i.schema = nil

	// The only other valid option is a list of schemas.
	if err := json.Unmarshal(b, &i.schemaSlice); err != nil {
		i.schemaSlice = nil
		return err
	}
	for index, v := range i.schemaSlice {
		i.EmbeddedSchemas[strconv.Itoa(index)] = v
	}
	return nil
}

type dependencies struct {
	EmbeddedSchemas map[string]*Schema
	propertyDeps    map[string]propertySet
}

type propertySet map[string]struct{}

func (d dependencies) Validate(v interface{}) []ValidationError {
	var valErrs []ValidationError
	val, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}

	// Handle schema dependencies.
	for key, schema := range d.EmbeddedSchemas {
		if _, ok := val[key]; !ok {
			continue
		}
		valErrs = append(valErrs, schema.Validate(v)...)
	}

	// Handle property dependencies.
	for key, set := range d.propertyDeps {
		if _, ok := val[key]; !ok {
			continue
		}
		for a := range set {
			if _, ok := val[a]; !ok {
				valErrs = append(valErrs, ValidationError{
					fmt.Sprintf("instance does not have a property with the name %s", a)})
			}
		}
	}

	return valErrs
}

func (d *dependencies) UnmarshalJSON(b []byte) error {
	var c map[string]json.RawMessage
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}

	// d.schemaDeps = make(map[string]Schema, len(c))
	for k, v := range c {
		var s Schema
		if err := json.Unmarshal(v, &s); err != nil {
			continue
		}
		d.EmbeddedSchemas[k] = &s
	}

	d.propertyDeps = make(map[string]propertySet, len(c))
	for k, v := range c {
		var props []string
		if err := json.Unmarshal(v, &props); err != nil {
			continue
		}
		set := make(propertySet, len(props))
		for _, p := range props {
			set[p] = struct{}{}
		}
		d.propertyDeps[k] = set
	}

	if len(d.propertyDeps) == 0 && len(d.EmbeddedSchemas) == 0 {
		return errors.New("no valid schema or property dependency validators")
	}
	return nil
}

type maxProperties int

func (m maxProperties) Validate(v interface{}) []ValidationError {
	val, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	if len(val) > int(m) {
		return []ValidationError{ValidationError{
			fmt.Sprintf("Object has more properties than maxProperties (%d > %d)", len(val), m)}}
	}
	return nil
}

func (m *maxProperties) UnmarshalJSON(b []byte) error {
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	if n < 0 {
		return errors.New("maxProperties cannot be smaller than zero")
	}
	*m = maxProperties(n)
	return nil
}

type minProperties int

func (m minProperties) Validate(v interface{}) []ValidationError {
	val, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	if len(val) < int(m) {
		return []ValidationError{ValidationError{
			fmt.Sprintf("Object has fewer properties than minProperties (%d < %d)", len(val), m)}}
	}
	return nil
}

func (m *minProperties) UnmarshalJSON(b []byte) error {
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	if n < 0 {
		return errors.New("minProperties cannot be smaller than zero")
	}
	*m = minProperties(n)
	return nil
}

type patternProperties struct {
	EmbeddedSchemas map[string]*Schema
	// TODO: find a better name for object.
	object               map[string]regexp.Regexp
	propertiesIsNeighbor bool
}

func (p patternProperties) Validate(v interface{}) []ValidationError {
	// In this case validation will be handled by the "properties" validator.
	if p.propertiesIsNeighbor == true {
		return nil
	}

	var valErrs []ValidationError
	data, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	for dataKey, dataVal := range data {
		for key, val := range p.object {
			if val.MatchString(dataKey) {
				valErrs = append(valErrs, p.EmbeddedSchemas[key].Validate(dataVal)...)
			}
		}
	}
	return valErrs
}

func (p *patternProperties) SetSchema(v map[string]json.RawMessage) error {
	if _, ok := v["properties"]; ok {
		p.propertiesIsNeighbor = true
	}
	return nil
}

func (p *patternProperties) UnmarshalJSON(b []byte) error {
	m := make(map[string]*Schema)
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	p.object = make(map[string]regexp.Regexp, len(m))
	for k, v := range m {
		r, err := regexp.Compile(k)
		if err != nil {
			return err
		}
		p.EmbeddedSchemas[k] = v
		p.object[k] = *r
	}
	return nil
}

type properties struct {
	EmbeddedSchemas            map[string]*Schema
	patternProperties          *patternProperties
	additionalPropertiesBool   bool
	additionalPropertiesObject *Schema
}

func (p properties) Validate(v interface{}) []ValidationError {
	var valErrs []ValidationError
	dataMap, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	for dataKey, dataVal := range dataMap {
		var match = false
		schema, ok := p.EmbeddedSchemas[dataKey]
		if ok {
			valErrs = append(valErrs, schema.Validate(dataVal)...)
			match = true
		}
		if p.patternProperties != nil {
			for key, val := range p.patternProperties.object {
				if val.MatchString(dataKey) {
					valErrs = append(valErrs, p.patternProperties.EmbeddedSchemas[key].Validate(dataVal)...)
					match = true
				}
			}
		}
		if match {
			continue
		}
		if p.additionalPropertiesObject != nil {
			valErrs = append(valErrs, p.additionalPropertiesObject.Validate(dataVal)...)
			continue
		}
		if !p.additionalPropertiesBool {
			valErrs = append([]ValidationError{ValidationError{"Additional properties aren't allowed"}})
		}
	}
	return valErrs
}

func (p *properties) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &p.EmbeddedSchemas)
}

func (p *properties) GetNeighboringSchemas(nodes map[string]*Node) {
	v, ok := nodes["patternProperties"]
	if !ok {
		return
	}
	v2, ok := v.Validator.(*patternProperties)
	if !ok {
		return
	}
	p.patternProperties = v2
}

func (p *properties) SetSchema(v map[string]json.RawMessage) error {
	p.additionalPropertiesBool = true
	addVal, ok := v["additionalProperties"]
	if !ok {
		return nil
	}
	json.Unmarshal(addVal, &p.additionalPropertiesBool)
	if err := json.Unmarshal(addVal, &p.additionalPropertiesObject); err != nil {
		p.additionalPropertiesObject = nil
	}
	return nil
}

type required map[string]struct{}

func (r required) Validate(v interface{}) []ValidationError {
	var valErrs []ValidationError
	data, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	for key := range r {
		if _, ok := data[key]; !ok {
			valErrs = append(valErrs, ValidationError{fmt.Sprintf("Required error. The data must be an object with %v as one of its keys", key)})
		}
	}
	return valErrs
}

func (r *required) UnmarshalJSON(b []byte) error {
	var l []string
	if err := json.Unmarshal(b, &l); err != nil {
		return err
	}
	*r = make(required)
	for _, val := range l {
		(*r)[val] = struct{}{}
	}
	return nil
}

type allOf struct {
	EmbeddedSchemas map[string]*Schema
}

func (a allOf) Validate(v interface{}) (valErrs []ValidationError) {
	for _, schema := range a.EmbeddedSchemas {
		valErrs = append(valErrs, schema.Validate(v)...)
	}
	return
}

func (a *allOf) UnmarshalJSON(b []byte) error {
	var schemas []*Schema
	if err := json.Unmarshal(b, &schemas); err != nil {
		return err
	}
	for i, v := range schemas {
		a.EmbeddedSchemas[strconv.Itoa(i)] = v
	}
	return nil
}

type anyOf struct {
	EmbeddedSchemas map[string]*Schema
}

func (a anyOf) Validate(v interface{}) []ValidationError {
	for _, schema := range a.EmbeddedSchemas {
		if schema.Validate(v) == nil {
			return nil
		}
	}
	return []ValidationError{
		ValidationError{"Validation failed for each schema in 'anyOf'."}}
}

func (a *anyOf) UnmarshalJSON(b []byte) error {
	var schemas []*Schema
	if err := json.Unmarshal(b, &schemas); err != nil {
		return err
	}
	for i, v := range schemas {
		a.EmbeddedSchemas[strconv.Itoa(i)] = v
	}
	return nil
}

type definitions struct {
	EmbeddedSchemas map[string]*Schema
}

func (d definitions) Validate(v interface{}) []ValidationError {
	return nil
}

func (d *definitions) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &d.EmbeddedSchemas)
}

type enum []interface{}

func (a enum) Validate(v interface{}) []ValidationError {
	for _, b := range a {
		if DeepEqual(v, b) {
			return nil
		}
	}
	return []ValidationError{
		ValidationError{fmt.Sprintf("Enum error. The data must be equal to one of these values %v.", a)}}
}

type not struct {
	EmbeddedSchemas map[string]*Schema
}

func (n not) Validate(v interface{}) []ValidationError {
	// TODO: error handling.
	schema := n.EmbeddedSchemas[""]
	if schema.Validate(v) == nil {
		return []ValidationError{ValidationError{"The 'not' schema didn't raise an error."}}
	}
	return nil
}

func (n *not) UnmarshalJSON(b []byte) error {
	var s Schema
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	n.EmbeddedSchemas[""] = &s
	return nil
}

type oneOf struct {
	EmbeddedSchemas map[string]*Schema
}

func (o oneOf) Validate(v interface{}) []ValidationError {
	var succeeded int
	for _, schema := range o.EmbeddedSchemas {
		if schema.Validate(v) == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		return []ValidationError{ValidationError{
			fmt.Sprintf("Validation passed for %d schemas in 'oneOf'.", succeeded)}}
	}
	return nil
}

func (o *oneOf) UnmarshalJSON(b []byte) error {
	var schemas []*Schema
	if err := json.Unmarshal(b, &schemas); err != nil {
		return err
	}
	for i, v := range schemas {
		o.EmbeddedSchemas[strconv.Itoa(i)] = v
	}
	return nil
}

type ref string

func (r ref) Validate(v interface{}) []ValidationError {
	return nil
}

type typeValidator map[string]bool

func (t typeValidator) Validate(v interface{}) []ValidationError {
	var s string

	switch x := v.(type) {

	case string:
		s = "string"
	case bool:
		s = "boolean"
	case nil:
		s = "null"
	case []interface{}:
		s = "array"
	case map[string]interface{}:
		s = "object"

	case json.Number:
		if strings.Contains(x.String(), ".") {
			s = "number"
		} else {
			s = "integer"
		}
	case float64:
		s = "number"
	}

	_, ok := t[s]

	// The "number" type includes the "integer" type.
	if !ok && s == "integer" {
		_, ok = t["number"]
	}

	if !ok {
		types := make([]string, 0, len(t))
		for key := range t {
			types = append(types, key)
		}
		return []ValidationError{ValidationError{
			fmt.Sprintf("Value must be one of these types: %s.", types)}}
	}
	return nil
}

func (t *typeValidator) UnmarshalJSON(b []byte) error {
	*t = make(typeValidator)
	var s string
	var l []string

	// The value of the "type" keyword can be a string or an array.
	if err := json.Unmarshal(b, &s); err != nil {
		err = json.Unmarshal(b, &l)
		if err != nil {
			return err
		}
	} else {
		l = []string{s}
	}

	for _, val := range l {
		(*t)[val] = true
	}
	return nil
}
