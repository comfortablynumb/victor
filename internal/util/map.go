package util

import "fmt"

// Static functions

func ConvertMap[K comparable, V any](m map[K]any) (map[K]V, error) {
	newMap := make(map[K]V)

	for k, v := range m {
		value, ok := v.(V)

		if !ok {
			return nil, fmt.Errorf("map has values of unexpected types. Key: %#v - Value: %#v", k, v)
		}

		newMap[k] = value
	}

	return newMap, nil
}
