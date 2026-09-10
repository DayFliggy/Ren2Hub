package router

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// Every router that mounts a generation or submission entry must release the
// user admission lease itself. Gin applies engine middleware only to routes
// registered after the Use call, so relying on SetRouter ordering would let a
// lease survive until its TTL whenever a router is mounted on its own, which
// the integration tests in this package do.
//
// The chain is matched by function name because Gin stores the middleware as a
// closure: calling the constructor again here yields a different code pointer
// once the closure is inlined.
func TestGenerationRoutersRegisterUserAdmissionCleanup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, test := range []struct {
		name  string
		mount func(*gin.Engine)
	}{
		{name: "relay router", mount: SetRelayRouter},
		{name: "video router", mount: SetVideoRouter},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			test.mount(engine)
			found := false
			for _, handler := range engine.Handlers {
				name := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
				if strings.Contains(name, "UserAdmissionCleanup") {
					found = true
					break
				}
			}
			assert.True(t, found, "router must register UserAdmissionCleanup for its own routes")
		})
	}
}
