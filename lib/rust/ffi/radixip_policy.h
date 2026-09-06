#ifndef RADIXIP_POLICY_H
#define RADIXIP_POLICY_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct RadixPolicyHandle RadixPolicyHandle;

typedef struct RadixIpValue {
    uint8_t family;
    uint8_t bytes[16];
} RadixIpValue;

typedef enum RadixPolicyDecision {
    RADIX_POLICY_ALLOW = 0,
    RADIX_POLICY_BLOCK = 1,
    RADIX_POLICY_LIMIT = 2,
    RADIX_POLICY_BAD_REQUEST = 3
} RadixPolicyDecision;

typedef struct RadixPolicyResult {
    RadixPolicyDecision decision;
    uint32_t retry_after_seconds;
} RadixPolicyResult;

/* error_code: 0 success, 1 null path, 2 invalid UTF-8, 3 config error, 4 runtime error */
RadixPolicyHandle* radix_policy_new_from_yaml(
    const char* config_path,
    int* error_code
);

/* return 0 success, 1 invalid pointer, 2 invalid address family */
int radix_policy_check(
    const RadixPolicyHandle* handle,
    const RadixIpValue* ip,
    RadixPolicyResult* result
);

void radix_policy_free(RadixPolicyHandle* handle);

#ifdef __cplusplus
}
#endif

#endif /* RADIXIP_POLICY_H */
