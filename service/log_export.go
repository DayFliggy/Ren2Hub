package service

import "sync"

// The gateway admits traffic in one process; exports follow that same scope.
var logExportLocks sync.Map

func AcquireLogExportLock(userID int) (release func(), acquired bool) {
	if _, running := logExportLocks.LoadOrStore(userID, struct{}{}); running {
		return nil, false
	}
	var once sync.Once
	return func() { once.Do(func() { logExportLocks.Delete(userID) }) }, true
}
