package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func init() {
	初始空状态 := []byte(`{"status":"loading"}`)
	当前状态快照.Store(&初始空状态)
}

type reusableNopCloser struct {
	*bytes.Reader
}

func (reusableNopCloser) Close() error { return nil }

// 必须0分配，专门为了应对调用程序写得太烂，调用过于频繁导致卡死，我测过生产环境70万QPS还能保证0错误和稳定的无波动50m内存，再频繁也快不过这个函数
func Benchmark_api(b *testing.B) {
	刷新服务器状态()
	测试矩阵 := []struct {
		代号   string
		模拟方法 string
		模拟路由 string
		模拟主体 []byte
		物理引擎 http.HandlerFunc
	}{
		{"status", "GET", "/api/status", nil, api_status},
	}

	for _, 靶机 := range 测试矩阵 {
		b.Run(靶机.代号, func(b *testing.B) {
			w := httptest.NewRecorder()
			w.Body = bytes.NewBuffer(make([]byte, 0, 1024))

			var 真实请求体 io.Reader = nil
			var bodyReader *bytes.Reader
			var fakeBody reusableNopCloser

			if 靶机.模拟主体 != nil {
				bodyReader = bytes.NewReader(靶机.模拟主体)
				真实请求体 = bodyReader
				fakeBody = reusableNopCloser{Reader: bodyReader}
			}

			req := httptest.NewRequest(靶机.模拟方法, 靶机.模拟路由, 真实请求体)
			if bodyReader != nil {
				req.Body = fakeBody
			}

			靶机.物理引擎(w, req)

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				w.Body.Reset()

				if bodyReader != nil {
					bodyReader.Seek(0, 0)
				}

				靶机.物理引擎(w, req)
			}
		})
	}
}

// plan 9 不能内联还挺浪费性能的，看看要浪费多少
func BenchmarkLUT(b *testing.B) {
	for i := 0; i < b.N; i++ {
		计算物理调度膨胀()
	}
}

// 必须测到0内存分配，500ms一次的高频函数
func Benchmark_拼status_json(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		刷新服务器状态()
	}
}
