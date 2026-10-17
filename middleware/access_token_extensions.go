package middleware

import "fmt"

// DeclareSessionOnlyAccessTokenRoute extends the same route registry used by
// native dashboard permissions; every PAT, including legacy PATs, is rejected.
func DeclareSessionOnlyAccessTokenRoute(method, fullPath string) {
	key := method + " " + fullPath
	if existing, ok := accessTokenRouteRules[key]; ok {
		if existing != accessTokenSessionRule {
			panic(fmt.Sprintf("conflicting access token route %s", key))
		}
		return
	}
	permissionRouteRulesMu.Lock()
	defer permissionRouteRulesMu.Unlock()
	if existing, ok := permissionRouteRules[key]; ok && existing != accessTokenSessionRule {
		panic(fmt.Sprintf("conflicting access token route %s", key))
	}
	permissionRouteRules[key] = accessTokenSessionRule
}
