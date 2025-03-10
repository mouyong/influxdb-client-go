package genclient
import(
	"sort"
	"fmt"
	"strings"
	"reflect"
	"time"
	"strconv"
)

// This is borrowed from https://github.com/deepmap/oapi-codegen/blob/cf4fce0f88dc56a9fafc65cc6ff6d4a7c0ef0f9d/pkg/runtime/styleparam.go
// so we can eliminate the dependency
func styleParam(style string, explode bool, paramName string, value interface{}) (string, error) {
	t := reflect.TypeOf(value)
	v := reflect.ValueOf(value)

	// Things may be passed in by pointer, we need to dereference, so return
	// error on nil.
	if t.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "", fmt.Errorf("value is a nil pointer")
		}
		v = reflect.Indirect(v)
		t = v.Type()
	}

	switch t.Kind() {
	case reflect.Slice:
		n := v.Len()
		sliceVal := make([]interface{}, n)
		for i := 0; i < n; i++ {
			sliceVal[i] = v.Index(i).Interface()
		}
		return styleSlice(style, explode, paramName, sliceVal)
	case reflect.Struct:
		return styleStruct(style, explode, paramName, value)
	default:
		return stylePrimitive(style, explode, paramName, value)
	}
}


func styleSlice(style string, explode bool, paramName string, values []interface{}) (string, error) {
	var prefix string
	var separator string

	switch style {
	case "simple":
		separator = ","
	case "label":
		prefix = "."
		if explode {
			separator = "."
		} else {
			separator = ","
		}
	case "matrix":
		prefix = ";"+paramName+"="
		if explode {
			separator = prefix
		} else {
			separator = ","
		}
	case "form":
		prefix = paramName + "="
		if explode {
			separator = "&" + prefix
		} else {
			separator = ","
		}
	case "spaceDelimited":
		prefix = paramName+"="
		if explode {
			separator = "&" + prefix
		} else {
			separator = " "
		}
	case "pipeDelimited":
		prefix = paramName + "="
		if explode {
			separator = "&" + prefix
		} else {
			separator = "|"
		}
	default:
		return "", fmt.Errorf("unsupported style '%s'", style)
	}

	// We're going to assume here that the array is one of simple types.
	var err error
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i], err = primitiveToString(v)
		if err != nil {
			return "", fmt.Errorf("error formatting '%s': %s", paramName, err)
		}
	}
	return prefix + strings.Join(parts, separator), nil
}

func sortedKeys(strMap map[string]string) []string {
	keys := make([]string, len(strMap))
	i := 0
	for k := range strMap {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	return keys
}

func styleStruct(style string, explode bool, paramName string, value interface{}) (string, error) {
	// This is a special case. The struct may be a time, in which case, marshal
	// it in RFC3339 format.
	if timeVal, ok := value.(*time.Time); ok {
		return timeVal.Format(time.RFC3339Nano), nil
	}

	// Otherwise, we need to build a dictionary of the struct's fields. Each
	// field may only be a primitive value.
	v := reflect.ValueOf(value)
	t := reflect.TypeOf(value)
	fieldDict := make(map[string]string)

	for i := 0; i < t.NumField(); i++ {
		fieldT := t.Field(i)
		// Find the json annotation on the field, and use the json specified
		// name if available, otherwise, just the field name.
		tag := fieldT.Tag.Get("json")
		fieldName := fieldT.Name
		if tag != "" {
			tagParts := strings.Split(tag, ",")
			name := tagParts[0]
			if name != "" {
				fieldName = name
			}
		}
		f := v.Field(i)

		// Unset optional fields will be nil pointers, skip over those.
		if f.Type().Kind() == reflect.Ptr && f.IsNil() {
			continue
		}
		str, err := primitiveToString(f.Interface())
		if err != nil {
			return "", fmt.Errorf("error formatting '%s': %s", paramName, err)
		}
		fieldDict[fieldName] = str
	}

	var parts []string

	// This works for everything except deepObject. We'll handle that one
	// separately.
	if style != "deepObject" {
		if explode {
			for _, k := range sortedKeys(fieldDict) {
				v := fieldDict[k]
				parts = append(parts, k+"="+v)
			}
		} else {
			for _, k := range sortedKeys(fieldDict) {
				v := fieldDict[k]
				parts = append(parts, k)
				parts = append(parts, v)
			}
		}
	}

	var prefix string
	var separator string

	switch style {
	case "simple":
		separator = ","
	case "label":
		prefix = "."
		if explode {
			separator = prefix
		} else {
			separator = ","
		}
	case "matrix":
		if explode {
			separator = ";"
			prefix = ";"
		} else {
			separator = ","
			prefix = fmt.Sprintf(";%s=", paramName)
		}
	case "form":
		if explode {
			separator = "&"
		} else {
			prefix = fmt.Sprintf("%s=", paramName)
			separator = ","
		}
	case "deepObject":
		{
			if !explode {
				return "", fmt.Errorf("deepObject parameters must be exploded")
			}
			for _, k := range sortedKeys(fieldDict) {
				v := fieldDict[k]
				part := fmt.Sprintf("%s[%s]=%s", paramName, k, v)
				parts = append(parts, part)
			}
			separator = "&"
		}
	default:
		return "", fmt.Errorf("unsupported style '%s'", style)
	}

	return prefix + strings.Join(parts, separator), nil
}

func stylePrimitive(style string, explode bool, paramName string, value interface{}) (string, error) {
	strVal, err := primitiveToString(value)
	if err != nil {
		return "", err
	}

	var prefix string
	switch style {
	case "simple":
	case "label":
		prefix = "."
	case "matrix":
		prefix = fmt.Sprintf(";%s=", paramName)
	case "form":
		prefix = fmt.Sprintf("%s=", paramName)
	default:
		return "", fmt.Errorf("unsupported style '%s'", style)
	}
	return prefix + strVal, nil
}

// Converts a primitive value to a string. We need to do this based on the
// Kind of an interface, not the Type to work with aliased types.
func primitiveToString(value interface{}) (string, error) {
	var output string

	// Values may come in by pointer for optionals, so make sure to dereferene.
	v := reflect.Indirect(reflect.ValueOf(value))
	t := v.Type()
	kind := t.Kind()

	switch kind {
	case reflect.Int8, reflect.Int32, reflect.Int64, reflect.Int:
		output = strconv.FormatInt(v.Int(), 10)
	case reflect.Float32, reflect.Float64:
		output = strconv.FormatFloat(v.Float(), 'f', -1, 64)
	case reflect.Bool:
		if v.Bool() {
			output = "true"
		} else {
			output = "false"
		}
	case reflect.String:
		output = v.String()
	default:
		return "", fmt.Errorf("unsupported type %s", reflect.TypeOf(value).String())
	}
	return output, nil
}