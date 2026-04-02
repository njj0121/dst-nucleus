//go:build amd64 && linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const 饥荒可执行文件名 = `dontstarve_dedicated_server_nullrenderer_x64`
const steamcmd下载链接 = `https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz`
const steamcmd文件名 = "steamcmd.sh"

var steamcmd默认路径 = filepath.Join(os.Getenv("HOME"), "Steam")

var (
	探针缓冲池 = sync.Pool{
		New: func() any {
			b := make([]byte, 1024)
			return &b
		},
	}
)

var 平台专属属性 *syscall.SysProcAttr

func init() {
	平台专属属性 = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
	}
}

var 控制台换行符 = []byte{'\n'}
var 全局向量缓存 [64]syscall.Iovec

func 控制台合并输出(碎片组 ...[]byte) {
	输出阻塞锁.Lock()
	defer 输出阻塞锁.Unlock()

	var 实际向量数 int
	for _, 碎片 := range 碎片组 {
		长度 := len(碎片)
		if 长度 > 0 {
			全局向量缓存[实际向量数].Base = &碎片[0]
			全局向量缓存[实际向量数].SetLen(长度)
			实际向量数++
		}
	}
	if 实际向量数 == 0 {
		return
	}
	syscall.RawSyscall(
		syscall.SYS_WRITEV,
		uintptr(1),
		uintptr(unsafe.Pointer(&全局向量缓存[0])),
		uintptr(实际向量数),
	)
}

func 控制台合并输出换行(碎片组 ...[]byte) {
	输出阻塞锁.Lock()
	defer 输出阻塞锁.Unlock()

	var 实际向量数 int
	for _, 碎片 := range 碎片组 {
		长度 := len(碎片)
		if 长度 > 0 {
			全局向量缓存[实际向量数].Base = &碎片[0]
			全局向量缓存[实际向量数].SetLen(长度)
			实际向量数++
		}
	}
	全局向量缓存[实际向量数].Base = &控制台换行符[0]
	全局向量缓存[实际向量数].SetLen(len(控制台换行符))
	实际向量数++
	syscall.RawSyscall(
		syscall.SYS_WRITEV,
		uintptr(1),
		uintptr(unsafe.Pointer(&全局向量缓存[0])),
		uintptr(实际向量数),
	)
}

func 检查linux运行环境(跳过自检 bool, 跳过root bool) bool {
	if os.Getuid() == 0 {
		if !跳过root {
			控制台合并输出换行(S2B("[fatal] root privilege detected. lua sandbox breakout risk."))
			控制台合并输出换行(S2B("[info] set 'permit_root_usage: true' in config to bypass this security lock."))
			return true
		}
	}
	if 跳过自检 {
		return true
	}

	const 加载器路径 = `/lib/ld-linux.so.2`

	_, err := os.Stat(加载器路径)
	加载器存在 := !os.IsNotExist(err)

	if !加载器存在 {
		控制台合并输出换行(S2B("[fatal] missing 32-bit ld-linux.so.2 loader. steamcmd requires lib32gcc-s1."))
		控制台合并输出换行(S2B("[info] ubuntu: sudo dpkg --add-architecture i386 && sudo apt update && sudo apt install lib32gcc-s1"))
		控制台合并输出换行(S2B("[info] debian: sudo dpkg --add-architecture i386 && sudo apt update && sudo apt install lib32gcc-s1"))
		控制台合并输出换行(S2B("[info] set 'skip_linux_lib32_check: true' in config if you know what you are doing."))
		return false
	}
	return true
}

var SteamCMD解压缓冲池_Linux = sync.Pool{
	New: func() any {
		b := make([]byte, 64*1024)
		return &b
	},
}

func 安装SteamCMD(目标目录 string) uint8 {
	if err := os.MkdirAll(目标目录, 0755); err != nil {
		控制台合并输出换行(S2B("[sys] steamcmd_setup: mkdir failed: "), E2B(err))
		return 128
	}

	控制台合并输出换行(S2B("[init] steamcmd_setup: pulling payload..."))

	客户端 := &http.Client{
		Timeout: 5 * time.Minute,
	}

	响应, err := 客户端.Get(steamcmd下载链接)
	if err != nil {
		控制台合并输出换行(S2B("[sys] steamcmd_setup: tcp request failed: "), E2B(err))
		return 129
	}
	defer 响应.Body.Close()

	if 响应.StatusCode != 200 {
		控制台合并输出换行(S2B("[sys] steamcmd_setup: payload download rejected (status != 200)"))
		return 130
	}

	解压流, err := gzip.NewReader(响应.Body)
	if err != nil {
		控制台合并输出换行(S2B("[sys] steamcmd_setup: gzip reader init failed: "), E2B(err))
		return 131
	}
	defer 解压流.Close()

	缓冲指针 := SteamCMD解压缓冲池_Linux.Get().(*[]byte)
	defer SteamCMD解压缓冲池_Linux.Put(缓冲指针)

	归档读取器 := tar.NewReader(解压流)
	绝对目标目录 := filepath.Clean(目标目录) + string(os.PathSeparator)

	var 已解压总量 int64 = 0
	const 解压熔断阈值 int64 = 20 * 1024 * 1024

	for {
		头部, err := 归档读取器.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			控制台合并输出换行(S2B("[sys] steamcmd_setup: tar stream corrupted: "), E2B(err))
			return 132
		}

		解压目标 := filepath.Join(目标目录, 头部.Name)

		if !strings.HasPrefix(解压目标, 绝对目标目录) {
			控制台合并输出换行(S2B("[fatal] steamcmd_setup: path traversal payload detected, aborted: "), E2B(err))
			return 133
		}

		switch 头部.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(解压目标, 0755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(解压目标), 0755); err != nil {
				控制台合并输出换行(S2B("[sys] steamcmd_setup: mkdir failed: "), E2B(err))
				return 134
			}

			输出文件, err := os.OpenFile(解压目标, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(头部.Mode))
			if err != nil {
				控制台合并输出换行(S2B("[sys] steamcmd_setup: fd create failed: "), E2B(err))
				return 135
			}

			限流读取器 := io.LimitReader(归档读取器, 解压熔断阈值-已解压总量+1)

			写入字节数, copyErr := io.CopyBuffer(输出文件, 限流读取器, *缓冲指针)

			已解压总量 += 写入字节数

			closeErr := 输出文件.Close()

			if 已解压总量 > 解压熔断阈值 {
				控制台合并输出换行(S2B("[fatal] steamcmd_setup: tar bomb triggered (>20MB). aborting tcp & wiping disk."))
				os.RemoveAll(目标目录)
				return 140
			}

			if copyErr != nil {
				控制台合并输出换行(S2B("[sys] steamcmd_setup: buffer flush failed: "), E2B(copyErr))
				return 136
			}
			if closeErr != nil {
				控制台合并输出换行(S2B("[sys] steamcmd_setup: fd close failed: "), E2B(closeErr))
				return 137
			}
		}
	}

	控制台合并输出换行(S2B("[init] steamcmd_setup: tarball extracted."))
	控制台合并输出换行(S2B("[init] steamcmd_setup: executing dry-run to bootstrap environment..."))

	程序路径 := filepath.Join(目标目录, steamcmd文件名)
	命令 := exec.Command(程序路径, "+quit")

	命令.Stdout = os.Stdout
	命令.Stderr = os.Stderr

	if err := 命令.Run(); err != nil {
		控制台合并输出换行(S2B("[sys] steamcmd_setup: dry-run failed: "), E2B(err))
		return 138
	}

	控制台合并输出换行(S2B("[init] steamcmd_setup: deployment complete."))
	return 0
}

func 获取默认存档根目录() string {
	home目录, err := os.UserHomeDir()
	if err != nil {
		return "/root/.klei"
	}

	return filepath.Join(home目录, ".klei")
}

type 探针私有快照 struct {
	_           [64]byte
	MasterTicks uint64
	CavesTicks  uint64
	上次时间点       int64
	_           [64]byte
}

var 上次监控快照 探针私有快照

func 全服资源探针任务() {
	合体PID := 全局配置.进程状态.PID.Load()
	if 合体PID == 0 {
		return
	}

	masterPID := int32(合体PID >> 32)
	cavesPID := int32(合体PID & 0xFFFFFFFF)

	缓冲指针 := 探针缓冲池.Get().(*[]byte)
	defer 探针缓冲池.Put(缓冲指针)
	缓冲 := *缓冲指针

	现在 := time.Now().UnixNano()
	时间差纳秒 := 现在 - 上次监控快照.上次时间点
	if 时间差纳秒 <= 0 {
		时间差纳秒 = 1
	}
	var 新MasterTicks, 新CavesTicks uint64

	if masterPID > 0 {
		cpu, mem, 当前Ticks := 采集单进程数据(masterPID, 缓冲, 上次监控快照.MasterTicks, 时间差纳秒)
		全局配置.全服监控态.MasterCPU.Store(cpu)
		全局配置.全服监控态.MasterMem.Store(mem)
		新MasterTicks = 当前Ticks
	}

	if cavesPID > 0 {
		cpu, mem, 当前Ticks := 采集单进程数据(cavesPID, 缓冲, 上次监控快照.CavesTicks, 时间差纳秒)
		全局配置.全服监控态.CavesCPU.Store(cpu)
		全局配置.全服监控态.CavesMem.Store(mem)
		新CavesTicks = 当前Ticks
	}
	上次监控快照.MasterTicks = 新MasterTicks
	上次监控快照.CavesTicks = 新CavesTicks
	上次监控快照.上次时间点 = 现在

}

func 采集单进程数据(pid int32, 缓冲 []byte, 上次滴答 uint64, 纳秒差 int64) (万分比CPU uint32, 字节Mem uint64, 当前滴答 uint64) {
	var (
		fd   uintptr
		n    uintptr
		err1 syscall.Errno
	)

	拼接进程路径(缓冲, pid, "/statm")
	fd, _, err1 = syscall.RawSyscall6(syscall.SYS_OPENAT, uintptr(0xffffff9c), uintptr(unsafe.Pointer(&缓冲[0])), uintptr(syscall.O_RDONLY), 0, 0, 0)

	if err1 == 0 {
		n, _, err1 = syscall.RawSyscall(syscall.SYS_READ, fd, uintptr(unsafe.Pointer(&缓冲[0])), uintptr(len(缓冲)))
		syscall.RawSyscall(syscall.SYS_CLOSE, fd, 0, 0)

		if n > 0 {
			游标 := 0
			读取长度 := int(n)
			for 游标 < 读取长度 && 缓冲[游标] != ' ' {
				游标++
			}
			for 游标 < 读取长度 && 缓冲[游标] == ' ' {
				游标++
			}

			var rss uint64
			for 游标 < 读取长度 && 缓冲[游标] >= '0' && 缓冲[游标] <= '9' {
				rss = rss*10 + uint64(缓冲[游标]-'0')
				游标++
			}
			字节Mem = rss * 4096
		}
	}

	拼接进程路径(缓冲, pid, "/stat")
	fd, _, err1 = syscall.RawSyscall6(syscall.SYS_OPENAT, uintptr(0xffffff9c), uintptr(unsafe.Pointer(&缓冲[0])), uintptr(syscall.O_RDONLY), 0, 0, 0)

	if err1 == 0 {
		n, _, err1 = syscall.RawSyscall(syscall.SYS_READ, fd, uintptr(unsafe.Pointer(&缓冲[0])), uintptr(len(缓冲)))
		syscall.RawSyscall(syscall.SYS_CLOSE, fd, 0, 0)

		if err1 == 0 && n > 0 {
			有效数据 := 缓冲[:n]
			utime := 提取stat字段(有效数据, 12)
			stime := 提取stat字段(有效数据, 13)
			当前总滴答 := utime + stime
			当前滴答 = 当前总滴答

			if 上次滴答 > 0 && 当前总滴答 >= 上次滴答 {
				滴答差 := 当前总滴答 - 上次滴答
				万分比CPU = uint32((滴答差 * 100000000000) / uint64(纳秒差))
			}
		}
	}

	return 万分比CPU, 字节Mem, 当前滴答
}

func 拼接进程路径(缓冲 []byte, pid int32, 后缀 string) {
	copy(缓冲[0:6], "/proc/")
	游标 := 6

	var 临时 [16]byte
	i := 15
	for n := uint32(pid); n > 0; n /= 10 {
		临时[i] = byte('0' + (n % 10))
		i--
	}
	长度 := 15 - i
	copy(缓冲[游标:], 临时[i+1:])
	游标 += 长度

	copy(缓冲[游标:], 后缀)
	游标 += len(后缀)

	缓冲[游标] = 0
}

func 提取stat字段(数据 []byte, 目标索引 int) uint64 {
	括号结束 := bytes.LastIndexByte(数据, ')')
	if 括号结束 == -1 {
		return 0
	}

	当前索引 := 0
	游标 := 括号结束 + 2
	var 结果 uint64

	for 游标 < len(数据) {
		if 数据[游标] == ' ' {
			游标++
			continue
		}

		当前索引++
		if 当前索引 == 目标索引 {
			for 游标 < len(数据) && 数据[游标] >= '0' && 数据[游标] <= '9' {
				结果 = 结果*10 + uint64(数据[游标]-'0')
				游标++
			}
			return 结果
		}

		for 游标 < len(数据) && 数据[游标] != ' ' {
			游标++
		}
	}
	return 0
}

func 底层监听(接口地址 string, 路由器 http.Handler) error {
	var 协议, 实际路径 string
	var 启用负载均衡 bool

	if strings.HasPrefix(接口地址, "/") || strings.HasPrefix(接口地址, "./") || strings.HasSuffix(接口地址, ".sock") {
		协议 = "unix"
		实际路径 = 接口地址
		启用负载均衡 = false
		syscall.Unlink(实际路径)
	} else {
		协议 = "tcp"
		实际路径 = 接口地址
		启用负载均衡 = true
	}

	核心数 := runtime.NumCPU()
	if !启用负载均衡 {
		核心数 = 1
	}

	错误通道 := make(chan error, 核心数)

	for i := 0; i < 核心数; i++ {
		go func() {
			配置 := net.ListenConfig{
				Control: func(network, address string, c syscall.RawConn) error {
					var err error
					c.Control(func(fd uintptr) {
						if 启用负载均衡 {
							err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 15, 1)
						}
					})
					return err
				},
			}

			底层监听器, err := 配置.Listen(context.Background(), 协议, 实际路径)
			if err != nil {
				错误通道 <- err
				return
			}

			if 协议 == "unix" {
				os.Chmod(实际路径, 0666)
			}

			错误通道 <- http.Serve(底层监听器, 路由器)
		}()
	}

	return <-错误通道
}

func 绑定子进程生命周期(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

func 设置进程退出信号(cmd *exec.Cmd) {
}
