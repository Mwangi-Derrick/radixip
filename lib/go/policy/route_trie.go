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

func (t *RouteTrieNode) AddRoute(path string, method string, rateLimitConfig config.TokenBucketConfig) {
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
	bucket := NewTokenBucketLimiter(rateLimitConfig)
	currentNode.methodLimiters[method] = bucket
}

func (t *RouteTrieNode) Match(path string, method string) *TokenBucketLimiter {

}
