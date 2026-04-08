package main

import (
	"unsafe"
)

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
