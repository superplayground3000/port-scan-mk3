package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unsafe"
)

type snapshotSchema uint8

const (
	snapshotTopSchema snapshotSchema = iota
	snapshotChunkSchema
	snapshotPreScanSchema
	snapshotOutputSchema
	snapshotExpansionSchema
)

func validateSnapshotSchema(data []byte) (bool, error) {
	preScanPresent := false
	err := visitJSONObject(data, func(name string, value []byte) error {
		switch name {
		case "chunks":
			return visitJSONArray(value, func(item []byte) error {
				return validateSnapshotObject(item, snapshotChunkSchema)
			})
		case "pre_scan_ping":
			preScanPresent = true
			return validateSnapshotObject(value, snapshotPreScanSchema)
		case "output":
			return validateSnapshotObject(value, snapshotOutputSchema)
		case "target_expansion":
			return validateSnapshotObject(value, snapshotExpansionSchema)
		case "rich_deny_excluded", "target_semantics_version", "basic_port_fallback":
			return nil
		default:
			return fmt.Errorf("json: unknown field %q", name)
		}
	})
	return preScanPresent, err
}

func validateSnapshotObject(data []byte, schema snapshotSchema) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	return visitJSONObject(data, func(name string, _ []byte) error {
		if snapshotFieldAllowed(schema, name) {
			return nil
		}
		return fmt.Errorf("json: unknown field %q", name)
	})
}

func snapshotFieldAllowed(schema snapshotSchema, name string) bool {
	switch schema {
	case snapshotChunkSchema:
		switch name {
		case "cidr", "cidr_name", "ports", "next_index", "scanned_count", "total_count", "status":
			return true
		}
	case snapshotPreScanSchema:
		switch name {
		case "enabled", "timeout_ms", "unreachable_ipv4_u32":
			return true
		}
	case snapshotOutputSchema:
		switch name {
		case "scan_path", "open_path":
			return true
		}
	case snapshotExpansionSchema:
		switch name {
		case "candidate_count", "candidate_limit", "memory_limit_gb":
			return true
		}
	}
	return false
}

func visitJSONObject(data []byte, visit func(string, []byte) error) error {
	start := skipJSONSpace(data, 0)
	if start >= len(data) || data[start] != '{' {
		return nil
	}
	position := start + 1
	for {
		position = skipJSONSpace(data, position)
		if position >= len(data) || data[position] == '}' {
			return nil
		}
		keyEnd, ok := scanJSONString(data, position)
		if !ok {
			return nil
		}
		nameBytes := data[position+1 : keyEnd-1]
		name := bytesToString(nameBytes)
		if bytes.IndexByte(nameBytes, '\\') >= 0 {
			if err := json.Unmarshal(data[position:keyEnd], &name); err != nil {
				return nil
			}
		}
		position = skipJSONSpace(data, keyEnd)
		if position >= len(data) || data[position] != ':' {
			return nil
		}
		valueStart := skipJSONSpace(data, position+1)
		valueEnd, ok := scanJSONValue(data, valueStart)
		if !ok {
			return nil
		}
		if err := visit(name, data[valueStart:valueEnd]); err != nil {
			return err
		}
		position = skipJSONSpace(data, valueEnd)
		if position >= len(data) || data[position] == '}' {
			return nil
		}
		if data[position] != ',' {
			return nil
		}
		position++
	}
}

func visitJSONArray(data []byte, visit func([]byte) error) error {
	start := skipJSONSpace(data, 0)
	if start >= len(data) || data[start] != '[' {
		return nil
	}
	position := start + 1
	for {
		position = skipJSONSpace(data, position)
		if position >= len(data) || data[position] == ']' {
			return nil
		}
		valueEnd, ok := scanJSONValue(data, position)
		if !ok {
			return nil
		}
		if err := visit(data[position:valueEnd]); err != nil {
			return err
		}
		position = skipJSONSpace(data, valueEnd)
		if position >= len(data) || data[position] == ']' {
			return nil
		}
		if data[position] != ',' {
			return nil
		}
		position++
	}
}

func scanJSONValue(data []byte, start int) (int, bool) {
	if start >= len(data) {
		return start, false
	}
	if data[start] == '"' {
		return scanJSONString(data, start)
	}
	if data[start] != '{' && data[start] != '[' {
		position := start
		for position < len(data) {
			switch data[position] {
			case ',', '}', ']', ' ', '\t', '\r', '\n':
				return position, position > start
			}
			position++
		}
		return position, position > start
	}

	depth := 1
	for position := start + 1; position < len(data); position++ {
		switch data[position] {
		case '"':
			end, ok := scanJSONString(data, position)
			if !ok {
				return position, false
			}
			position = end - 1
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return position + 1, true
			}
		}
	}
	return len(data), false
}

func scanJSONString(data []byte, start int) (int, bool) {
	if start >= len(data) || data[start] != '"' {
		return start, false
	}
	escaped := false
	for position := start + 1; position < len(data); position++ {
		if escaped {
			escaped = false
			continue
		}
		if data[position] == '\\' {
			escaped = true
			continue
		}
		if data[position] == '"' {
			return position + 1, true
		}
	}
	return len(data), false
}

func skipJSONSpace(data []byte, position int) int {
	for position < len(data) {
		switch data[position] {
		case ' ', '\t', '\r', '\n':
			position++
		default:
			return position
		}
	}
	return position
}

func bytesToString(data []byte) string {
	return unsafe.String(unsafe.SliceData(data), len(data))
}
