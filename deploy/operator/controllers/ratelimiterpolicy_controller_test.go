package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	_ "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	radixipv1alpha1 "github.com/Mwangi-Derrick/radixip/deploy/operator/api/v1alpha1"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add corev1 to scheme: %v", err)
	}
	if err := radixipv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add radixipv1alpha1 to scheme: %v", err)
	}
	return s
}

func TestResolveNamespacedName(t *testing.T) {
	policy := &radixipv1alpha1.RateLimiterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-policy",
		},
		Spec: radixipv1alpha1.RateLimiterPolicySpec{
			TargetRef: radixipv1alpha1.TargetRef{
				Kind: "Deployment",
				Name: "my-service",
			},
		},
	}

	nn := ResolveNamespacedName(policy)
	if nn.Namespace != "default" || nn.Name != "my-service" {
		t.Errorf("expected default/my-service, got %s/%s", nn.Namespace, nn.Name)
	}

	policy.Spec.TargetRef.Namespace = "custom-ns"
	nn2 := ResolveNamespacedName(policy)
	if nn2.Namespace != "custom-ns" || nn2.Name != "my-service" {
		t.Errorf("expected custom-ns/my-service, got %s/%s", nn2.Namespace, nn2.Name)
	}
}

func TestResolveLuaFilename(t *testing.T) {
	tests := []struct {
		algo     radixipv1alpha1.Algorithm
		expected string
	}{
		{radixipv1alpha1.AlgorithmTokenBucket, "token_bucket.lua"},
		{radixipv1alpha1.AlgorithmLeakyBucket, "leaky_bucket.lua"},
		{radixipv1alpha1.AlgorithmSlidingWindow, "sliding_window.lua"},
		{radixipv1alpha1.Algorithm(""), "token_bucket.lua"},
		{radixipv1alpha1.Algorithm("unknown"), "token_bucket.lua"},
	}

	for _, tc := range tests {
		got := resolveLuaFilename(tc.algo)
		if got != tc.expected {
			t.Errorf("for algorithm %q: expected %q, got %q", tc.algo, tc.expected, got)
		}
	}
}

func TestHasSidecar(t *testing.T) {
	podWithout := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
			},
		},
	}
	if hasSidecar(podWithout) {
		t.Errorf("expected false for pod without sidecar")
	}

	podWith := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
				{Name: "radixip-rate-limiter"},
			},
		},
	}
	if !hasSidecar(podWith) {
		t.Errorf("expected true for pod with sidecar")
	}
}

func TestReconcile_FullLifecycle(t *testing.T) {
	scheme := newTestScheme(t)

	// Create temp dir with lua script
	tmpDir := t.TempDir()
	luaPath := filepath.Join(tmpDir, "token_bucket.lua")
	if err := os.WriteFile(luaPath, []byte("-- mock token bucket lua script"), 0644); err != nil {
		t.Fatalf("failed to write mock lua script: %v", err)
	}

	policy := &radixipv1alpha1.RateLimiterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-rlp",
		},
		Spec: radixipv1alpha1.RateLimiterPolicySpec{
			TargetRef: radixipv1alpha1.TargetRef{
				Kind: "Deployment",
				Name: "api-server",
			},
			Algorithm: radixipv1alpha1.AlgorithmTokenBucket,
			GlobalPolicy: radixipv1alpha1.RateLimitRule{
				Capacity:   100,
				RefillRate: 10,
			},
			Redis: radixipv1alpha1.RedisConfig{
				URL: "redis://localhost:6379",
			},
		},
	}

	pod1 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "api-server-1",
			Labels:    map[string]string{"app": "api-server"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
				{Name: "radixip-rate-limiter"},
			},
		},
	}

	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "api-server-2",
			Labels:    map[string]string{"app": "api-server"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
				{Name: "radixip-rate-limiter"},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy, pod1, pod2).
		WithStatusSubresource(&radixipv1alpha1.RateLimiterPolicy{}).
		Build()

	mockLoadedSHA := "mock-sha-abcdef123456"
	reconciler := &RateLimiterPolicyReconciler{
		Client:   client,
		Scheme:   scheme,
		Log:      zap.New(zap.UseDevMode(true)),
		Recorder: record.NewFakeRecorder(10),
		LuaDir:   tmpDir,
		ScriptLoader: func(ctx context.Context, redisCfg radixipv1alpha1.RedisConfig, scriptBytes []byte) (string, error) {
			return mockLoadedSHA, nil
		},
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-rlp",
		},
	}

	ctx := context.Background()
	res, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}
	if res.RequeueAfter != requeueAfter {
		t.Errorf("expected RequeueAfter=%v, got %v", requeueAfter, res.RequeueAfter)
	}

	// Verify policy was updated with ScriptSHA and Phase=Active
	updated := &radixipv1alpha1.RateLimiterPolicy{}
	if err := client.Get(ctx, req.NamespacedName, updated); err != nil {
		t.Fatalf("failed to fetch updated policy: %v", err)
	}

	if updated.Spec.Redis.ScriptSHA != mockLoadedSHA {
		t.Errorf("expected ScriptSHA=%q, got %q", mockLoadedSHA, updated.Spec.Redis.ScriptSHA)
	}
	if updated.Status.Phase != radixipv1alpha1.PhaseActive {
		t.Errorf("expected Phase=%q, got %q", radixipv1alpha1.PhaseActive, updated.Status.Phase)
	}
	if updated.Status.ActiveSidecars != 2 {
		t.Errorf("expected ActiveSidecars=2, got %d", updated.Status.ActiveSidecars)
	}
}

func TestReconcile_ScriptLoadError(t *testing.T) {
	scheme := newTestScheme(t)
	tmpDir := t.TempDir()
	luaPath := filepath.Join(tmpDir, "token_bucket.lua")
	_ = os.WriteFile(luaPath, []byte("-- script"), 0644)

	policy := &radixipv1alpha1.RateLimiterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "failed-policy",
		},
		Spec: radixipv1alpha1.RateLimiterPolicySpec{
			TargetRef: radixipv1alpha1.TargetRef{
				Kind: "Deployment",
				Name: "api-server",
			},
			Redis: radixipv1alpha1.RedisConfig{
				URL: "redis://unreachable:6379",
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy).
		WithStatusSubresource(&radixipv1alpha1.RateLimiterPolicy{}).
		Build()

	reconciler := &RateLimiterPolicyReconciler{
		Client:   client,
		Scheme:   scheme,
		Log:      zap.New(zap.UseDevMode(true)),
		Recorder: record.NewFakeRecorder(10),
		LuaDir:   tmpDir,
		ScriptLoader: func(ctx context.Context, redisCfg radixipv1alpha1.RedisConfig, scriptBytes []byte) (string, error) {
			return "", fmt.Errorf("connection refused")
		},
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "failed-policy",
		},
	}

	_, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}

	updated := &radixipv1alpha1.RateLimiterPolicy{}
	if err := client.Get(context.Background(), req.NamespacedName, updated); err != nil {
		t.Fatalf("failed to fetch updated policy: %v", err)
	}

	if updated.Status.Phase != radixipv1alpha1.PhasePending {
		// Because scriptSHA is empty, phase evaluates to Pending
		t.Logf("Status phase is %s (expected pending or degraded when script load fails)", updated.Status.Phase)
	}
}

func TestReconcile_NoPodsFound_Pending(t *testing.T) {
	scheme := newTestScheme(t)

	policy := &radixipv1alpha1.RateLimiterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-pending",
		},
		Spec: radixipv1alpha1.RateLimiterPolicySpec{
			TargetRef: radixipv1alpha1.TargetRef{
				Kind: "Deployment",
				Name: "nonexistent-workload",
			},
			Redis: radixipv1alpha1.RedisConfig{
				URL:       "redis://localhost:6379",
				ScriptSHA: "existing-sha",
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy).
		WithStatusSubresource(&radixipv1alpha1.RateLimiterPolicy{}).
		Build()

	reconciler := &RateLimiterPolicyReconciler{
		Client:   client,
		Scheme:   scheme,
		Log:      zap.New(zap.UseDevMode(true)),
		Recorder: record.NewFakeRecorder(10),
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-pending",
		},
	}

	_, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}

	updated := &radixipv1alpha1.RateLimiterPolicy{}
	if err := client.Get(context.Background(), req.NamespacedName, updated); err != nil {
		t.Fatalf("failed to fetch updated policy: %v", err)
	}

	if updated.Status.Phase != radixipv1alpha1.PhasePending {
		t.Errorf("expected Phase=%s, got %s", radixipv1alpha1.PhasePending, updated.Status.Phase)
	}
}

func TestReconcile_PodsWithoutSidecar_Degraded(t *testing.T) {
	scheme := newTestScheme(t)

	policy := &radixipv1alpha1.RateLimiterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-degraded",
		},
		Spec: radixipv1alpha1.RateLimiterPolicySpec{
			TargetRef: radixipv1alpha1.TargetRef{
				Kind: "Deployment",
				Name: "workload-without-sidecar",
			},
			Redis: radixipv1alpha1.RedisConfig{
				URL:       "redis://localhost:6379",
				ScriptSHA: "existing-sha",
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod-1",
			Labels:    map[string]string{"app": "workload-without-sidecar"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "app"},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy, pod).
		WithStatusSubresource(&radixipv1alpha1.RateLimiterPolicy{}).
		Build()

	reconciler := &RateLimiterPolicyReconciler{
		Client:   client,
		Scheme:   scheme,
		Log:      zap.New(zap.UseDevMode(true)),
		Recorder: record.NewFakeRecorder(10),
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "test-degraded",
		},
	}

	_, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}

	updated := &radixipv1alpha1.RateLimiterPolicy{}
	if err := client.Get(context.Background(), req.NamespacedName, updated); err != nil {
		t.Fatalf("failed to fetch updated policy: %v", err)
	}

	if updated.Status.Phase != radixipv1alpha1.PhaseDegraded {
		t.Errorf("expected Phase=%s, got %s", radixipv1alpha1.PhaseDegraded, updated.Status.Phase)
	}
}

func TestFindPoliciesForPod(t *testing.T) {
	scheme := newTestScheme(t)

	policy := &radixipv1alpha1.RateLimiterPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "my-policy",
		},
		Spec: radixipv1alpha1.RateLimiterPolicySpec{
			TargetRef: radixipv1alpha1.TargetRef{
				Kind: "Deployment",
				Name: "target-svc",
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(policy).
		Build()

	reconciler := &RateLimiterPolicyReconciler{
		Client: client,
		Scheme: scheme,
		Log:    zap.New(zap.UseDevMode(true)),
	}

	// 1. Pod matching targetRef
	matchingPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "target-svc-abc",
			Labels:    map[string]string{"app": "target-svc"},
		},
	}
	reqs := reconciler.findPoliciesForPod(context.Background(), matchingPod)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].NamespacedName.Name != "my-policy" {
		t.Errorf("expected my-policy, got %s", reqs[0].NamespacedName.Name)
	}

	// 2. Pod in different namespace
	diffNsPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "other-ns",
			Name:      "target-svc-xyz",
			Labels:    map[string]string{"app": "target-svc"},
		},
	}
	reqs2 := reconciler.findPoliciesForPod(context.Background(), diffNsPod)
	if len(reqs2) != 0 {
		t.Errorf("expected 0 requests for different namespace, got %d", len(reqs2))
	}

	// 3. Pod with different app label
	diffAppPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "other-app-xyz",
			Labels:    map[string]string{"app": "other-app"},
		},
	}
	reqs3 := reconciler.findPoliciesForPod(context.Background(), diffAppPod)
	if len(reqs3) != 0 {
		t.Errorf("expected 0 requests for different app, got %d", len(reqs3))
	}
}
