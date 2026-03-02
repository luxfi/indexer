//go:build cgo && rustffi

// Package rustffi provides Go bindings to the Rust verification services via CGO.
//
// Build with CGO_ENABLED=1 and link against libexplorer_ffi.a:
//
//	CGO_LDFLAGS="-L/path/to/lib -lexplorer_ffi" go build
//
// Without the Rust static library, the pure Go fallbacks in the indexer's
// evm/contracts package are used instead.
package rustffi

// #cgo LDFLAGS: -lexplorer_ffi -ldl -lm -lpthread
// #include "explorer_ffi.h"
// #include <stdlib.h>
import "C"
import (
	"encoding/json"
	"fmt"
	"unsafe"
)

// VerifySolidity verifies a Solidity smart contract using the Rust verifier.
func VerifySolidity(sourceCode, compilerVersion, contractName string, optimization bool, optimizationRuns int, evmVersion, bytecode string) (json.RawMessage, error) {
	cSource := C.CString(sourceCode)
	cVersion := C.CString(compilerVersion)
	cName := C.CString(contractName)
	cBytecode := C.CString(bytecode)
	defer C.free(unsafe.Pointer(cSource))
	defer C.free(unsafe.Pointer(cVersion))
	defer C.free(unsafe.Pointer(cName))
	defer C.free(unsafe.Pointer(cBytecode))

	opt := C.int(0)
	if optimization {
		opt = 1
	}

	var cEVM *C.char
	if evmVersion != "" {
		cEVM = C.CString(evmVersion)
		defer C.free(unsafe.Pointer(cEVM))
	}

	var outJSON *C.char
	rc := C.explorer_verify_solidity(cSource, cVersion, cName, opt, C.int(optimizationRuns), cEVM, cBytecode, &outJSON)
	if outJSON != nil {
		defer C.explorer_free(outJSON)
	}

	if rc != 0 {
		msg := C.GoString(outJSON)
		return nil, fmt.Errorf("rust verifier: %s", msg)
	}

	return json.RawMessage(C.GoString(outJSON)), nil
}

// VerifyVyper verifies a Vyper smart contract using the Rust verifier.
func VerifyVyper(sourceCode, compilerVersion, contractName, bytecode string) (json.RawMessage, error) {
	cSource := C.CString(sourceCode)
	cVersion := C.CString(compilerVersion)
	cName := C.CString(contractName)
	cBytecode := C.CString(bytecode)
	defer C.free(unsafe.Pointer(cSource))
	defer C.free(unsafe.Pointer(cVersion))
	defer C.free(unsafe.Pointer(cName))
	defer C.free(unsafe.Pointer(cBytecode))

	var outJSON *C.char
	rc := C.explorer_verify_vyper(cSource, cVersion, cName, cBytecode, &outJSON)
	if outJSON != nil {
		defer C.explorer_free(outJSON)
	}

	if rc != 0 {
		return nil, fmt.Errorf("rust vyper verifier: %s", C.GoString(outJSON))
	}

	return json.RawMessage(C.GoString(outJSON)), nil
}

// VerifyStandardJSON verifies using Standard JSON input.
func VerifyStandardJSON(standardJSON, compilerVersion, bytecode string) (json.RawMessage, error) {
	cJSON := C.CString(standardJSON)
	cVersion := C.CString(compilerVersion)
	cBytecode := C.CString(bytecode)
	defer C.free(unsafe.Pointer(cJSON))
	defer C.free(unsafe.Pointer(cVersion))
	defer C.free(unsafe.Pointer(cBytecode))

	var outJSON *C.char
	rc := C.explorer_verify_standard_json(cJSON, cVersion, cBytecode, &outJSON)
	if outJSON != nil {
		defer C.explorer_free(outJSON)
	}

	if rc != 0 {
		return nil, fmt.Errorf("rust standard json verifier: %s", C.GoString(outJSON))
	}

	return json.RawMessage(C.GoString(outJSON)), nil
}

// LookupSignature looks up function signatures by 4-byte selector.
func LookupSignature(selector string) ([]string, error) {
	cSel := C.CString(selector)
	defer C.free(unsafe.Pointer(cSel))

	var outJSON *C.char
	rc := C.explorer_lookup_signature(cSel, &outJSON)
	if outJSON != nil {
		defer C.explorer_free(outJSON)
	}

	if rc < 0 {
		return nil, fmt.Errorf("rust sig lookup: %s", C.GoString(outJSON))
	}

	var sigs []string
	if err := json.Unmarshal([]byte(C.GoString(outJSON)), &sigs); err != nil {
		return nil, err
	}
	return sigs, nil
}

// LookupEvent looks up event signatures by topic0 hash.
func LookupEvent(topic0 string) ([]string, error) {
	cTopic := C.CString(topic0)
	defer C.free(unsafe.Pointer(cTopic))

	var outJSON *C.char
	rc := C.explorer_lookup_event(cTopic, &outJSON)
	if outJSON != nil {
		defer C.explorer_free(outJSON)
	}

	if rc < 0 {
		return nil, fmt.Errorf("rust event lookup: %s", C.GoString(outJSON))
	}

	var events []string
	if err := json.Unmarshal([]byte(C.GoString(outJSON)), &events); err != nil {
		return nil, err
	}
	return events, nil
}

// FFIVersion returns the version of the linked Rust FFI library.
func FFIVersion() string {
	ptr := C.explorer_ffi_version()
	if ptr != nil {
		defer C.explorer_free(ptr)
		return C.GoString(ptr)
	}
	return "unknown"
}
