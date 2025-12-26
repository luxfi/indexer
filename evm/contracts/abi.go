// Copyright (c) 2025 Lux Partners Limited
// SPDX-License-Identifier: MIT

package contracts

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// ABIDecoder decodes function calls and event logs using ABI.
type ABIDecoder struct {
	methods map[string]MethodSignature // selector -> method
	events  map[string]EventSignature  // topic0 -> event
}

// NewABIDecoder creates a decoder from ABI JSON.
func NewABIDecoder(abiJSON json.RawMessage) (*ABIDecoder, error) {
	methods, events, err := ParseABI(abiJSON)
	if err != nil {
		return nil, err
	}

	decoder := &ABIDecoder{
		methods: make(map[string]MethodSignature),
		events:  make(map[string]EventSignature),
	}

	for _, m := range methods {
		decoder.methods[strings.ToLower(m.Selector)] = m
	}
	for _, e := range events {
		decoder.events[strings.ToLower(e.Topic0)] = e
	}

	return decoder, nil
}

// DecodeCall decodes a function call from input data.
func (d *ABIDecoder) DecodeCall(input string) (*DecodedCall, error) {
	input = strings.TrimPrefix(strings.ToLower(input), "0x")
	if len(input) < 8 {
		return nil, errors.New("input too short")
	}

	selector := "0x" + input[:8]
	method, ok := d.methods[selector]
	if !ok {
		return nil, fmt.Errorf("unknown selector: %s", selector)
	}

	params := make(map[string]interface{})
	if len(input) > 8 {
		data := input[8:]
		decoded, err := decodeParameters(method.Inputs, data)
		if err != nil {
			return nil, fmt.Errorf("decode parameters: %w", err)
		}
		params = decoded
	}

	return &DecodedCall{
		MethodID:   selector,
		Name:       method.Name,
		Signature:  method.Signature,
		Parameters: params,
	}, nil
}

// DecodeLog decodes an event log.
func (d *ABIDecoder) DecodeLog(topics []string, data string) (*DecodedLog, error) {
	if len(topics) == 0 {
		return nil, errors.New("no topics")
	}

	topic0 := strings.ToLower(topics[0])
	event, ok := d.events[topic0]
	if !ok {
		return nil, fmt.Errorf("unknown event: %s", topic0)
	}

	indexed := make(map[string]interface{})
	params := make(map[string]interface{})

	// Separate indexed and non-indexed parameters
	var indexedInputs []ABIParam
	var dataInputs []ABIParam
	topicIndex := 1 // topic[0] is the event signature

	for _, input := range event.Inputs {
		if input.Indexed {
			indexedInputs = append(indexedInputs, input)
		} else {
			dataInputs = append(dataInputs, input)
		}
	}

	// Decode indexed parameters from topics
	for _, input := range indexedInputs {
		if topicIndex >= len(topics) {
			break
		}
		value, err := decodeTopicValue(input.Type, topics[topicIndex])
		if err != nil {
			indexed[input.Name] = topics[topicIndex] // Return raw topic on error
		} else {
			indexed[input.Name] = value
		}
		topicIndex++
	}

	// Decode non-indexed parameters from data
	data = strings.TrimPrefix(strings.ToLower(data), "0x")
	if len(data) > 0 && len(dataInputs) > 0 {
		decoded, err := decodeParameters(dataInputs, data)
		if err == nil {
			for k, v := range decoded {
				params[k] = v
			}
		}
	}

	return &DecodedLog{
		Topic0:     topic0,
		Name:       event.Name,
		Signature:  event.Signature,
		Parameters: params,
		Indexed:    indexed,
	}, nil
}

// decodeParameters decodes ABI-encoded parameters.
func decodeParameters(inputs []ABIParam, data string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	offset := 0

	for _, input := range inputs {
		if offset >= len(data) {
			break
		}

		value, consumed, err := decodeValue(input.Type, data[offset:], input.Components)
		if err != nil {
			return nil, fmt.Errorf("decode %s: %w", input.Name, err)
		}

		name := input.Name
		if name == "" {
			name = fmt.Sprintf("param%d", len(result))
		}
		result[name] = value
		offset += consumed
	}

	return result, nil
}

// decodeValue decodes a single ABI value.
func decodeValue(typ string, data string, components []ABIParam) (interface{}, int, error) {
	// Handle arrays
	if strings.HasSuffix(typ, "[]") {
		baseType := strings.TrimSuffix(typ, "[]")
		return decodeDynamicArray(baseType, data, components)
	}
	if idx := strings.LastIndex(typ, "["); idx != -1 && strings.HasSuffix(typ, "]") {
		baseType := typ[:idx]
		sizeStr := typ[idx+1 : len(typ)-1]
		var size int
		fmt.Sscanf(sizeStr, "%d", &size)
		return decodeFixedArray(baseType, data, size, components)
	}

	// Handle tuples
	if typ == "tuple" || strings.HasPrefix(typ, "(") {
		return decodeTuple(data, components)
	}

	// Handle basic types
	return decodeBasicType(typ, data)
}

// decodeBasicType decodes a basic ABI type.
func decodeBasicType(typ string, data string) (interface{}, int, error) {
	if len(data) < 64 {
		return nil, 0, errors.New("data too short")
	}

	word := data[:64]

	switch {
	case typ == "address":
		return "0x" + word[24:], 64, nil

	case typ == "bool":
		if word[63] == '1' {
			return true, 64, nil
		}
		return false, 64, nil

	case strings.HasPrefix(typ, "uint"):
		n := new(big.Int)
		n.SetString(word, 16)
		return n.String(), 64, nil

	case strings.HasPrefix(typ, "int"):
		n := new(big.Int)
		n.SetString(word, 16)
		// Handle two's complement for signed integers
		bits := 256
		if len(typ) > 3 {
			fmt.Sscanf(typ[3:], "%d", &bits)
		}
		if n.Bit(bits-1) == 1 {
			// Negative number
			maxVal := new(big.Int).Lsh(big.NewInt(1), uint(bits))
			n.Sub(n, maxVal)
		}
		return n.String(), 64, nil

	case strings.HasPrefix(typ, "bytes") && len(typ) > 5:
		// Fixed bytes (bytes1 - bytes32)
		var size int
		fmt.Sscanf(typ[5:], "%d", &size)
		return "0x" + word[:size*2], 64, nil

	case typ == "bytes":
		// Dynamic bytes
		return decodeDynamicBytes(data)

	case typ == "string":
		// Dynamic string
		bytes, consumed, err := decodeDynamicBytes(data)
		if err != nil {
			return nil, 0, err
		}
		decoded, _ := hex.DecodeString(strings.TrimPrefix(bytes.(string), "0x"))
		return string(decoded), consumed, nil

	default:
		// Unknown type, return raw hex
		return "0x" + word, 64, nil
	}
}

// decodeDynamicBytes decodes dynamic bytes.
func decodeDynamicBytes(data string) (interface{}, int, error) {
	if len(data) < 64 {
		return nil, 0, errors.New("data too short for dynamic bytes")
	}

	// First word is offset
	offsetHex := data[:64]
	offset := hexToInt(offsetHex) * 2 // Convert to hex chars

	if offset >= len(data) {
		return nil, 0, errors.New("offset out of bounds")
	}

	// Read length at offset
	if offset+64 > len(data) {
		return nil, 0, errors.New("length out of bounds")
	}
	lengthHex := data[offset : offset+64]
	length := hexToInt(lengthHex)

	// Read bytes
	start := offset + 64
	end := start + length*2
	if end > len(data) {
		end = len(data)
	}

	return "0x" + data[start:end], 64, nil
}

// decodeDynamicArray decodes a dynamic array.
func decodeDynamicArray(baseType, data string, components []ABIParam) (interface{}, int, error) {
	if len(data) < 64 {
		return nil, 0, errors.New("data too short for dynamic array")
	}

	// First word is offset
	offsetHex := data[:64]
	offset := hexToInt(offsetHex) * 2

	if offset >= len(data) {
		return nil, 0, errors.New("offset out of bounds")
	}

	// Read length at offset
	if offset+64 > len(data) {
		return nil, 0, errors.New("length out of bounds")
	}
	lengthHex := data[offset : offset+64]
	length := hexToInt(lengthHex)

	// Decode array elements
	result := make([]interface{}, length)
	elemData := data[offset+64:]

	for i := 0; i < length; i++ {
		value, _, err := decodeValue(baseType, elemData, components)
		if err != nil {
			return nil, 0, fmt.Errorf("decode array element %d: %w", i, err)
		}
		result[i] = value
		if len(elemData) >= 64 {
			elemData = elemData[64:]
		}
	}

	return result, 64, nil
}

// decodeFixedArray decodes a fixed-size array.
func decodeFixedArray(baseType, data string, size int, components []ABIParam) (interface{}, int, error) {
	result := make([]interface{}, size)
	offset := 0

	for i := 0; i < size; i++ {
		if offset >= len(data) {
			break
		}
		value, consumed, err := decodeValue(baseType, data[offset:], components)
		if err != nil {
			return nil, 0, fmt.Errorf("decode array element %d: %w", i, err)
		}
		result[i] = value
		offset += consumed
	}

	return result, offset, nil
}

// decodeTuple decodes a tuple (struct).
func decodeTuple(data string, components []ABIParam) (interface{}, int, error) {
	result := make(map[string]interface{})
	offset := 0

	for i, comp := range components {
		if offset >= len(data) {
			break
		}
		value, consumed, err := decodeValue(comp.Type, data[offset:], comp.Components)
		if err != nil {
			return nil, 0, fmt.Errorf("decode tuple field %d: %w", i, err)
		}
		name := comp.Name
		if name == "" {
			name = fmt.Sprintf("field%d", i)
		}
		result[name] = value
		offset += consumed
	}

	return result, offset, nil
}

// decodeTopicValue decodes a value from an event topic.
func decodeTopicValue(typ string, topic string) (interface{}, error) {
	topic = strings.TrimPrefix(strings.ToLower(topic), "0x")
	if len(topic) != 64 {
		return nil, errors.New("invalid topic length")
	}

	switch {
	case typ == "address":
		return "0x" + topic[24:], nil

	case typ == "bool":
		if topic[63] == '1' {
			return true, nil
		}
		return false, nil

	case strings.HasPrefix(typ, "uint"):
		n := new(big.Int)
		n.SetString(topic, 16)
		return n.String(), nil

	case strings.HasPrefix(typ, "int"):
		n := new(big.Int)
		n.SetString(topic, 16)
		bits := 256
		if len(typ) > 3 {
			fmt.Sscanf(typ[3:], "%d", &bits)
		}
		if n.Bit(bits-1) == 1 {
			maxVal := new(big.Int).Lsh(big.NewInt(1), uint(bits))
			n.Sub(n, maxVal)
		}
		return n.String(), nil

	case strings.HasPrefix(typ, "bytes") && len(typ) <= 7:
		var size int
		fmt.Sscanf(typ[5:], "%d", &size)
		return "0x" + topic[:size*2], nil

	default:
		// For dynamic types (bytes, string, arrays), topic contains keccak256 hash
		return "0x" + topic, nil
	}
}

// FourByteDB provides 4byte.directory signature lookup.
type FourByteDB struct {
	signatures map[string][]string // selector -> list of possible signatures
}

// NewFourByteDB creates a new 4byte database.
func NewFourByteDB() *FourByteDB {
	return &FourByteDB{
		signatures: make(map[string][]string),
	}
}

// AddSignature adds a function signature.
func (db *FourByteDB) AddSignature(sig string) {
	selector := selectorFromSignature(sig)
	db.signatures[selector] = append(db.signatures[selector], sig)
}

// LookupSelector returns possible signatures for a selector.
func (db *FourByteDB) LookupSelector(selector string) []string {
	selector = strings.ToLower(selector)
	return db.signatures[selector]
}

// AddFromABI adds all function signatures from an ABI.
func (db *FourByteDB) AddFromABI(abiJSON json.RawMessage) error {
	methods, events, err := ParseABI(abiJSON)
	if err != nil {
		return err
	}

	for _, m := range methods {
		db.AddSignature(m.Signature)
	}
	for _, e := range events {
		db.AddSignature(e.Signature)
	}

	return nil
}

// MultiABIDecoder combines multiple ABIs for decoding.
type MultiABIDecoder struct {
	decoders []*ABIDecoder
}

// NewMultiABIDecoder creates a decoder from multiple ABIs.
func NewMultiABIDecoder(abis ...json.RawMessage) (*MultiABIDecoder, error) {
	var decoders []*ABIDecoder

	for _, abi := range abis {
		if len(abi) == 0 {
			continue
		}
		decoder, err := NewABIDecoder(abi)
		if err != nil {
			continue // Skip invalid ABIs
		}
		decoders = append(decoders, decoder)
	}

	return &MultiABIDecoder{decoders: decoders}, nil
}

// DecodeCall tries to decode with all available ABIs.
func (m *MultiABIDecoder) DecodeCall(input string) (*DecodedCall, error) {
	var lastErr error
	for _, d := range m.decoders {
		result, err := d.DecodeCall(input)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("no matching function found")
}

// DecodeLog tries to decode with all available ABIs.
func (m *MultiABIDecoder) DecodeLog(topics []string, data string) (*DecodedLog, error) {
	var lastErr error
	for _, d := range m.decoders {
		result, err := d.DecodeLog(topics, data)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("no matching event found")
}

// EncodeCall encodes a function call.
func EncodeCall(method MethodSignature, args ...interface{}) (string, error) {
	if len(args) != len(method.Inputs) {
		return "", fmt.Errorf("expected %d arguments, got %d", len(method.Inputs), len(args))
	}

	data := method.Selector
	for i, input := range method.Inputs {
		encoded, err := encodeValue(input.Type, args[i])
		if err != nil {
			return "", fmt.Errorf("encode arg %d: %w", i, err)
		}
		data += encoded
	}

	return data, nil
}

// encodeValue encodes a value to ABI format.
func encodeValue(typ string, value interface{}) (string, error) {
	switch typ {
	case "address":
		addr, ok := value.(string)
		if !ok {
			return "", errors.New("address must be string")
		}
		addr = strings.TrimPrefix(strings.ToLower(addr), "0x")
		return strings.Repeat("0", 24) + addr, nil

	case "bool":
		b, ok := value.(bool)
		if !ok {
			return "", errors.New("bool must be boolean")
		}
		if b {
			return strings.Repeat("0", 63) + "1", nil
		}
		return strings.Repeat("0", 64), nil

	case "uint256", "int256":
		var n *big.Int
		switch v := value.(type) {
		case *big.Int:
			n = v
		case string:
			n = new(big.Int)
			n.SetString(v, 10)
		case int64:
			n = big.NewInt(v)
		default:
			return "", fmt.Errorf("cannot convert %T to integer", value)
		}
		hex := fmt.Sprintf("%064x", n)
		if len(hex) > 64 {
			hex = hex[len(hex)-64:]
		}
		return hex, nil

	default:
		return "", fmt.Errorf("unsupported type: %s", typ)
	}
}
