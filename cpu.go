package main

import (
	"runtime"
	"time"
)

func testcputime() uint64

func getCPUBaseFreq() uint32

func init() {
	Frequency := getCPUBaseFreq()

	if Frequency == 0 {
		vStart := testcputime()
		time.Sleep(10 * time.Millisecond)
		vEnd := testcputime()
		GlobalConf.CoreCpuMetrics.CPUFrequency.Store((vEnd - vStart) / 10)
	} else {
		GlobalConf.CoreCpuMetrics.CPUFrequency.Store(uint64(Frequency) * 1000)
	}

}

func CalcSchedBloat() {
	var MaxJitterCycle uint64 = 0

	LastCycle := testcputime()

	for range 10 {
		runtime.Gosched()
		CurrentCycle := testcputime()

		CycleRift := CurrentCycle - LastCycle
		LastCycle = CurrentCycle

		if CycleRift > MaxJitterCycle {
			MaxJitterCycle = CycleRift
		}
	}

	GlobalConf.CoreCpuMetrics.CurrFrequency.Store(MaxJitterCycle)
}
