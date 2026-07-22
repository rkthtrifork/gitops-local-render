package adapter

import "fmt"

func Map(value any, field string) (map[string]any, error) {
	result, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", field)
	}
	return result, nil
}

func OptionalMap(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func Slice(value any, field string) ([]any, error) {
	result, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list", field)
	}
	return result, nil
}

func String(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func Bool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}
