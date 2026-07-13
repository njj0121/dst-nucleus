package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func init() {
	ZeroState := []byte(`{"status":"loading"}`)
	CurrStateSnap.Store(&ZeroState)
}

type reusableNopCloser struct {
	*bytes.Reader
}

func (reusableNopCloser) Close() error { return nil }

// 必须0分配，专门为了应对调用程序写得太烂，调用过于频繁导致卡死，我测过生产环境70万QPS还能保证0错误和稳定的无波动50m内存，再频繁也快不过这个函数
func Benchmark_api(b *testing.B) {
	FlushServerState()
	TestMatrix := []struct {
		代号            string
		MockMethod    string
		MockRoute     string
		MockBody      []byte
		PhysicsEngine http.HandlerFunc
	}{
		{"status", "GET", "/api/status", nil, api_status},
	}

	for _, TargetHost := range TestMatrix {
		b.Run(TargetHost.代号, func(b *testing.B) {
			w := httptest.NewRecorder()
			w.Body = bytes.NewBuffer(make([]byte, 0, 1024))

			var RealReqBody io.Reader = nil
			var bodyReader *bytes.Reader
			var fakeBody reusableNopCloser

			if TargetHost.MockBody != nil {
				bodyReader = bytes.NewReader(TargetHost.MockBody)
				RealReqBody = bodyReader
				fakeBody = reusableNopCloser{Reader: bodyReader}
			}

			req := httptest.NewRequest(TargetHost.MockMethod, TargetHost.MockRoute, RealReqBody)
			if bodyReader != nil {
				req.Body = fakeBody
			}

			TargetHost.PhysicsEngine(w, req)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				w.Body.Reset()

				if bodyReader != nil {
					bodyReader.Seek(0, 0)
				}

				TargetHost.PhysicsEngine(w, req)
			}
		})
	}
}

// plan 9 不能内联还挺浪费性能的，看看要浪费多少
func BenchmarkLUT(b *testing.B) {
	for i := 0; i < b.N; i++ {
		CalcSchedBloat()
	}
}

// 必须测到0内存分配，500ms一次的高频函数
func Benchmark_拼status_json(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FlushServerState()
	}
}
