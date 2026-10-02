// Package controllers implements the controller-runtime reconcilers for the
// radixip.io API group.
//
// The RateLimiterPolicyReconciler watches RateLimiterPolicy CRs and:
//  1. Loads the Lua rate-limit script into Redis via SCRIPT LOAD → EVALSHA.
//  2. Lists pods for the target workload and counts active rate-limiter sidecars.
//  3. Manages CR status (Phase, ActiveSidecars, LastSyncTime) and emits Events.
//  4. Requeues every 30 seconds for liveness drift correction.
package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	radixipv1alpha1 "github.com/Mwangi-Derrick/radixip/deploy/operator/api/v1alpha1"
)

const (
	// requeueAfter is the reconciliation polling interval.
	// Keeps status fresh without hammering the API server.
	requeueAfter = 30 * time.Second

	// sidecarContainerName is the well-known name of the rate-limiter sidecar
	// container injected into target pods. Used to count active sidecars.
	sidecarContainerName = "radixip-rate-limiter"

	// defaultLuaScriptFilename is the token-bucket Lua script loaded into Redis.
	defaultLuaScriptFilename = "token_bucket.lua"

	// defaultLuaDir is the fallback mount path for Lua scripts inside the operator pod.
	// Override via env RADIXIP_LUA_DIR.
	defaultLuaDir = "/etc/radixip/lua"
)

// +kubebuilder:rbac:groups=radixip.io,resources=ratelimiterpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=radixip.io,resources=ratelimiterpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=radixip.io,resources=ratelimiterpolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// RateLimiterPolicyReconciler reconciles RateLimiterPolicy objects.
type RateLimiterPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Log      logr.Logger
	Recorder record.EventRecorder
}

// Reconcile is the main reconciliation loop. It is called by controller-runtime
// whenever a RateLimiterPolicy or an owned Pod changes.
func (r *RateLimiterPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("ratelimiterpolicy", req.NamespacedName)

	// ── 1. Fetch the CR ───────────────────────────────────────────────────────
	policy := &radixipv1alpha1.RateLimiterPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		if apierrors.IsNotFound(err) {
			// Object deleted — nothing to do.
			log.Info("RateLimiterPolicy not found; likely deleted — skipping")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to fetch RateLimiterPolicy")
		return ctrl.Result{}, err
	}

	// Keep a copy so we can detect status changes without a full diff.
	original := policy.DeepCopy()

	// ── 2. Load Lua EVALSHA script into Redis if missing ─────────────────────
	// The ScriptSHA is populated once and persisted on the CR spec.
	// Sidecars read it from the CR via the Downward API or a projected Secret.
	if policy.Spec.Redis.ScriptSHA == "" {
		sha, err := r.loadLuaScript(ctx, policy.Spec.Redis)
		if err != nil {
			log.Error(err, "Failed to load Lua rate-limit script into Redis")
			r.setStatus(policy,
				radixipv1alpha1.PhaseDegraded,
				fmt.Sprintf("Lua script load failed: %v", err),
				policy.Status.ActiveSidecars,
			)
			r.Recorder.Eventf(policy, corev1.EventTypeWarning, "ScriptLoadFailed",
				"Could not load Lua EVALSHA script: %v", err)
		} else {
			log.Info("Lua script loaded into Redis", "sha", sha)
			policy.Spec.Redis.ScriptSHA = sha

			// Persist ScriptSHA on the spec so sidecars can discover it.
			if updateErr := r.Update(ctx, policy); updateErr != nil {
				log.Error(updateErr, "Failed to persist ScriptSHA on CR spec")
				return ctrl.Result{}, updateErr
			}
			r.Recorder.Eventf(policy, corev1.EventTypeNormal, "ScriptLoaded",
				"Lua EVALSHA script registered: SHA=%s", sha)
		}
	}

	// ── 3. Resolve target namespace ───────────────────────────────────────────
	targetNS := policy.Spec.TargetRef.Namespace
	if targetNS == "" {
		targetNS = policy.Namespace
	}

	// ── 4. List pods for the target workload ──────────────────────────────────
	// Pods are matched by label app=<targetRef.name>.
	// TODO(phase-2): resolve the owning Deployment's pod-template selector
	// for more robust matching across arbitrary label schemes.
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(targetNS),
		client.MatchingLabels{"app": policy.Spec.TargetRef.Name},
	); err != nil {
		log.Error(err, "Failed to list target pods")
		return ctrl.Result{}, err
	}

	// ── 5. Count pods carrying the sidecar ───────────────────────────────────
	activeSidecars := int32(0)
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp != nil {
			continue // Skip terminating pods.
		}
		if hasSidecar(pod) {
			activeSidecars++
		}
	}
	log.Info("Sidecar inventory",
		"totalTargetPods", len(podList.Items),
		"activeSidecars", activeSidecars,
	)

	// ── 6. Determine new Phase ────────────────────────────────────────────────
	previousPhase := policy.Status.Phase

	var newPhase radixipv1alpha1.Phase
	var message string

	switch {
	case policy.Spec.Redis.ScriptSHA == "":
		newPhase = radixipv1alpha1.PhasePending
		message = "Waiting for Lua EVALSHA script to be loaded into Redis"

	case len(podList.Items) == 0:
		newPhase = radixipv1alpha1.PhasePending
		message = fmt.Sprintf("No pods found for target %s/%s",
			targetNS, policy.Spec.TargetRef.Name)

	case activeSidecars == 0:
		newPhase = radixipv1alpha1.PhaseDegraded
		message = fmt.Sprintf(
			"Target has %d pod(s) but none carry the '%s' sidecar yet",
			len(podList.Items), sidecarContainerName)

	default:
		newPhase = radixipv1alpha1.PhaseActive
		message = fmt.Sprintf("Rate limiter active on %d/%d pod(s)",
			activeSidecars, len(podList.Items))
	}

	r.setStatus(policy, newPhase, message, activeSidecars)

	// Emit an Event on phase transitions for observability.
	if previousPhase != newPhase {
		evtType := corev1.EventTypeNormal
		if newPhase == radixipv1alpha1.PhaseDegraded || newPhase == radixipv1alpha1.PhaseFailed {
			evtType = corev1.EventTypeWarning
		}
		r.Recorder.Eventf(policy, evtType, "PhaseTransition",
			"Phase changed %s → %s: %s", previousPhase, newPhase, message)
		log.Info("Phase transition", "from", previousPhase, "to", newPhase)
	}

	// ── 7. Persist status if anything changed ────────────────────────────────
	if statusChanged(original.Status, policy.Status) {
		if err := r.Status().Update(ctx, policy); err != nil {
			if apierrors.IsConflict(err) {
				// Stale resource version — requeue immediately.
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "Failed to update RateLimiterPolicy status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// setStatus writes the given phase, message, and active sidecar count into the
// policy's Status, also updating ObservedGeneration and LastSyncTime.
func (r *RateLimiterPolicyReconciler) setStatus(
	policy *radixipv1alpha1.RateLimiterPolicy,
	phase radixipv1alpha1.Phase,
	message string,
	activeSidecars int32,
) {
	now := metav1.Now()
	policy.Status.Phase = phase
	policy.Status.Message = message
	policy.Status.ActiveSidecars = activeSidecars
	policy.Status.ObservedGeneration = policy.Generation
	policy.Status.LastSyncTime = &now
}

// statusChanged returns true if any observable status field has changed.
// This avoids unnecessary API server writes.
func statusChanged(a, b radixipv1alpha1.RateLimiterPolicyStatus) bool {
	return a.Phase != b.Phase ||
		a.Message != b.Message ||
		a.ActiveSidecars != b.ActiveSidecars ||
		a.ObservedGeneration != b.ObservedGeneration
}

// hasSidecar returns true if the pod has a container named sidecarContainerName.
func hasSidecar(pod *corev1.Pod) bool {
	for _, c := range pod.Spec.Containers {
		if c.Name == sidecarContainerName {
			return true
		}
	}
	return false
}

// resolveNamespacedName builds a NamespacedName from a TargetRef, defaulting
// the namespace to the owning policy's namespace.
// Exported for use in unit tests.
func ResolveNamespacedName(policy *radixipv1alpha1.RateLimiterPolicy) types.NamespacedName {
	ns := policy.Spec.TargetRef.Namespace
	if ns == "" {
		ns = policy.Namespace
	}
	return types.NamespacedName{Namespace: ns, Name: policy.Spec.TargetRef.Name}
}

// loadLuaScript reads the token-bucket Lua script from disk and loads it into
// Redis using SCRIPT LOAD, returning the EVALSHA SHA1.
//
// The Lua script path is resolved from:
//  1. env RADIXIP_LUA_DIR / token_bucket.lua
//  2. defaultLuaDir / token_bucket.lua
//
// Redis connectivity uses the RedisConfig from the CR spec.
// The real go-redis implementation is wired in once `go mod tidy` is run.
func (r *RateLimiterPolicyReconciler) loadLuaScript(
	ctx context.Context,
	redisCfg radixipv1alpha1.RedisConfig,
) (string, error) {
	luaDir := os.Getenv("RADIXIP_LUA_DIR")
	if luaDir == "" {
		luaDir = defaultLuaDir
	}
	scriptPath := filepath.Join(luaDir, defaultLuaScriptFilename)

	scriptBytes, err := os.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("reading Lua script %q: %w", scriptPath, err)
	}

	// ── Redis SCRIPT LOAD ──────────────────────────────────────────────────
	// Real implementation (uncomment after go mod tidy):
	//
	//   opt, err := redis.ParseURL(redisCfg.URL)
	//   if err != nil {
	//       return "", fmt.Errorf("parsing Redis URL: %w", err)
	//   }
	//   if redisCfg.PoolSize > 0 {
	//       opt.PoolSize = redisCfg.PoolSize
	//   }
	//   rdb := redis.NewClient(opt)
	//   defer rdb.Close()
	//
	//   sha, err := rdb.ScriptLoad(ctx, string(scriptBytes)).Result()
	//   if err != nil {
	//       return "", fmt.Errorf("SCRIPT LOAD: %w", err)
	//   }
	//   return sha, nil
	//
	// Stub: returns a deterministic placeholder so the operator compiles and
	// runs in dry-run / no-Redis mode for local development.
	_ = scriptBytes
	_ = ctx
	return "", fmt.Errorf(
		"loadLuaScript: Redis client stub — set spec.redis.scriptSHA manually "+
			"or wire in go-redis after 'go mod tidy'; url=%s", redisCfg.URL,
	)
}

// SetupWithManager registers the reconciler with the controller-runtime Manager
// and sets up ownership watches on Pods so that pod state changes trigger
// RateLimiterPolicy reconciliation.
func (r *RateLimiterPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("ratelimiterpolicy-controller")
	return ctrl.NewControllerManagedBy(mgr).
		For(&radixipv1alpha1.RateLimiterPolicy{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
