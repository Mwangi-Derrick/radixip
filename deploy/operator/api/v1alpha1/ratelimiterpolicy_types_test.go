package v1alpha1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme failed: %v", err)
	}

	types := scheme.KnownTypes(GroupVersion)
	if _, ok := types["RateLimiterPolicy"]; !ok {
		t.Errorf("expected RateLimiterPolicy to be registered in scheme")
	}
	if _, ok := types["RateLimiterPolicyList"]; !ok {
		t.Errorf("expected RateLimiterPolicyList to be registered in scheme")
	}
}

func TestDeepCopy(t *testing.T) {
	now := metav1.NewTime(time.Now())
	original := &RateLimiterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "default",
			Labels:    map[string]string{"env": "test"},
		},
		Spec: RateLimiterPolicySpec{
			TargetRef: TargetRef{
				Kind:      "Deployment",
				Name:      "api",
				Namespace: "default",
			},
			Algorithm: AlgorithmSlidingWindow,
			GlobalPolicy: RateLimitRule{
				Capacity:   500,
				RefillRate: 50,
				WindowSecs: 60,
			},
			MethodPolicies: []MethodPolicy{
				{
					Method: "/service/MethodA",
					Rule: RateLimitRule{
						Capacity:   10,
						RefillRate: 1,
					},
				},
			},
			Redis: RedisConfig{
				URL:       "redis://redis:6379",
				ScriptSHA: "abc123",
				PoolSize:  10,
			},
			FailureMode: FailureModeOpen,
			TTLSecs:     120,
		},
		Status: RateLimiterPolicyStatus{
			Phase:              PhaseActive,
			Message:            "all good",
			ObservedGeneration: 1,
			LastSyncTime:       &now,
			ActiveSidecars:     3,
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatalf("expected non-nil copy")
	}

	// Verify deep copy fields
	if copied.Name != original.Name || copied.Namespace != original.Namespace {
		t.Errorf("mismatched metadata")
	}
	if copied.Spec.Algorithm != original.Spec.Algorithm {
		t.Errorf("mismatched algorithm")
	}
	if len(copied.Spec.MethodPolicies) != len(original.Spec.MethodPolicies) {
		t.Errorf("mismatched method policies length")
	}
	if copied.Status.LastSyncTime.Time != original.Status.LastSyncTime.Time {
		t.Errorf("mismatched lastSyncTime")
	}

	// Verify mutations on copy do not affect original
	copied.Spec.MethodPolicies[0].Method = "/service/Modified"
	if original.Spec.MethodPolicies[0].Method == "/service/Modified" {
		t.Errorf("expected slice mutation not to affect original")
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Errorf("expected DeepCopyObject to return non-nil runtime.Object")
	}

	// Test nil receiver
	var nilPolicy *RateLimiterPolicy
	if nilPolicy.DeepCopy() != nil {
		t.Errorf("expected nil for nil receiver DeepCopy")
	}
	if nilPolicy.DeepCopyObject() != nil {
		t.Errorf("expected nil for nil receiver DeepCopyObject")
	}
}

func TestRateLimiterPolicyList_DeepCopy(t *testing.T) {
	list := &RateLimiterPolicyList{
		Items: []RateLimiterPolicy{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "item1"},
			},
		},
	}

	copiedList := list.DeepCopy()
	if len(copiedList.Items) != 1 || copiedList.Items[0].Name != "item1" {
		t.Errorf("list deepcopy mismatch")
	}

	obj := list.DeepCopyObject()
	if obj == nil {
		t.Errorf("expected runtime.Object from list")
	}

	var nilList *RateLimiterPolicyList
	if nilList.DeepCopy() != nil || nilList.DeepCopyObject() != nil {
		t.Errorf("expected nil for nil list copy")
	}
}
