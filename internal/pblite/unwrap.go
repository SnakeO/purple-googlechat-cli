// unwrap.go handles Google Chat's pblite response wrapper format.
// Responses come wrapped as [["method.name", [actual_pblite_data]]].
// This module extracts the inner pblite array.
package pblite

import (
	"encoding/json"
)

// UnwrapResponse extracts the pblite payload from Google's response wrapper.
// Input format: [["method.name", [pblite_data]]]
// Returns the inner [pblite_data] array as raw JSON.
func UnwrapResponse(data []byte) []byte {
	var outer []json.RawMessage
	if err := json.Unmarshal(data, &outer); err != nil {
		return data
	}

	if len(outer) == 0 {
		return data
	}

	// Check if first element is itself an array (wrapper format)
	var inner []json.RawMessage
	if err := json.Unmarshal(outer[0], &inner); err != nil {
		return data
	}

	// Wrapper format: inner[0] = method string, inner[1:] = pblite fields
	if len(inner) >= 2 {
		var methodName string
		if json.Unmarshal(inner[0], &methodName) == nil {
			// Reconstruct the pblite array from inner[1:]
			fields := inner[1:]
			rebuilt, err := json.Marshal(fields)
			if err != nil {
				return data
			}
			return rebuilt
		}
	}

	return data
}
