// Package v1alpha1 contains API Schema definitions for the radixip.io v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=radixip.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Algorithm defines the rate-limiting algorithm to apply.
// +kubebuilder:validation:Enum=token_bucket;leaky_bucket;sliding_window
type Algorithm string

const (
	AlgorithmTokenBucket   Algorithm = "token_bucket"
	AlgorithmLeakyBucket   Algorithm = "leaky_bucket"
	AlgorithmSlidingWindow Algorithm = "sliding_window"
)

// FailureMode defines the behavior when the distributed rate-limit backend is unavailable.
// +kubebuilder:validation:Enum=local_fallback;open;closed
type FailureMode string

const (
	// FailureModeLocalFallback falls back to the in-process token bucket (no Redis).
	// Best of both worlds — availability wins, with local enforcement as safety net.
	FailureModeLocalFallback FailureMode = "local_fallback"
	// FailureModeOpen passes all requests through when Redis is down.
	FailureModeOpen FailureMode = "open"
	// FailureModeClosed rejects all requests when Redis is down.
	FailureModeClosed FailureMode = "closed"
)

// Phase represents the current lifecycle phase of the RateLimiterPolicy.
type Phase string

const (
	PhasePending  Phase = "Pending"
	PhaseActive   Phase = "Active"
	PhaseDegraded Phase = "Degraded"
	PhaseFailed   Phase = "Failed"
)

// TargetRef identifies the workload (Deployment, StatefulSet, DaemonSet) that the
// policy applies to. The operator matches pods via the label app=<Name>.
type TargetRef struct {
	// Kind is the target resource kind (Deployment, StatefulSet, DaemonSet).
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// Name of the target resource.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Namespace of the target resource. Defaults to the policy's namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// RateLimitRule holds the parameters for a single rate-limit bucket.
type RateLimitRule struct {
	// Capacity is the burst ceiling — maximum tokens (token bucket) or max water
	// level (leaky bucket). Must be ≥ 1.
	// +kubebuilder:validation:Minimum=1
	Capacity int64 `json:"capacity"`

	// RefillRate is tokens added per second (token bucket) or drops drained per
	// second (leaky bucket). Must be ≥ 1.
	// +kubebuilder:validation:Minimum=1
	RefillRate int64 `json:"refillRate"`

	// WindowSecs is the sliding window duration in seconds. Only used by the
	// sliding_window algorithm; ignored by token_bucket and leaky_bucket.
	// +optional
	// +kubebuilder:validation:Minimum=1
	WindowSecs int64 `json:"windowSecs,omitempty"`
}

// MethodPolicy applies a specific RateLimitRule to a single gRPC method,
// overriding the global policy for that method.
type MethodPolicy struct {
	// Method is the full gRPC method path, e.g. /radixip.v1.RadixService/Lookup.
	// +kubebuilder:validation:Required
	Method string `json:"method"`

	// Rule is the per-method rate-limit rule.
	Rule RateLimitRule `json:"rule"`
}

// RedisConfig holds the connection parameters for the distributed Redis backend.
type RedisConfig struct {
	// URL is the Redis connection string, e.g. redis://redis-master:6379.
	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// ScriptSHA is the EVALSHA SHA1 hash of the Lua rate-limit script loaded
	// into Redis via SCRIPT LOAD. Populated automatically by the operator.
	// +optional
	ScriptSHA string `json:"scriptSHA,omitempty"`

	// PoolSize controls the number of connections in the Redis connection pool.
	// +optional
	// +kubebuilder:validation:Minimum=1
	PoolSize int `json:"poolSize,omitempty"`
}

// RateLimiterPolicySpec defines the desired state of a RateLimiterPolicy.
type RateLimiterPolicySpec struct {
	// TargetRef identifies the workload that sidecars will be injected into.
	TargetRef TargetRef `json:"targetRef"`

	// Algorithm selects the rate-limiting algorithm.
	// Defaults to token_bucket.
	// +optional
	// +kubebuilder:default=token_bucket
	Algorithm Algorithm `json:"algorithm,omitempty"`

	// GlobalPolicy is the default rate-limit rule applied to all gRPC methods
	// not covered by MethodPolicies.
	GlobalPolicy RateLimitRule `json:"globalPolicy"`

	// MethodPolicies provide per-method overrides on top of GlobalPolicy.
	// +optional
	MethodPolicies []MethodPolicy `json:"methodPolicies,omitempty"`

	// Redis configures the distributed backend.
	Redis RedisConfig `json:"redis"`

	// FailureMode controls sidecar behavior when Redis is unreachable.
	// Defaults to local_fallback.
	// +optional
	// +kubebuilder:default=local_fallback
	FailureMode FailureMode `json:"failureMode,omitempty"`

	// TTLSecs sets the idle-bucket TTL in seconds. Buckets that have not seen
	// traffic for this long are automatically expired in Redis.
	// Defaults to 300 (5 minutes).
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=1
	TTLSecs int32 `json:"ttlSecs,omitempty"`
}

// RateLimiterPolicyStatus describes the observed state of a RateLimiterPolicy.
type RateLimiterPolicyStatus struct {
	// Phase is the current lifecycle state of the policy.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// Message is a human-readable description of the current Phase.
	// +optional
	Message string `json:"message,omitempty"`

	// ObservedGeneration is the .metadata.generation that was last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// LastSyncTime is the timestamp of the most recent successful reconciliation.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// ActiveSidecars is the number of target pods currently running the
	// rate-limiter sidecar container.
	// +optional
	ActiveSidecars int32 `json:"activeSidecars,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=rlp,categories=radixip
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase",description="Lifecycle phase"
// +kubebuilder:printcolumn:name="Algorithm",type=string,JSONPath=".spec.algorithm",description="Rate-limiting algorithm"
// +kubebuilder:printcolumn:name="ActiveSidecars",type=integer,JSONPath=".status.activeSidecars",description="Pods with active sidecar"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// RateLimiterPolicy configures distributed rate limiting for a gRPC workload.
// The operator reads this CR, loads the Lua rate-limit script into Redis, and
// tracks sidecar injection status across the target pods.
type RateLimiterPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RateLimiterPolicySpec   `json:"spec,omitempty"`
	Status RateLimiterPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RateLimiterPolicyList contains a list of RateLimiterPolicy resources.
type RateLimiterPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RateLimiterPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RateLimiterPolicy{}, &RateLimiterPolicyList{})
}
