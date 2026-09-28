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
			SysLog("read cpu usage failed: " + err.Error())
			if !WaitBackgroundInterval(ctx, 30*time.Second) {
				return
			}
			continue
		}
		if len(percent) > 0 && percent[0] > 80 {
			fmt.Println("cpu usage too high")
			if _, err := os.Stat("./pprof"); os.IsNotExist(err) {
				if err := os.Mkdir("./pprof", os.ModePerm); err != nil {
					SysLog("create pprof directory failed: " + err.Error())
					continue
				}
			}
			f, err := os.Create("./pprof/" + fmt.Sprintf("cpu-%s.pprof", time.Now().Format("20060102150405")))
			if err != nil {
				SysLog("create pprof file failed: " + err.Error())
				continue
			}
			if err := pprof.StartCPUProfile(f); err != nil {
				SysLog("start pprof failed: " + err.Error())
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

// Monitor 定时监控cpu使用率，超过阈值输出pprof文件
func Monitor() {
	for {
		percent, err := cpu.Percent(time.Second, false)
		if err != nil {
			panic(err)
		}
		if percent[0] > 80 {
			fmt.Println("cpu usage too high")
			// write pprof file
			if _, err := os.Stat("./pprof"); os.IsNotExist(err) {
				err := os.Mkdir("./pprof", os.ModePerm)
				if err != nil {
					SysLog("创建pprof文件夹失败 " + err.Error())
					continue
				}
			}
			f, err := os.Create("./pprof/" + fmt.Sprintf("cpu-%s.pprof", time.Now().Format("20060102150405")))
			if err != nil {
				SysLog("创建pprof文件失败 " + err.Error())
				continue
			}
			err = pprof.StartCPUProfile(f)
			if err != nil {
				SysLog("启动pprof失败 " + err.Error())
				continue
			}
			time.Sleep(10 * time.Second) // profile for 30 seconds
			pprof.StopCPUProfile()
			f.Close()
		}
		time.Sleep(30 * time.Second)
	}
}
