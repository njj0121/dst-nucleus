package main

import (
	"bytes"
	"context"
	_ "embed"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type 状态锁 struct {
	_    [64]byte
	锁定状态 atomic.Uint32
	_    [64]byte
}

var API唤醒通道 = make(chan struct{})

var 文件读写原子锁 状态锁

var 接收缓冲池 = sync.Pool{
	New: func() any {
		b := make([]byte, 1024)
		return &b
	},
}

var json头 = []string{"application/json; charset=utf-8"}
var plain头 = []string{"text/plain; charset=utf-8"}
var html头 = []string{"text/html; charset=utf-8"}

type 极速网关 struct{}

func (极速网关) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/events", "/api/epoch/master", "/api/epoch/caves", "/api/log/master", "/api/log/caves":
	default:
		控制器 := http.NewResponseController(w)
		超时时间 := time.Now().Add(10 * time.Second)

		控制器.SetReadDeadline(超时时间)
		控制器.SetWriteDeadline(超时时间)
	}

	switch r.URL.Path {
	case "/":
		api_ui(w, r)
	case "/api/status":
		api_status(w, r)
	case "/api/events":
		api_events(w, r)
	case "/api/epoch/master":
		api_epoch_master(w, r)
	case "/api/epoch/caves":
		api_epoch_caves(w, r)
	case "/api/command":
		api_command(w, r)
	case "/api/start":
		api_start(w, r)
	case "/api/stop":
		api_stop(w, r)
	case "/api/restart":
		api_restart(w, r)
	case "/api/file/read":
		api_file_read(w, r)
	case "/api/file/write":
		api_file_write(w, r)
	case "/api/update/state":
		api_update_state(w, r)
	case "/api/log/master":
		api_log_master(w, r)
	case "/api/log/caves":
		api_log_caves(w, r)
	case "/api/checkupdate":
		api_checkupdate(w, r)
	default:
		w.WriteHeader(404)
		w.Write(api_ui404)
	}
}

func 启动本地api接口() {
	api辅助协程生命周期, 探针取消 := context.WithCancel(context.Background())
	defer 探针取消()
	go 运行系统资源采集探针(api辅助协程生命周期)
	if 全局配置.配置区2.启用主世界.Load() != 全局配置.配置区2.启用洞穴.Load() {
		go 全局世代心跳(api辅助协程生命周期)
	}

	go 主世界日志广播中心(api辅助协程生命周期)
	go 洞穴日志广播中心(api辅助协程生命周期)

	接口地址 := 全局配置.配置区1.http接口

	if strings.HasPrefix(接口地址, "/") || strings.HasPrefix(接口地址, "./") || strings.HasSuffix(接口地址, ".sock") {
		控制台合并输出换行(S2B("[api] gateway listening on unix: "), S2B(接口地址))
	} else {
		展示地址 := 接口地址
		if strings.HasPrefix(展示地址, ":") {
			展示地址 = "127.0.0.1" + 展示地址
		} else if strings.HasPrefix(展示地址, "0.0.0.0:") {
			展示地址 = strings.Replace(展示地址, "0.0.0.0:", "127.0.0.1:", 1)
		}

		控制台合并输出换行(S2B("[api] gateway listening on: http://"), S2B(展示地址))
	}

	var 扁平网关 极速网关

	if err := 底层监听(全局配置.配置区1.http接口, 扁平网关); err != nil {
		控制台合并输出换行(S2B("[fatal] gateway crashed: "), S2B(全局配置.配置区1.http接口))
	}
}

//go:embed ui.html
var 面板HTML []byte

var api_ui404 = []byte("404 page not found\n")

func api_ui(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		w.WriteHeader(404)
		w.Write(api_ui404)
		return
	}

	w.Header()["Content-Type"] = html头
	w.Write(面板HTML)
}

var sse1 = []byte("data: ")
var sse2 = []byte("\n\n")
var post405 = []byte(`{"status":"error", "message":"method not allowed (POST required)"}`)
var get400 = []byte(`{"status":"error", "message":"method not allowed (GET required)"}`)
var success200 = []byte(`{"status": "success"}`)
var error4xx = []byte(`{"status":"error"}`)

var api_status0 = []byte(`{"status":"loading", "message":"probe warming up"}`)

// 80697765                13.62 ns/op            0 B/op          0 allocs/op
func api_status(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http报错(w, 400, get400)
		return
	}

	报文指针 := 当前状态快照.Load()
	if 报文指针 == nil {
		w.Write(api_status0)
		return
	}

	w.Header()["Content-Type"] = json头
	w.Write(*报文指针)
}

var sse头 = []string{"text/event-stream; charset=utf-8"}
var noCache头 = []string{"no-cache"}
var keepAlive头 = []string{"keep-alive"}
var 跨域头 = []string{"*"}

var sse观察者矩阵 sync.Map

var api_events500 = []byte(`{"status":"error", "message":"flusher not supported"}`)

func api_events(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = sse头
	w.Header()["Cache-Control"] = noCache头
	w.Header()["Connection"] = keepAlive头
	w.Header()["Access-Control-Allow-Origin"] = 跨域头

	flusher, 强转成功 := w.(http.Flusher)
	if !强转成功 {
		http报错(w, 500, api_events500)
		return
	}

	客户端推送通道 := make(chan struct{}, 1)
	sse观察者矩阵.Store(客户端推送通道, struct{}{})

	defer sse观察者矩阵.Delete(客户端推送通道)

	客户端拔管 := r.Context().Done()

	for {
		select {
		case <-客户端拔管:
			return
		case <-客户端推送通道:
			全局报文指针 := 当前状态快照.Load()
			if 全局报文指针 == nil {
				continue
			}
			w.Write(sse1)
			w.Write(*全局报文指针)
			w.Write(sse2)
			flusher.Flush()
		}
	}
}

func api_epoch_master(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = sse头
	w.Header()["Cache-Control"] = noCache头
	w.Header()["Connection"] = keepAlive头
	w.Header()["Access-Control-Allow-Origin"] = 跨域头

	冲刷器, 强转成功 := w.(http.Flusher)
	if !强转成功 {
		return
	}

	事件通道 := make(chan int64, 1)
	主世界世代观察者.Store(事件通道, struct{}{})
	defer 主世界世代观察者.Delete(事件通道)

	客户端拔管 := r.Context().Done()

	for {
		select {
		case <-客户端拔管:
			return
		case 世代 := <-事件通道:
			w.Write(sse1)
			w.Write(strconv.AppendInt(nil, 世代, 10))
			w.Write(sse2)
			冲刷器.Flush()
		}
	}
}

func api_epoch_caves(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = sse头
	w.Header()["Cache-Control"] = noCache头
	w.Header()["Connection"] = keepAlive头
	w.Header()["Access-Control-Allow-Origin"] = 跨域头

	冲刷器, 强转成功 := w.(http.Flusher)
	if !强转成功 {
		return
	}

	事件通道 := make(chan int64, 1)
	洞穴世代观察者.Store(事件通道, struct{}{})
	defer 洞穴世代观察者.Delete(事件通道)

	客户端拔管 := r.Context().Done()

	for {
		select {
		case <-客户端拔管:
			return
		case 世代 := <-事件通道:
			w.Write(sse1)
			w.Write(strconv.AppendInt(nil, 世代, 10))
			w.Write(sse2)
			冲刷器.Flush()
		}
	}
}

var 命令api原子锁 状态锁

var api_command413 = []byte(`{"status":"error", "message":"payload too large"}`)
var api_command400_1 = []byte(`{"status":"error", "message":"bad request or payload too large"}`)
var api_command400_2 = []byte(`{"status":"error", "message":"empty payload"}`)
var api_command503_1 = []byte(`{"status":"warning", "message":"master ack, caves dropped (congestion)"}`)
var api_command503_2 = []byte(`{"status":"caves ack, master dropped (congestion)"}`)
var api_command400_3 = []byte(`{"status":"error", "message":"invalid target"}`)
var api_command503_3 = []byte(`{"status":"error", "message":"pipeline congested, payload dropped"}`)

func api_command(w http.ResponseWriter, r *http.Request) {
	if !命令api原子锁.锁定状态.CompareAndSwap(0, 1) {
		http报错(w, 423, error4xx)
		return
	}
	defer 命令api原子锁.锁定状态.Store(0)

	if r.Method != "POST" {
		http报错(w, 405, post405)
		return
	}

	命令目标 := r.URL.Query().Get("target")
	if 命令目标 != "" {
		命令目标 = strings.ToLower(命令目标)
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024)

	池化指针 := 接收缓冲池.Get().(*[]byte)
	临时缓冲 := *池化指针

	defer func() {
		clear(临时缓冲)
		接收缓冲池.Put(池化指针)
	}()

	读取总数 := 0
	for {
		if 读取总数 == len(临时缓冲) {
			http报错(w, 413, api_command413)
			return
		}

		n, err := r.Body.Read(临时缓冲[读取总数:])
		读取总数 += n

		if err != nil {
			if err == io.EOF {
				break
			}
			http报错(w, 400, api_command400_1)
			return
		}
	}

	if 读取总数 == 0 {
		接收缓冲池.Put(池化指针)
		http报错(w, 400, api_command400_2)
		return
	}

	缺换行符 := 临时缓冲[读取总数-1] != '\n'
	最终长度 := 读取总数
	if 缺换行符 {
		最终长度++
	}

	最终指令 := make([]byte, 最终长度)
	copy(最终指令, 临时缓冲[:读取总数])
	if 缺换行符 {
		最终指令[最终长度-1] = '\n'
	}

	clear(临时缓冲[:读取总数])
	接收缓冲池.Put(池化指针)

	投递成功 := false
	switch 命令目标 {
	case "caves":
		select {
		case 洞穴命令通道 <- 最终指令:
			投递成功 = true
		default:
		}
	case "all":
		主世界OK := false
		洞穴OK := false

		select {
		case 主世界命令通道 <- 最终指令:
			主世界OK = true
		default:
		}

		select {
		case 洞穴命令通道 <- 最终指令:
			洞穴OK = true
		default:
		}

		if 主世界OK && 洞穴OK {
			投递成功 = true
		} else if 主世界OK && !洞穴OK {
			http报错(w, 503, api_command503_1)
			return
		} else if !主世界OK && 洞穴OK {
			http报错(w, 503, api_command503_2)
			return
		} else {
			投递成功 = false
		}
	case "master", "":
		select {
		case 主世界命令通道 <- 最终指令:
			投递成功 = true
		default:
		}
	default:
		http报错(w, 400, api_command400_3)
		return
	}

	if !投递成功 {
		http报错(w, 503, api_command503_3)
		return
	}

	w.Header()["Content-Type"] = json头
	w.Write(success200)
}

var api_start409 = []byte(`{"status":"error", "message":"start blocked"}`)

func api_start(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http报错(w, 405, post405)
		return
	}
	select {
	case API唤醒通道 <- struct{}{}:
		全局配置.原子锁.允许服务器运行原子锁.Store(true)
	default:
		http报错(w, 409, api_start409)
		return
	}
	w.Header()["Content-Type"] = json头
	w.Write(success200)
}

func api_stop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http报错(w, 405, post405)
		return
	}
	全局配置.原子锁.允许服务器运行原子锁.Store(false)

	select {
	case 动作总线 <- 动作_执行API强制关闭:
	default:
	}

	旧指针 := 全局局部生命周期终结.Load()
	if 旧指针 != nil && 全局局部生命周期终结.CompareAndSwap(旧指针, nil) {
		(*旧指针)()
	}

	w.Header()["Content-Type"] = json头
	w.Write(success200)
}

func api_restart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http报错(w, 405, post405)
		return
	}
	全局配置.原子锁.允许服务器运行原子锁.Store(true)

	select {
	case 动作总线 <- 动作_执行API强制关闭:
	default:
	}

	旧指针 := 全局局部生命周期终结.Load()
	if 旧指针 != nil && 全局局部生命周期终结.CompareAndSwap(旧指针, nil) {
		(*旧指针)()
	}

	select {
	case API唤醒通道 <- struct{}{}:
	default:
	}

	w.Header()["Content-Type"] = json头
	w.Write(success200)
}

var api_file_readHeaderByte [32]byte
var api_file_readHeader = make([]string, 1)
var targetHeader = []byte("target=")
var api_file_read400 = []byte(`{"status":"error", "message":"invalid target parameter"}`)
var api_file_read413 = []byte(`{"status":"error", "message":"file > 1MB, stream rejected"}`)
var api_file_readNone []byte

func api_file_read(w http.ResponseWriter, r *http.Request) {
	if !文件读写原子锁.锁定状态.CompareAndSwap(0, 1) {
		http报错(w, 423, error4xx)
		return
	}
	defer 文件读写原子锁.锁定状态.Store(0)

	if r.Method != "GET" {
		http报错(w, 400, get400)
		return
	}

	queryBytes := S2B(r.URL.RawQuery)

	var 目标 []byte
	剩余 := queryBytes

	for len(剩余) > 0 {
		var 当前块 []byte
		块尾 := bytes.IndexByte(剩余, '&')
		if 块尾 == -1 {
			当前块 = 剩余
			剩余 = nil
		} else {
			当前块 = 剩余[:块尾]
			剩余 = 剩余[块尾+1:]
		}

		if bytes.HasPrefix(当前块, targetHeader) {
			目标 = 当前块[len(targetHeader):]
			break
		}
	}

	if len(目标) == 0 || len(目标) > 32 {
		http报错(w, 400, api_file_read400)
		return
	}

	var 栈缓冲 [32]byte
	长度 := copy(栈缓冲[:], 目标)

	for i := 0; i < 长度; i++ {
		if 栈缓冲[i] >= 'A' && 栈缓冲[i] <= 'Z' {
			栈缓冲[i] += 'a' - 'A'
		}
	}

	var 文件路径 string

	switch string(栈缓冲[:长度]) {
	case "cluster":
		文件路径 = cluster路径
	case "whitelist":
		文件路径 = 白名单路径
	case "blacklist":
		文件路径 = 黑名单路径
	case "adminlist":
		文件路径 = 管理员名单路径
	case "master_server":
		文件路径 = 主世界server配置路径
	case "caves_server":
		文件路径 = 洞穴server配置路径
	case "master_world":
		文件路径 = 主世界world配置路径
	case "caves_world":
		文件路径 = 洞穴world配置路径
	case "master_mod":
		文件路径 = 主世界mod配置路径
	case "caves_mod":
		文件路径 = 洞穴mod配置路径
	case "setup":
		文件路径 = 全局配置.配置区1.模组Lua更新文件目标路径
	default:
		http报错(w, 400, api_file_read400)
		return
	}

	f, err := os.Open(文件路径)
	if err != nil {
		w.Header()["Content-Type"] = plain头
		w.Write(api_file_readNone)
		return
	}
	defer f.Close()
	w.Header()["Content-Type"] = plain头
	if stat, err := f.Stat(); err == nil {
		if stat.Size() > 1024*1024 {
			http报错(w, 413, api_file_read413)
			return
		}

		api_file_readHeader[0] = B2S(strconv.AppendInt(api_file_readHeaderByte[:0], stat.Size(), 10))
		w.Header()["Content-Length"] = api_file_readHeader
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}

var api_file_write400_1 = []byte(`{"status":"error", "message":"empty payload rejected"}`)
var api_file_write400_2 = []byte(`{"status":"error", "message":"invalid target"}`)
var api_file_write400_3 = []byte(`{"status":"error", "message":"unconfigured target"}`)
var api_file_write500_1 = []byte(`{"status":"error", "message":"primary write failed"}`)
var api_file_write500_2 = []byte(`{"status":"error", "message":"clone to secondary failed"}`)
var api_file_write200 = []byte(`{"status":"success", "message":"zero-copy stream write complete"}`)

func api_file_write(w http.ResponseWriter, r *http.Request) {
	if !文件读写原子锁.锁定状态.CompareAndSwap(0, 1) {
		http报错(w, 423, error4xx)
		return
	}
	defer 文件读写原子锁.锁定状态.Store(0)

	if r.Method != "POST" {
		http报错(w, 405, post405)
		return
	}

	if r.ContentLength == 0 {
		http报错(w, 400, api_file_write400_1)
		return
	}

	queryBytes := S2B(r.URL.RawQuery)

	var 目标 []byte
	剩余 := queryBytes

	for len(剩余) > 0 {
		var 当前块 []byte
		块尾 := bytes.IndexByte(剩余, '&')
		if 块尾 == -1 {
			当前块 = 剩余
			剩余 = nil
		} else {
			当前块 = 剩余[:块尾]
			剩余 = 剩余[块尾+1:]
		}

		if bytes.HasPrefix(当前块, targetHeader) {
			目标 = 当前块[len(targetHeader):]
			break
		}
	}

	if len(目标) == 0 || len(目标) > 32 {
		http报错(w, 400, api_file_read400)
		return
	}

	var 栈缓冲 [32]byte
	长度 := copy(栈缓冲[:], 目标)

	for i := 0; i < 长度; i++ {
		if 栈缓冲[i] >= 'A' && 栈缓冲[i] <= 'Z' {
			栈缓冲[i] += 'a' - 'A'
		}
	}
	var 目标写入路径 [2]string

	switch string(栈缓冲[:长度]) {
	case "cluster":
		目标写入路径[0] = cluster路径
	case "whitelist":
		目标写入路径[0] = 白名单路径
	case "blacklist":
		目标写入路径[0] = 黑名单路径
	case "adminlist":
		目标写入路径[0] = 管理员名单路径
	case "master_server":
		目标写入路径[0] = 主世界server配置路径
	case "caves_server":
		目标写入路径[0] = 洞穴server配置路径
	case "master_world":
		目标写入路径[0] = 主世界world配置路径
	case "caves_world":
		目标写入路径[0] = 洞穴world配置路径
	case "mod":
		目标写入路径 = 写入mod配置文件路径集
	case "setup":
		目标写入路径[0] = 全局配置.配置区1.模组Lua更新文件目标路径

	default:
		http报错(w, 400, api_file_write400_2)
		return
	}

	if 目标写入路径[0] == "" {
		http报错(w, 400, api_file_write400_3)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)

	if err := 原子写文件流(目标写入路径[0], r.Body); err != 0 {
		控制台合并输出换行(S2B("[sys] write failed "), S2B(目标写入路径[0]))
		http报错(w, 500, api_file_write500_1)
		return
	}

	if 目标写入路径[1] != "" {
		if _, err := 复制文件(目标写入路径[0], 目标写入路径[1]); err != 0 {
			控制台合并输出换行(S2B("[sys] clone failed "), S2B(目标写入路径[1]))
			http报错(w, 500, api_file_write500_2)
			return
		}
	}

	w.Header()["Content-Type"] = json头
	w.Write(api_file_write200)
}

var api_update_state413 = []byte(`{"status":"error", "message":"Payload Too Large"}`)

func api_update_state(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http报错(w, 405, post405)
		return
	}

	池化指针 := 接收缓冲池.Get().(*[]byte)
	临时缓冲 := *池化指针

	defer func() {
		clear(临时缓冲)
		接收缓冲池.Put(池化指针)
	}()

	读取总数 := 0
	for {
		if 读取总数 == len(临时缓冲) {
			http报错(w, 413, api_update_state413)
			return
		}

		n, err := r.Body.Read(临时缓冲[读取总数:])
		读取总数 += n
		if err != nil {
			break
		}
	}
	if 读取总数 == 0 {
		http报错(w, 400, error4xx)
		return
	}

	纯数据 := 临时缓冲[:读取总数]
	游标 := 0
	总长 := 读取总数

	for 游标 < 总长 {
		key起始 := 游标
		for 游标 < 总长 && 纯数据[游标] != '=' {
			游标++
		}
		if 游标 >= 总长 {
			break
		}
		key := 纯数据[key起始:游标]
		游标++

		val起始 := 游标
		for 游标 < 总长 && 纯数据[游标] != '&' && 纯数据[游标] != ';' && 纯数据[游标] != '\n' {
			游标++
		}
		val := 纯数据[val起始:游标]
		游标++

		if len(key) == 0 || len(val) == 0 {
			continue
		}

		switch string(key) {
		case "players":
			全局配置.游戏内状态.在线玩家人数.Store(解析API无符号整型(val))
		case "cycles":
			全局配置.游戏内状态.世界天数.Store(解析API无符号整型(val))
		case "season":
			全局配置.游戏内状态.当前季节.Store(解析API无符号整型(val))
		case "phase":
			全局配置.游戏内状态.昼夜阶段.Store(解析API无符号整型(val))
		case "rem_days":
			全局配置.游戏内状态.季节剩余天数.Store(解析API无符号整型(val))
		case "temp":
			全局配置.游戏内状态.绝对温度.Store(解析API有符号整型(val))

		// bool
		case "is_raining":
			全局配置.游戏内状态.是否下雨.Store(len(val) > 0 && (val[0] == '1' || val[0] == 't' || val[0] == 'T'))
		case "is_snowing":
			全局配置.游戏内状态.是否下雪.Store(len(val) > 0 && (val[0] == '1' || val[0] == 't' || val[0] == 'T'))
		case "alter_awake":
			全局配置.游戏内状态.天体唤醒.Store(len(val) > 0 && (val[0] == '1' || val[0] == 't' || val[0] == 'T'))

		case "moon_phase":
			全局配置.游戏内状态.月相状态.Store(解析API无符号整型(val))
		case "nightmare":
			全局配置.游戏内状态.暴动状态.Store(解析API无符号整型(val))

		// boss
		case "deerclops":
			全局配置.游戏内状态.巨鹿倒计时.Store(解析API无符号整型(val))
		case "bearger":
			全局配置.游戏内状态.熊大倒计时.Store(解析API无符号整型(val))
		case "moose":
			全局配置.游戏内状态.大鹅倒计时.Store(解析API无符号整型(val))
		case "dragonfly":
			全局配置.游戏内状态.龙蝇倒计时.Store(解析API无符号整型(val))
		case "beequeen":
			全局配置.游戏内状态.蜂后倒计时.Store(解析API无符号整型(val))
		case "klaus":
			全局配置.游戏内状态.克劳斯倒计时.Store(解析API无符号整型(val))
		case "toadstool":
			全局配置.游戏内状态.蛤蟆倒计时.Store(解析API无符号整型(val))
		case "fuelweaver":
			全局配置.游戏内状态.织影者倒计时.Store(解析API无符号整型(val))
		case "malbatross":
			全局配置.游戏内状态.邪天翁倒计时.Store(解析API无符号整型(val))
		case "lordfruitfly":
			全局配置.游戏内状态.果蝇王倒计时.Store(解析API无符号整型(val))
		case "antlion":
			全局配置.游戏内状态.蚁狮踩踏分钟倒计时.Store(解析API无符号整型(val))
		}
	}

	w.Header()["Content-Type"] = json头
	w.Write(success200)
}

type 广播日志块 struct {
	_   [64]byte
	引用数 atomic.Int32
	_   [64]byte
	数据  []byte
}

var 日志广播池 = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &广播日志块{数据: b}
	},
}

var 主世界日志连接数 atomic.Int32
var 主世界中央日志管道 = make(chan *广播日志块, 1024)
var 主世界日志订阅 = make(chan chan *广播日志块, 128)
var 主世界日志退订 = make(chan chan *广播日志块, 128)

var 洞穴日志连接数 atomic.Int32
var 洞穴中央日志管道 = make(chan *广播日志块, 1024)
var 洞穴日志订阅 = make(chan chan *广播日志块, 128)
var 洞穴日志退订 = make(chan chan *广播日志块, 128)

func api_log_master(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = sse头
	w.Header()["Cache-Control"] = noCache头
	w.Header()["Connection"] = keepAlive头
	w.Header()["Access-Control-Allow-Origin"] = 跨域头

	冲刷器, 强转成功 := w.(http.Flusher)
	if !强转成功 {
		return
	}

	事件通道 := make(chan *广播日志块, 256)
	主世界日志连接数.Add(1)

	主世界日志订阅 <- 事件通道

	w.WriteHeader(200)
	冲刷器.Flush()

	defer func() {
		主世界日志连接数.Add(-1)

		主世界日志退订 <- 事件通道

		for 块 := range 事件通道 {
			if 块.引用数.Add(-1) == 0 {
				日志广播池.Put(块)
			}
		}
	}()

	客户端拔管 := r.Context().Done()

	rc := http.NewResponseController(w)

	for {
		select {
		case <-客户端拔管:
			return
		case 块 := <-事件通道:
			rc.SetWriteDeadline(time.Now().Add(2 * time.Second))

			_, err := w.Write(块.数据)

			if 块.引用数.Add(-1) == 0 {
				日志广播池.Put(块)
			}

			if err != nil {
				return
			}
			冲刷器.Flush()
		}
	}
}

func api_log_caves(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = sse头
	w.Header()["Cache-Control"] = noCache头
	w.Header()["Connection"] = keepAlive头
	w.Header()["Access-Control-Allow-Origin"] = 跨域头

	冲刷器, 强转成功 := w.(http.Flusher)
	if !强转成功 {
		return
	}

	事件通道 := make(chan *广播日志块, 256)
	洞穴日志连接数.Add(1)

	洞穴日志订阅 <- 事件通道

	w.WriteHeader(200)
	冲刷器.Flush()

	defer func() {
		洞穴日志连接数.Add(-1)

		洞穴日志退订 <- 事件通道

		for 块 := range 事件通道 {
			if 块.引用数.Add(-1) == 0 {
				日志广播池.Put(块)
			}
		}
	}()

	客户端拔管 := r.Context().Done()

	rc := http.NewResponseController(w)

	for {
		select {
		case <-客户端拔管:
			return
		case 块 := <-事件通道:
			rc.SetWriteDeadline(time.Now().Add(2 * time.Second))

			_, err := w.Write(块.数据)

			if 块.引用数.Add(-1) == 0 {
				日志广播池.Put(块)
			}

			if err != nil {
				return
			}
			冲刷器.Flush()
		}
	}
}

var api_checkupdate409 = []byte(`{"status":"error", "message":"invalid state"}`)

func api_checkupdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http报错(w, 405, post405)
		return
	}
	select {
	case 主动触发更新检测 <- struct{}{}:
	default:
		http报错(w, 409, api_checkupdate409)
		return
	}
	w.Header()["Content-Type"] = json头
	w.Write(success200)
}

func 解析API无符号整型(载荷 []byte) uint32 {
	if len(载荷) == 0 {
		return 4294967295
	}

	解析结果, err := strconv.ParseUint(B2S(载荷), 10, 32)
	if err != nil {
		return 4294967295
	}

	return uint32(解析结果)
}

func 解析API有符号整型(载荷 []byte) int32 {
	if len(载荷) == 0 {
		return 2147483647
	}

	解析结果, err := strconv.ParseInt(B2S(载荷), 10, 32)
	if err != nil {
		return 2147483647
	}

	return int32(解析结果)
}

func http报错(w http.ResponseWriter, 状态码 int, 响应体 []byte) {
	w.Header()["Content-Type"] = json头
	w.WriteHeader(状态码)
	w.Write(响应体)
}

var 原子写文件流缓冲池 = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

func 原子写文件流(目标路径 string, 源流 io.Reader) uint8 {
	目标目录 := filepath.Dir(目标路径)
	os.MkdirAll(目标目录, 0755)

	临时文件, err := os.CreateTemp(目标目录, "tmp_stream_*")
	if err != nil {
		控制台合并输出换行(E2B(err))
		return 128
	}
	临时路径 := 临时文件.Name()

	defer os.Remove(临时路径)

	缓冲指针 := 原子写文件流缓冲池.Get().(*[]byte)

	_, err = io.CopyBuffer(临时文件, 源流, *缓冲指针)

	原子写文件流缓冲池.Put(缓冲指针)

	if err != nil {
		临时文件.Close()
		控制台合并输出换行(S2B("[sys] stream copy interrupted: "), E2B(err))
		return 129
	}

	err = 临时文件.Sync()
	if err != nil {
		临时文件.Close()
		控制台合并输出换行(E2B(err))
		return 130
	}

	err = 临时文件.Close()
	if err != nil {
		控制台合并输出换行(E2B(err))
		return 131
	}

	var renameErr error
	for i := 0; i < 5; i++ {
		renameErr = os.Rename(临时路径, 目标路径)
		if renameErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if renameErr != nil {
		控制台合并输出换行(S2B("[sys] atomic rename rejected (5 retries): "), E2B(renameErr))
		return 132
	}

	return 0
}

func 运行系统资源采集探针(生命周期 context.Context) {
	当前间隔 := 全局配置.全服监控态.采样间隔.Load()
	if 当前间隔 < 100 {
		当前间隔 = 100
	}
	定时器 := time.NewTicker(time.Duration(当前间隔) * time.Millisecond)
	defer 定时器.Stop()

	启用CPU膨胀探测 := 全局配置.核心CPU指标.启用CPU膨胀探测.Load()

	for {
		select {
		case <-生命周期.Done():
			return
		case <-定时器.C:
			刷新服务器状态()
			if 启用CPU膨胀探测 {
				计算物理调度膨胀()
			}
			//全服资源探针任务()
		}
	}
}

var 主世界世代观察者 sync.Map
var 洞穴世代观察者 sync.Map

func 全局世代心跳(生命周期 context.Context) {
	秒表 := time.NewTicker(2 * time.Second)
	defer 秒表.Stop()

	for {
		select {
		case <-生命周期.Done():
			return
		case <-秒表.C:
			当前主世界世代 := 全局配置.进程状态.主世界当前世代.Load()
			主世界世代观察者.Range(func(key, value any) bool {
				通道 := key.(chan int64)
				select {
				case 通道 <- 当前主世界世代:
				default:
				}
				return true
			})

			洞穴当前世代 := 全局配置.进程状态.洞穴当前世代.Load()
			洞穴世代观察者.Range(func(key, value any) bool {
				通道 := key.(chan int64)
				select {
				case 通道 <- 洞穴当前世代:
				default:
				}
				return true
			})
		}
	}
}

func 主世界日志广播中心(生命周期 context.Context) {
	订阅者集合 := make(map[chan *广播日志块]struct{})

	for {
		select {
		case <-生命周期.Done():
			return

		case ch := <-主世界日志订阅:
			订阅者集合[ch] = struct{}{}

		case ch := <-主世界日志退订:
			delete(订阅者集合, ch)
			close(ch)

		case 块 := <-主世界中央日志管道:
			for ch := range 订阅者集合 {
				块.引用数.Add(1)
				select {
				case ch <- 块:
				default:
					块.引用数.Add(-1)
				}
			}
			if 块.引用数.Add(-1) == 0 {
				日志广播池.Put(块)
			}
		}
	}
}

func 洞穴日志广播中心(生命周期 context.Context) {
	订阅者集合 := make(map[chan *广播日志块]struct{})

	for {
		select {
		case <-生命周期.Done():
			return

		case ch := <-洞穴日志订阅:
			订阅者集合[ch] = struct{}{}

		case ch := <-洞穴日志退订:
			delete(订阅者集合, ch)
			close(ch)

		case 块 := <-洞穴中央日志管道:
			for ch := range 订阅者集合 {
				块.引用数.Add(1)
				select {
				case ch <- 块:
				default:
					块.引用数.Add(-1)
				}
			}
			if 块.引用数.Add(-1) == 0 {
				日志广播池.Put(块)
			}
		}
	}
}

/*
Running 10s test @ http://127.0.0.1:20888/api/status
  8 threads and 1000 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     1.82ms    2.16ms  28.69ms   86.27%
    Req/Sec    94.26k    14.96k  133.60k    64.88%
  7513740 requests in 10.04s, 2.87GB read
Requests/sec: 748147.46
Transfer/sec:    292.53MB
*/
