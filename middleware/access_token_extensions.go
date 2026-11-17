package middleware

import "fmt"

// DeclareSessionOnlyAccessTokenRoute extends the same route registry used by
// native dashboard permissions; every PAT, including legacy PATs, is rejected.
func DeclareSessionOnlyAccessTokenRoute(method, fullPath string) {
	declareExtensionAccessTokenRoute(method, fullPath, accessTokenSessionRule)
}

// DeclareAnyAccessTokenRoute accepts every valid scoped PAT, the rule the
// native catalog gives GET /api/pricing; the route's own middleware decides.
func DeclareAnyAccessTokenRoute(method, fullPath string) {
	declareExtensionAccessTokenRoute(method, fullPath, accessTokenAnyRule)
}

func declareExtensionAccessTokenRoute(method, fullPath string, rule accessTokenRouteRule) {
	key := method + " " + fullPath
	if existing, ok := accessTokenRouteRules[key]; ok {
		if existing != rule {
			panic(fmt.Sprintf("conflicting access token route %s", key))
		}
		return
	}
	permissionRouteRulesMu.Lock()
	defer permissionRouteRulesMu.Unlock()
	if existing, ok := permissionRouteRules[key]; ok && existing != rule {
		panic(fmt.Sprintf("conflicting access token route %s", key))
	}
	permissionRouteRules[key] = rule
}
