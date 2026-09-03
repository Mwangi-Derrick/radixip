package policy

import (
	"strings"

	"github.com/Mwangi-Derrick/radixip/lib/go/config"
)

type RouteTrieNode struct {
	children       map[string]*RouteTrieNode
	methodLimiters map[string]*TokenBucketLimiter
}

func NewRouteTrie() *RouteTrieNode {
	return &RouteTrieNode{
		children:       make(map[string]*RouteTrieNode),
		methodLimiters: make(map[string]*TokenBucketLimiter),
	}
}

func (t *RouteTrieNode) AddRoute(path string, method string, rateLimitConfig config.RateLimitConfig) {
	// Validate and normalize inputs
	if path == "" || method == "" {
		return // Skip invalid routes
	}

	// Remove leading slash for consistent parsing
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}

	// Normalize method to uppercase
	method = strings.ToUpper(method)

	// Parse path segments
	segments := strings.Split(path, "/")
	currentNode := t

	// Traverse or create nodes for each segment
	for _, segment := range segments {
		if segment == "" {
			continue // Skip empty segments
		}

		// Check if child node exists
		if _, exists := currentNode.children[segment]; !exists {
			// Create new child node
			currentNode.children[segment] = &RouteTrieNode{
				children:       make(map[string]*RouteTrieNode),
				methodLimiters: make(map[string]*TokenBucketLimiter),
			}
		}

		// Move to child node
		currentNode = currentNode.children[segment]
	}

	// Create token bucket for this specific method
	bucket := NewTokenBucketLimiter(
		uint64(rateLimitConfig.Capacity),
		uint64(rateLimitConfig.RefillRate),
		uint32(rateLimitConfig.TTLSeconds),
		uint64(rateLimitConfig.MaxBuckets),
	)
	currentNode.methodLimiters[method] = bucket
}

func (t *RouteTrieNode) Match(path string, method string) *TokenBucketLimiter {
	// Remove leading slash for consistent parsing.
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}

	// Normalize method to uppercase.
	method = strings.ToUpper(method)

	// Parse path segments.
	segments := strings.Split(path, "/")
	currentNode := t
	var lastWildcardNode *RouteTrieNode // best wildcard match seen so far

	for _, segment := range segments {
		if segment == "" {
			continue
		}

		// Check for an exact-segment child first (more specific wins).
		if child, exists := currentNode.children[segment]; exists {
			currentNode = child
		} else if wildcard, exists := currentNode.children["*"]; exists {
			// Wildcard matches any remaining path — record as fallback but keep traversing.
			lastWildcardNode = wildcard
			currentNode = wildcard
		} else {
			// Dead end: fall back to the most-recent wildcard match.
			if lastWildcardNode != nil {
				return lastWildcardNode.methodLimiters[method]
			}
			return nil
		}
	}

	// Prefer the exact terminal node's limiter; fall back to wildcard.
	if lim := currentNode.methodLimiters[method]; lim != nil {
		return lim
	}
	if lastWildcardNode != nil {
		return lastWildcardNode.methodLimiters[method]
	}
	return nil
}

func (t *RouteTrieNode) HasRoute(path string, method string) bool {
	// Remove leading slash for consistent parsing
	if len(path) > 0 && path[0] == '/' {
		path = path[1:]
	}

	// Normalize method to uppercase
	method = strings.ToUpper(method)

	// Parse path segments
	segments := strings.Split(path, "/")
	currentNode := t

	// Traverse the trie
	for _, segment := range segments {
		if segment == "" {
			continue // Skip empty segments
		}

		if node, exists := currentNode.children[segment]; exists {
			currentNode = node
		} else {
			// No matching route found
			return false
		}
	}

	// Return the token bucket for this specific method
	return currentNode.methodLimiters[method] != nil
}

func (t *RouteTrieNode) MatchMethod(method string) *TokenBucketLimiter {
	// Normalize method to uppercase
	method = strings.ToUpper(method)

	// Return the token bucket for this specific method
	return t.methodLimiters[method]
}
