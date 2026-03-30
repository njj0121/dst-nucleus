package main

import (
	"sync"
	"unsafe"
)

var 全局行缓存 = make([]byte, 0, 1024)

// 单纯为了阻塞而阻塞，绝对不能写成异步输出，指令执行周期远小于IO调度周期，很容易乱序，对于排错有极大的障碍
var 输出阻塞锁 sync.Mutex

func S2B(s string) []byte {
	if s == "" {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func B2S(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func E2B(err error) []byte {
	if err == nil {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(err.Error()), len(err.Error()))
}

func U642B(val *uint64) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(val)), 8)
}
