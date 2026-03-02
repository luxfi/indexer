/* explorer_ffi.h — C API for embedded Rust verification services */
#ifndef EXPLORER_FFI_H
#define EXPLORER_FFI_H

#ifdef __cplusplus
extern "C" {
#endif

/* Smart Contract Verifier */
int explorer_verify_solidity(
    const char *source_code,
    const char *compiler_version,
    const char *contract_name,
    int optimization,
    int optimization_runs,
    const char *evm_version,
    const char *bytecode,
    char **out_json
);

int explorer_verify_vyper(
    const char *source_code,
    const char *compiler_version,
    const char *contract_name,
    const char *bytecode,
    char **out_json
);

int explorer_verify_standard_json(
    const char *standard_json,
    const char *compiler_version,
    const char *bytecode,
    char **out_json
);

/* Signature Provider */
int explorer_lookup_signature(const char *selector, char **out_json);
int explorer_lookup_event(const char *topic0, char **out_json);

/* Memory management */
void explorer_free(char *ptr);
char *explorer_ffi_version(void);

#ifdef __cplusplus
}
#endif
#endif /* EXPLORER_FFI_H */
