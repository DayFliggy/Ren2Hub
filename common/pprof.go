package common

import (
	"context"
	"fmt"
	"os"
	"runtime/pprof"
	"time"

	"github.com/shirou/gopsutil/cpu"
)

// MonitorWithContext periodically captures CPU profiles while load is high.
func MonitorWithContext(ctx context.Context) {
	for {
		percent, err := cpu.Percent(time.Second, false)
		if err != nil {
			SysLog(LogText("read cpu usage failed: %s", err.Error()))
			if !WaitBackgroundInterval(ctx, 30*time.Second) {
				return
			}
			continue
		}
		if len(percent) > 0 && percent[0] > 80 {
			fmt.Println(LogText("cpu usage too high"))
			if _, err := os.Stat("./pprof"); os.IsNotExist(err) {
				if err := os.Mkdir("./pprof", os.ModePerm); err != nil {
					SysLog(LogText("failed to create the pprof directory: %s", err.Error()))
					continue
				}
			}
			f, err := os.Create("./pprof/" + fmt.Sprintf("cpu-%s.pprof", time.Now().Format("20060102150405")))
			if err != nil {
				SysLog(LogText("failed to create the pprof file: %s", err.Error()))
				continue
			}
			if err := pprof.StartCPUProfile(f); err != nil {
				SysLog(LogText("failed to start pprof: %s", err.Error()))
				_ = f.Close()
				continue
			}
			if !WaitBackgroundInterval(ctx, 10*time.Second) {
				pprof.StopCPUProfile()
				_ = f.Close()
				return
			}
			pprof.StopCPUProfile()
			_ = f.Close()
		}
		if !WaitBackgroundInterval(ctx, 30*time.Second) {
			return
		}
	}
}
