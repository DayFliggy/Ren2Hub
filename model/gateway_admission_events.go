package model

import "sync"

var admissionChangeHook struct {
	sync.RWMutex
	fn func(string, ...int)
}

func SetGatewayAdmissionChangeHook(fn func(string, ...int)) {
	admissionChangeHook.Lock()
	admissionChangeHook.fn = fn
	admissionChangeHook.Unlock()
}

// NotifyGatewayAdmissionChanged is called only after the owning transaction commits.
func NotifyGatewayAdmissionChanged(kind string, ids ...int) {
	if kind != "config" && len(ids) == 0 {
		return
	}
	admissionChangeHook.RLock()
	fn := admissionChangeHook.fn
	admissionChangeHook.RUnlock()
	if fn != nil {
		fn(kind, ids...)
	}
}
