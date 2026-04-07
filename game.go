package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

//go:generate go run build_lua.go
//go:embed parasite_min.lua
var 寄生指令 []byte

type 目标服 uint8

const (
	发往主世界 目标服 = 1 << 0
	发往洞穴  目标服 = 1 << 1
	发往全服  目标服 = 3
)

var 主世界命令通道 = make(chan []byte, 100)
var 洞穴命令通道 = make(chan []byte, 100)

var 主世界寄生虫心跳 = make(chan struct{}, 1)
var 洞穴寄生虫心跳 = make(chan struct{}, 1)

func 发送纯文本指令(指令字符串 string, 命令目标 目标服) {
	var 最终指令 []byte

	if len(指令字符串) > 0 && 指令字符串[len(指令字符串)-1] != '\n' {
		最终指令 = append(S2B(指令字符串), '\n')
	} else {
		最终指令 = S2B(指令字符串)
	}

	安全投递 := func(通道 chan []byte, 指令 []byte) {
		select {
		case 通道 <- 指令:
		default:
		}
	}

	switch 命令目标 {
	case 发往主世界:
		安全投递(主世界命令通道, 最终指令)
	case 发往洞穴:
		安全投递(洞穴命令通道, 最终指令)
	case 发往全服:
		安全投递(主世界命令通道, 最终指令)
		安全投递(洞穴命令通道, 最终指令)
	}
}

var wg sync.WaitGroup

func 运行主世界(生命周期 context.Context, 终止生命周期 context.CancelFunc) {
	defer wg.Done()
	defer 终止生命周期()
	defer 全局配置.进程状态.主世界当前世代.Store(0)
	defer 初始化游戏内状态()

	if 全局配置.配置区2.洞穴世代同步链路 != "" {
		go 远端世代传感器(生命周期, 全局配置.配置区2.洞穴世代同步链路, 动作_洞穴崩溃, "master", &全局配置.原子锁.主世界就绪原子锁)
	}

	饥荒世界生命周期, 终止饥荒世界生命周期 := context.WithCancel(context.Background())
	defer 终止饥荒世界生命周期()
	主世界进程 := exec.CommandContext(饥荒世界生命周期, 游戏程序路径, append(全局配置.配置区1.通用启动参数, "-shard", "Master")...)
	主世界进程.Dir = 全局配置.配置区1.游戏程序目录
	绑定子进程生命周期(主世界进程)

	主世界输入, _ := 主世界进程.StdinPipe()

	主世界输出, _ := 主世界进程.StdoutPipe()
	go 实时输出流(节点_主世界, "[Master] ", 主世界输出, &全局配置.原子锁.主世界就绪原子锁)

	主世界错误, _ := 主世界进程.StderrPipe()
	go 实时输出流(节点_主世界_错误, "[Master_ERR] ", 主世界错误, nil)

	if err := 主世界进程.Start(); err != nil {
		控制台合并输出换行(S2B("[fatal] master boot failed: "), E2B(err))
		select {
		case 动作总线 <- 动作_主世界崩溃:
		default:
		}
		return
	} else {
		全局配置.进程状态.主世界当前世代.Store(time.Now().UnixNano())
	}
	设置进程退出信号(主世界进程)
	进程结束信号 := make(chan struct{})
	defer 杀掉进程(主世界进程, "Master", 进程结束信号, 终止饥荒世界生命周期, &全局配置.原子锁.主世界就绪原子锁)
	for {
		old := 全局配置.进程状态.PID.Load()
		newPID := (old & 0x00000000FFFFFFFF) | (uint64(uint32(主世界进程.Process.Pid)) << 32)
		if 全局配置.进程状态.PID.CompareAndSwap(old, newPID) {
			break
		}
	}

	defer func() {
		for {
			old := 全局配置.进程状态.PID.Load()
			newPID := old & 0x00000000FFFFFFFF
			if 全局配置.进程状态.PID.CompareAndSwap(old, newPID) {
				break
			}
		}
		全局配置.原子锁.主世界就绪原子锁.Store(false)
	}()

	debug.FreeOSMemory()

	go func() {
		清洗命令通道(主世界命令通道)
		全局配置.原子锁.主世界接收器存活.Store(true)
		defer 清洗命令通道(主世界命令通道)
		defer 全局配置.原子锁.主世界接收器存活.Store(false)
		go 主世界探针轮询器(生命周期)
		for {
			select {
			case <-生命周期.Done():
				return
			case 命令 := <-主世界命令通道:
				if 底层管道, 强转成功 := 主世界输入.(interface{ SetWriteDeadline(time.Time) error }); 强转成功 {
					底层管道.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
				}
				主世界输入.Write(命令)
			}
		}
	}()

	退出通道 := make(chan error, 1)
	go func() {
		退出通道 <- 主世界进程.Wait()
		close(进程结束信号)
	}()
	select {
	case <-生命周期.Done():
		控制台合并输出换行(S2B("[sys] master execution aborted by context cancel"))
		return
	case err := <-退出通道:
		控制台合并输出换行(S2B("[sys] master crashed or terminated: "), E2B(err))
		select {
		case 动作总线 <- 动作_主世界崩溃:
		default:
		}
		return
	}
}

func 运行洞穴(生命周期 context.Context, 终止生命周期 context.CancelFunc) {
	defer wg.Done()
	defer 终止生命周期()
	defer 全局配置.进程状态.洞穴当前世代.Store(0)
	if !全局配置.配置区2.启用主世界.Load() {
		defer 初始化游戏内状态()
	}

	if 全局配置.配置区2.主世界世代同步链路 != "" {
		go 远端世代传感器(生命周期, 全局配置.配置区2.主世界世代同步链路, 动作_主世界崩溃, "caves", &全局配置.原子锁.洞穴就绪原子锁)
	}

	饥荒世界生命周期, 终止饥荒世界生命周期 := context.WithCancel(context.Background())
	defer 终止饥荒世界生命周期()
	洞穴进程 := exec.CommandContext(饥荒世界生命周期, 游戏程序路径, append(全局配置.配置区1.通用启动参数, "-shard", "Caves")...)
	洞穴进程.Dir = 全局配置.配置区1.游戏程序目录
	绑定子进程生命周期(洞穴进程)

	洞穴输入, _ := 洞穴进程.StdinPipe()

	洞穴输出, _ := 洞穴进程.StdoutPipe()
	go 实时输出流(节点_洞穴, "[Caves] ", 洞穴输出, &全局配置.原子锁.洞穴就绪原子锁)

	洞穴错误, _ := 洞穴进程.StderrPipe()
	go 实时输出流(节点_洞穴_错误, "[Caves_ERR] ", 洞穴错误, nil)

	if err := 洞穴进程.Start(); err != nil {
		控制台合并输出换行(S2B("[fatal] caves boot failed: "), E2B(err))
		select {
		case 动作总线 <- 动作_洞穴崩溃:
		default:
		}
		return
	} else {
		全局配置.进程状态.洞穴当前世代.Store(time.Now().UnixNano())
	}
	设置进程退出信号(洞穴进程)
	进程结束信号 := make(chan struct{})
	defer 杀掉进程(洞穴进程, "Caves", 进程结束信号, 终止饥荒世界生命周期, &全局配置.原子锁.洞穴就绪原子锁)
	for {
		old := 全局配置.进程状态.PID.Load()
		newPID := (old & 0xFFFFFFFF00000000) | uint64(uint32(洞穴进程.Process.Pid))
		if 全局配置.进程状态.PID.CompareAndSwap(old, newPID) {
			break
		}
	}
	defer func() {
		for {
			old := 全局配置.进程状态.PID.Load()
			newPID := old & 0xFFFFFFFF00000000
			if 全局配置.进程状态.PID.CompareAndSwap(old, newPID) {
				break
			}
		}
		全局配置.原子锁.洞穴就绪原子锁.Store(false)
	}()

	debug.FreeOSMemory()

	go func() {
		清洗命令通道(洞穴命令通道)
		全局配置.原子锁.洞穴接收器存活.Store(true)
		defer 清洗命令通道(洞穴命令通道)
		defer 全局配置.原子锁.洞穴接收器存活.Store(false)
		go 洞穴探针轮询器(生命周期)
		for {
			select {
			case <-生命周期.Done():
				return
			case 命令 := <-洞穴命令通道:
				if 底层管道, 强转成功 := 洞穴输入.(interface{ SetWriteDeadline(time.Time) error }); 强转成功 {
					底层管道.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
				}

				洞穴输入.Write(命令)
			}

		}
	}()

	退出通道 := make(chan error, 1)
	go func() {
		退出通道 <- 洞穴进程.Wait()
		close(进程结束信号)
	}()
	select {
	case <-生命周期.Done():
		控制台合并输出换行(S2B("[sys] caves execution aborted by context cancel"))
		return
	case err := <-退出通道:
		控制台合并输出换行(S2B("[sys] caves crashed or terminated: "), E2B(err))
		select {
		case 动作总线 <- 动作_洞穴崩溃:
		default:
		}
		return
	}
}

func 远端世代传感器(生命周期 context.Context, 远端API string, 崩溃动作 核心执行动作, 自身名称 string, 就绪标记 *atomic.Bool) {
	var 记录的非零世代 int64 = 0

	// 为了防止一端重启，另一端因为网络刚好断了一下不知道，等知道了再重启，这边又因为网络刚好断了一下，对方不知道，等知道了再重启的无限重启，延迟只能缓解问题，不能保证极端情况一定不会出现
	秒表 := time.NewTicker(1 * time.Second)
	for i := 0; i < 15; i++ {
		if 就绪标记.Load() {
			break
		}
		select {
		case <-生命周期.Done():
			秒表.Stop()
			return
		case <-秒表.C:
		}
	}
	秒表.Stop()

	控制台合并输出换行(S2B("[sys] "), S2B(自身名称), S2B(" causal link arming: "), S2B(远端API))

	for {
		select {
		case <-生命周期.Done():
			return
		default:
		}

		请求生命周期, 掐断请求 := context.WithCancel(生命周期)

		req, err := http.NewRequestWithContext(请求生命周期, "GET", 远端API, nil)
		if err != nil {
			掐断请求()
			time.Sleep(2 * time.Second)
			continue
		}

		req.Header.Set("Accept", "text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			掐断请求()
			time.Sleep(2 * time.Second)
			continue
		}

		心跳信号 := make(chan struct{}, 1)
		go func() {
			看门狗 := time.NewTimer(5 * time.Second)
			defer 看门狗.Stop()
			for {
				select {
				case <-请求生命周期.Done():
					return
				case <-心跳信号:
					if !看门狗.Stop() {
						select {
						case <-看门狗.C:
						default:
						}
					}
					看门狗.Reset(5 * time.Second)
				case <-看门狗.C:
					控制台合并输出换行(S2B("[sys] "), S2B(自身名称), S2B(" sse heartbeat timeout (5s), dropping connection..."))
					掐断请求()
					return
				}
			}
		}()

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				resp.Body.Close()
				掐断请求()
				break
			}

			select {
			case 心跳信号 <- struct{}{}:
			default:
			}

			if bytes.HasPrefix(line, []byte("data: ")) {
				当前对方世代, err := strconv.ParseInt(string(bytes.TrimSpace(line[6:])), 10, 64)
				if err != nil {
					continue
				}

				if 当前对方世代 == 0 {
					if 记录的非零世代 != 0 {
						控制台合并输出换行(S2B("[sys] "), S2B(自身名称), S2B(" causal violation: remote epoch dropped to 0. trigger suicide."))
						select {
						case 动作总线 <- 崩溃动作:
						default:
						}
						resp.Body.Close()
						掐断请求()
						return
					}
				} else {
					if 记录的非零世代 == 0 {
						记录的非零世代 = 当前对方世代
						控制台合并输出换行(S2B("[sys] "), S2B(自身名称), S2B(" causal link locked to remote epoch: "), []byte(strconv.FormatInt(当前对方世代, 10)))
					} else if 记录的非零世代 != 当前对方世代 {
						控制台合并输出换行(S2B("[sys] "), S2B(自身名称), S2B(" causal violation: remote epoch mutated. trigger suicide."))
						select {
						case 动作总线 <- 崩溃动作:
						default:
						}
						resp.Body.Close()
						掐断请求()
						return
					}
				}
			}
		}
	}
}

func 主世界探针轮询器(生命周期 context.Context) {
	var 声呐探测指令 = []byte(`local w=rawget(_G,"TheWorld"); if type(w)=="table" and w.state then print("DST_NUCLEUS_PING") end` + "\n")

	怠速秒表 := time.NewTicker(15 * time.Second)

	for !全局配置.原子锁.主世界就绪原子锁.Load() {
		select {
		case <-生命周期.Done():

			return

		case <-怠速秒表.C:
			select {
			case 主世界命令通道 <- 声呐探测指令:
			default:
			}
		}
	}
	怠速秒表.Stop()

	if !全局配置.配置区2.启用寄生.Load() {
		return
	}

	看门狗 := time.NewTimer(10 * time.Second)
	defer 看门狗.Stop()

	for {
		select {
		case <-生命周期.Done():
			return

		case <-主世界寄生虫心跳:
			if !看门狗.Stop() {
				select {
				case <-看门狗.C:
				default:
				}
			}
			看门狗.Reset(10 * time.Second)

		case <-看门狗.C:
			select {
			case 主世界命令通道 <- 寄生指令:
			default:
			}

			看门狗.Reset(10 * time.Second)
		}
	}
}

func 洞穴探针轮询器(生命周期 context.Context) {
	var 声呐探测指令 = []byte(`local w=rawget(_G,"TheWorld"); if type(w)=="table" and w.state then print("DST_NUCLEUS_PING") end` + "\n")

	怠速秒表 := time.NewTicker(15 * time.Second)

	for !全局配置.原子锁.洞穴就绪原子锁.Load() {
		select {
		case <-生命周期.Done():

			return

		case <-怠速秒表.C:
			select {
			case 洞穴命令通道 <- 声呐探测指令:
			default:
			}
		}
	}
	怠速秒表.Stop()

	看门狗 := time.NewTimer(10 * time.Second)
	defer 看门狗.Stop()

	for {
		select {
		case <-生命周期.Done():
			return

		case <-洞穴寄生虫心跳:
			if !看门狗.Stop() {
				select {
				case <-看门狗.C:
				default:
				}
			}
			看门狗.Reset(10 * time.Second)

		case <-看门狗.C:
			select {
			case 洞穴命令通道 <- 寄生指令:
			default:
			}

			看门狗.Reset(10 * time.Second)
		}
	}
}

var 正在重启原子锁 atomic.Uint32
var 保存完成信号 = make(chan struct{}, 1)

func 执行优雅重启(生命周期 context.Context, 提示消息 string, 自定义等待时间 *uint32) {
	if !全局配置.原子锁.正在重启原子锁.CompareAndSwap(false, true) {
		return
	}
	defer 全局配置.原子锁.正在重启原子锁.Store(false)

	select {
	case <-保存完成信号:
	default:
	}

	主世界是否开启 := 全局配置.配置区2.启用主世界.Load()

	var i uint32
	if 自定义等待时间 != nil {
		if *自定义等待时间 == 0 {
			i = 0
		} else {
			i = *自定义等待时间
		}
	} else {
		i = uint32(全局配置.配置区2.优雅重启等待时间.Load())
	}

	发送纯文本指令(`if #GetPlayerClientTable() == 0 then c_shutdown(true) end`, 发往主世界)

	if i > 0 {
		发送纯文本指令(`c_save()`, 发往主世界)

		秒表 := time.NewTicker(1 * time.Second)
		defer 秒表.Stop()

		下一次播报 := i - (i % 5)

		for ; i > 0; i-- {
			if i == 下一次播报 {
				命令 := make([]byte, 0, 128)

				命令 = append(命令, "if #GetPlayerClientTable() == 0 then c_shutdown(true) else c_announce(\""...)
				命令 = append(命令, 提示消息...)
				命令 = append(命令, 公告_优雅重启上半...)
				命令 = strconv.AppendInt(命令, int64(i), 10)
				命令 = append(命令, 公告_优雅重启下半...)
				命令 = append(命令, "\") end\n"...)
				if 主世界是否开启 {
					select {
					case 主世界命令通道 <- 命令:
					default:
					}
				} else {
					select {
					case 洞穴命令通道 <- 命令:
					default:
					}
				}

				下一次播报 = 下一次播报 - 5
			}

			select {
			case <-生命周期.Done():
				return
			case <-秒表.C:
			}
		}
	}
	控制台合并输出换行(S2B("[core] dispatching final save"))
	发送纯文本指令(`c_save(); print("K_SAVED")`, 发往主世界)
	控制台合并输出换行(S2B("[core] waiting for save ack"))

	超时定时器 := time.NewTimer(10 * time.Second)
	defer 超时定时器.Stop()

	select {
	case <-生命周期.Done():
		return
	case <-保存完成信号:
		控制台合并输出换行(S2B("[core] save ack received, shutting down"))
	case <-超时定时器.C:
		控制台合并输出换行(S2B("[warn] save ack timeout (10s), forcing shutdown..."))
	}

	const 磁盘彻底落盘缓冲毫秒数 = 500
	select {
	case <-生命周期.Done():
	case <-time.After(磁盘彻底落盘缓冲毫秒数 * time.Millisecond):
	}
}

const (
	节点_主世界 uint8 = iota
	节点_洞穴
	节点_主世界_错误
	节点_洞穴_错误
	节点_外部工具
)

var (
	特征_模组过期   = []byte(`is out of date and needs to be updated for new users to be able to join the server`)
	特征_模拟暂停   = []byte("Sim paused")
	特征_保存完成   = []byte("K_SAVED")
	特征_玩家聊天前缀 = "KU_"
	回显污染头     = []byte("RemoteCommandInput")
	极速二进制协议头  = []byte{0xCE, 0xDF}
)

var 日志读取池 = sync.Pool{
	New: func() any {
		return bufio.NewReaderSize(nil, 256*1024)
	},
}

func 实时输出流(节点类型 uint8, 前缀 string, 读取器 io.Reader, 就绪标记 *atomic.Bool) {
	var 目标日志文件 string
	var 允许控制台输出 bool
	var 中央管道 chan *广播日志块
	var 连接数 *atomic.Int32

	switch 节点类型 {
	case 节点_主世界, 节点_主世界_错误:
		目标日志文件 = 全局配置.日志状态.主世界日志路径
		允许控制台输出 = 全局配置.日志状态.主世界输出.Load()
		中央管道 = 主世界中央日志管道
		连接数 = &主世界日志连接数
	case 节点_洞穴, 节点_洞穴_错误:
		目标日志文件 = 全局配置.日志状态.洞穴日志路径
		允许控制台输出 = 全局配置.日志状态.洞穴输出.Load()
		中央管道 = 洞穴中央日志管道
		连接数 = &洞穴日志连接数
	case 节点_外部工具:
		目标日志文件 = ""
		允许控制台输出 = true
	}
	var 写入的文件 *os.File
	if 目标日志文件 != "" {
		写入的文件, _ = os.OpenFile(目标日志文件, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if 写入的文件 != nil {
			defer 写入的文件.Close()
		}
	}

	安全读取器 := 日志读取池.Get().(*bufio.Reader)

	安全读取器.Reset(读取器)

	defer func() {
		安全读取器.Reset(nil)
		日志读取池.Put(安全读取器)
	}()

	for {
		行字节, 截断标志, err := 安全读取器.ReadLine()
		if err != nil {
			break
		}
		if 截断标志 {
			控制台合并输出换行(S2B(前缀), S2B("[pipe] toxic log >256KB truncated"))

			for 截断标志 {
				_, 截断标志, err = 安全读取器.ReadLine()
				if err != nil {
					return
				}
			}

			continue
		}

		if bytes.Contains(行字节, 回显污染头) && bytes.Contains(行字节, S2B("NUCLEUS_ACTIVATE")) {
			continue
		}

		if 位置 := bytes.Index(行字节, 极速二进制协议头); 位置 != -1 {
			解析二进制包(行字节[位置+2:])

			switch 节点类型 {
			case 节点_主世界:
				select {
				case 主世界寄生虫心跳 <- struct{}{}:
				default:
				}
			case 节点_洞穴:
				select {
				case 洞穴寄生虫心跳 <- struct{}{}:
				default:
				}
			}

			continue
		}

		if 允许控制台输出 {
			控制台合并输出换行(S2B(前缀), 行字节)
		}
		if 写入的文件 != nil {
			写入的文件.Write(行字节)
			写入的文件.Write(控制台换行符)
		}

		if 连接数 != nil && 连接数.Load() > 0 {
			块 := 日志广播池.Get().(*广播日志块)
			块.数据 = append(块.数据[:0], "data: "...)
			块.数据 = append(块.数据, 行字节...)
			块.数据 = append(块.数据, "\n\n"...)
			块.引用数.Store(1)

			select {
			case 中央管道 <- 块:
			default:
				日志广播池.Put(块)
			}
		}

		if bytes.Contains(行字节, 回显污染头) {
			continue
		}

		if 就绪标记 != nil && !就绪标记.Load() {
			if bytes.Contains(行字节, 特征_模拟暂停) {
				就绪标记.Store(true)
				控制台合并输出换行(S2B(前缀), S2B("sim paused, server ready"))
			}
			if bytes.Contains(行字节, S2B("DST_NUCLEUS_PING")) {
				就绪标记.Store(true)
				控制台合并输出换行(S2B(前缀), S2B("sonar ack received, server ready"))
			}
		}

		if 节点类型 == 节点_主世界 {
			if bytes.Contains(行字节, 特征_保存完成) {
				select {
				case 保存完成信号 <- struct{}{}:
				default:
				}
			}

			if 正在重启原子锁.Load() == 1 {
				continue
			}

			位置 := bytes.Index(行字节, 特征_模组过期)
			if 位置 != -1 {
				前缀部分 := 行字节[:位置]
				if bytes.Contains(前缀部分, S2B(特征_玩家聊天前缀)) {
					控制台合并输出换行(S2B("[sys] ignoring chat message"))
					continue
				}
				select {
				case 动作总线 <- 动作_执行模组热更新:
				default:
				}
			}
		}
	}
}

func 杀掉进程(cmd *exec.Cmd, 进程名称 string, 死亡传感器 <-chan struct{}, 终止生命周期 context.CancelFunc, 就绪标记 *atomic.Bool) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	if !就绪标记.Load() {
		终止生命周期()
		<-死亡传感器
		控制台合并输出换行(S2B("[core] "), S2B(进程名称), S2B(" has been purged from the OS."))
	}

	select {
	case <-死亡传感器:
		return
	default:
	}

	err := cmd.Process.Signal(os.Interrupt)
	if err != nil {
		控制台合并输出换行(S2B("[warn] SIGINT failed, deploying SIGKILL immediately..."))
		终止生命周期()
		<-死亡传感器
	}

	超时秒表 := time.NewTimer(5 * time.Second)
	defer 超时秒表.Stop()

	select {
	case <-死亡传感器:
		控制台合并输出换行(S2B("[core] "), S2B(进程名称), S2B(" gracefully saved and terminated."))
	case <-超时秒表.C:
		控制台合并输出换行(S2B("[warn] "), S2B(进程名称), S2B(" shutdown timeout (5s)! deploying SIGKILL..."))
		终止生命周期()
		<-死亡传感器
		控制台合并输出换行(S2B("[core] "), S2B(进程名称), S2B(" has been purged from the OS."))
	}
}

func 清洗命令通道(通道 chan []byte) {
	for {
		select {
		case <-通道:
		default:
			return
		}
	}
}
