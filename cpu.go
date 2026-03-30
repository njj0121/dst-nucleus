package main

import (
	"runtime"
	"time"
)

func testcputime() uint64

func getCPUBaseFreq() uint32

func init() {
	频率 := getCPUBaseFreq()

	if 频率 == 0 {
		开始 := testcputime()
		time.Sleep(10 * time.Millisecond)
		结束 := testcputime()
		全局配置.核心CPU指标.CPU频率.Store((结束 - 开始) / 10)
	} else {
		全局配置.核心CPU指标.CPU频率.Store(uint64(频率) * 1000)
	}

}

func 计算物理调度膨胀() {
	var 局部最大震荡周期 uint64 = 0

	上次周期 := testcputime()

	for range 10 {
		runtime.Gosched()
		当前周期 := testcputime()

		震荡裂缝 := 当前周期 - 上次周期
		上次周期 = 当前周期

		if 震荡裂缝 > 局部最大震荡周期 {
			局部最大震荡周期 = 震荡裂缝
		}
	}

	全局配置.核心CPU指标.当前频率.Store(局部最大震荡周期)
}
