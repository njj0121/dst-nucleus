//go:build amd64 && windows

package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	ENABLE_QUICK_EDIT_MODE = 0x0040
	ENABLE_EXTENDED_FLAGS  = 0x0080
)

const 饥荒可执行文件名 = `dontstarve_dedicated_server_nullrenderer_x64.exe`
const steamcmd下载链接 = `https://steamcdn-a.akamaihd.net/client/installer/steamcmd.zip`
const steamcmd文件名 = "steamcmd.exe"
const steamcmd默认路径 = `C:\steamcmd`

var 标准输出句柄 syscall.Handle

const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000

const (
	作业信息类_扩展限制 = 9
	随父进程退出标志   = 0x2000
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject = kernel32.NewProc("SetInformationJobObject")
)

var 作业组句柄 uintptr

func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")
	setConsoleOutputCP.Call(uintptr(65001))
	句柄, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err == nil {
		标准输出句柄 = 句柄
	}
	h, _, _ := procCreateJobObject.Call(0, 0)
	作业组句柄 = h
	type 扩展限制结构体 struct {
		_    [48]byte
		自裁标志 uint32
		_    [16]byte
		_    [96]byte
	}
	var 信息 扩展限制结构体
	信息.自裁标志 = 随父进程退出标志
	procSetInformationJobObject.Call(
		作业组句柄,
		作业信息类_扩展限制,
		uintptr(unsafe.Pointer(&信息)),
		uintptr(unsafe.Sizeof(信息)),
	)

}

var 控制台换行符 = []byte{'\r', '\n'}
var 输出缓冲池 = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &b
	},
}

func 控制台合并输出(碎片组 ...[]byte) {
	if 标准输出句柄 == 0 {
		return
	}

	池指针 := 输出缓冲池.Get().(*[]byte)
	buf := (*池指针)[:0]

	for _, 碎片 := range 碎片组 {
		if len(碎片) > 0 {
			buf = append(buf, 碎片...)
		}
	}

	if len(buf) > 0 {
		var 实际写入数量 uint32
		syscall.WriteFile(标准输出句柄, buf, &实际写入数量, nil)
	}

	if cap(buf) <= 64*1024 {
		*池指针 = buf
		输出缓冲池.Put(池指针)
	}
}

func 控制台合并输出换行(碎片组 ...[]byte) {
	if 标准输出句柄 == 0 {
		return
	}

	池指针 := 输出缓冲池.Get().(*[]byte)
	buf := (*池指针)[:0]

	for _, 碎片 := range 碎片组 {
		if len(碎片) > 0 {
			buf = append(buf, 碎片...)
		}
	}

	buf = append(buf, 控制台换行符...)

	var 实际写入数量 uint32
	syscall.WriteFile(标准输出句柄, buf, &实际写入数量, nil)

	if cap(buf) <= 64*1024 {
		*池指针 = buf
		输出缓冲池.Put(池指针)
	}
}

var SteamCMD下载缓冲池 = sync.Pool{
	New: func() any {
		b := make([]byte, 2*1024*1024)
		return &b
	},
}

func 安装SteamCMD(目标目录 string) uint {
	if err := os.MkdirAll(目标目录, 0755); err != nil {
		控制台合并输出换行(S2B("[sys] steamcmd_setup: mkdir failed: "), E2B(err))
		return 128
	}

	控制台合并输出换行(S2B("[init] steamcmd_setup: pulling payload..."))

	响应, err := http.Get(steamcmd下载链接)
	if err != nil {
		控制台合并输出换行(S2B("[sys] steamcmd_setup: tcp request failed: "), E2B(err))
		return 129
	}
	defer 响应.Body.Close()

	if 响应.StatusCode != 200 {
		控制台合并输出换行(S2B("[sys] steamcmd_setup: payload download rejected (status != 200)"))
		return 130
	}

	缓冲指针 := SteamCMD下载缓冲池.Get().(*[]byte)

	defer func() {
		if cap(*缓冲指针) <= 2*1024*1024 {
			SteamCMD下载缓冲池.Put(缓冲指针)
		}
	}()

	下载数据 := (*缓冲指针)[:0]

	for {
		if len(下载数据) == cap(下载数据) {
			新容量 := cap(下载数据) * 2
			if 新容量 > 20*1024*1024 {
				新容量 = 20*1024*1024 + 1
			}
			新缓冲 := make([]byte, len(下载数据), 新容量)
			copy(新缓冲, 下载数据)
			下载数据 = 新缓冲
			*缓冲指针 = 下载数据
		}

		n, err := 响应.Body.Read(下载数据[len(下载数据):cap(下载数据)])
		下载数据 = 下载数据[:len(下载数据)+n]

		if len(下载数据) > 20*1024*1024 {
			控制台合并输出换行(S2B("[fatal] steamcmd_setup: mitm defense triggered. payload >20MB. aborting tcp."))
			return 139
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			控制台合并输出换行(S2B("[sys] steamcmd_setup: tcp stream read failed: "), E2B(err))
			return 131
		}
	}

	内存读取器 := bytes.NewReader(下载数据)
	zip读取器, err := zip.NewReader(内存读取器, int64(len(下载数据)))
	if err != nil {
		控制台合并输出换行(S2B("[sys] steamcmd_setup: zip parsing failed: "), E2B(err))
		return 132
	}

	绝对目标目录 := filepath.Clean(目标目录) + string(os.PathSeparator)

	控制台合并输出换行(S2B("[init] steamcmd_setup: extracting payload..."))

	var 已解压总量 int64 = 0
	const 解压熔断阈值 int64 = 20 * 1024 * 1024

	for _, 文件 := range zip读取器.File {
		文件路径 := filepath.Join(目标目录, 文件.Name)

		if !strings.HasPrefix(文件路径, 绝对目标目录) {
			控制台合并输出换行(S2B("[fatal] steamcmd_setup: path traversal payload detected, aborted: "), S2B(文件路径))
			return 133
		}

		if 文件.FileInfo().IsDir() {
			os.MkdirAll(文件路径, os.ModePerm)
			continue
		}

		if err = os.MkdirAll(filepath.Dir(文件路径), os.ModePerm); err != nil {
			控制台合并输出换行(S2B("[sys] steamcmd_setup: mkdir failed: "), E2B(err))
			return 134
		}

		输出文件, err := os.OpenFile(文件路径, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 文件.Mode())
		if err != nil {
			控制台合并输出换行(S2B("[sys] steamcmd_setup: fd create failed: "), E2B(err))
			return 135
		}

		压缩内容, err := 文件.Open()
		if err != nil {
			输出文件.Close()
			控制台合并输出换行(S2B("[sys] steamcmd_setup: zip entry open failed: "), E2B(err))
			return 136
		}

		限流读取器 := io.LimitReader(压缩内容, 解压熔断阈值-已解压总量+1)
		写入字节数, err := io.Copy(输出文件, 限流读取器)

		已解压总量 += 写入字节数

		输出文件.Close()
		压缩内容.Close()

		if 已解压总量 > 解压熔断阈值 {
			控制台合并输出换行(S2B("[fatal] steamcmd_setup: zip bomb triggered (>20MB). aborting & wiping disk."))
			os.RemoveAll(目标目录)
			return 140
		}

		if err != nil {
			控制台合并输出换行(S2B("[sys] steamcmd_setup: fd write failed: "), E2B(err))
			return 137
		}
	}

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
	ps脚本 := `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $path = (Get-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders').Personal; [Environment]::ExpandEnvironmentVariables($path)`

	命令 := exec.Command("powershell", "-NoProfile", "-Command", ps脚本)
	输出, err := 命令.Output()

	真实的文档路径 := ""

	if err == nil {
		真实的文档路径 = strings.TrimSpace(string(输出))
	}

	if 真实的文档路径 == "" {
		home, _ := os.UserHomeDir()
		真实的文档路径 = filepath.Join(home, "Documents")
	}

	return filepath.Join(真实的文档路径, "Klei")
}

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetProcessTimes         = modkernel32.NewProc("GetProcessTimes")
	procGetSystemTimeAsFileTime = modkernel32.NewProc("GetSystemTimeAsFileTime")
	procOpenProcess             = modkernel32.NewProc("OpenProcess")
	procGetProcessMemoryInfo    = modkernel32.NewProc("K32GetProcessMemoryInfo")
)

type 探针私有快照 struct {
	_            [64]byte
	MasterKernel uint64
	MasterUser   uint64
	CavesKernel  uint64
	CavesUser    uint64
	上次系统时间       uint64
	_            [64]byte
}

type PROCESS_MEMORY_COUNTERS struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

var 上次监控快照 探针私有快照

func 全服资源探针任务() {
	合体PID := 全局配置.进程状态.PID.Load()
	if 合体PID == 0 {
		return
	}

	masterPID := uint32(合体PID >> 32)
	cavesPID := uint32(合体PID & 0xFFFFFFFF)

	var 系统时间 uint64
	procGetSystemTimeAsFileTime.Call(uintptr(unsafe.Pointer(&系统时间)))
	时间差 := 系统时间 - 上次监控快照.上次系统时间
	if 时间差 <= 0 {
		时间差 = 1
	}

	if masterPID > 0 {
		cpu, mem, k, u := 采集Win32数据(masterPID, 上次监控快照.MasterKernel, 上次监控快照.MasterUser, 时间差)
		全局配置.全服监控态.MasterCPU.Store(cpu)
		全局配置.全服监控态.MasterMem.Store(uint64(mem))
		上次监控快照.MasterKernel = k
		上次监控快照.MasterUser = u
	}

	if cavesPID > 0 {
		cpu, mem, k, u := 采集Win32数据(cavesPID, 上次监控快照.CavesKernel, 上次监控快照.CavesUser, 时间差)
		全局配置.全服监控态.CavesCPU.Store(cpu)
		全局配置.全服监控态.CavesMem.Store(uint64(mem))
		上次监控快照.CavesKernel = k
		上次监控快照.CavesUser = u
	}

	上次监控快照.上次系统时间 = 系统时间
}

func 采集Win32数据(pid uint32, 上次K, 上次U, 系统时间差 uint64) (万分比CPU uint32, 字节Mem uintptr, 新K, 新U uint64) {
	h, _, _ := procOpenProcess.Call(0x1000, 0, uintptr(pid))
	if h == 0 {
		return
	}
	defer syscall.CloseHandle(syscall.Handle(h))

	var c, e, k, u uint64
	ret, _, _ := procGetProcessTimes.Call(h, uintptr(unsafe.Pointer(&c)), uintptr(unsafe.Pointer(&e)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))

	if ret != 0 {
		新K, 新U = k, u
		if 上次K > 0 || 上次U > 0 {
			进程差 := (k - 上次K) + (u - 上次U)
			万分比CPU = uint32((进程差 * 10000) / 系统时间差)
		}
	}

	var 内存计 PROCESS_MEMORY_COUNTERS
	内存计.CB = uint32(unsafe.Sizeof(内存计))

	retMem, _, _ := procGetProcessMemoryInfo.Call(h, uintptr(unsafe.Pointer(&内存计)), uintptr(内存计.CB))
	if retMem != 0 {
		字节Mem = 内存计.WorkingSetSize
	}

	return
}

func 底层监听(端口 string, 路由器 http.Handler) error {
	网络协议 := "tcp"
	if strings.HasSuffix(端口, ".sock") {
		网络协议 = "unix"
		os.Remove(端口)
	}

	监听器, err := net.Listen(网络协议, 端口)
	if err != nil {
		return err
	}

	return http.Serve(监听器, 路由器)
}

var (
	createJobObject          = kernel32.NewProc("CreateJobObjectW")
	setInfoJobObject         = kernel32.NewProc("SetInformationJobObject")
	assignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
)

const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x2000
	processAssignAccess               = 0x0101
)

type jobObjectExtendedLimitInformationStruct struct {
	BasicLimitInformation struct {
		PerProcessUserTimeLimit int64
		PerJobUserTimeLimit     int64
		LimitFlags              uint32
		MinimumWorkingSetSize   uintptr
		MaximumWorkingSetSize   uintptr
		ActiveProcessLimit      uint32
		Affinity                uintptr
		PriorityClass           uint32
		SchedulingClass         uint32
	}
	IoInfo struct {
		ReadOperationCount  uint64
		WriteOperationCount uint64
		OtherOperationCount uint64
		ReadTransferCount   uint64
		WriteTransferCount  uint64
		OtherTransferCount  uint64
	}
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

func 绑定子进程生命周期(cmd *exec.Cmd) {
}

func 设置进程退出信号(cmd *exec.Cmd) {
	jobHandle, _, _ := createJobObject.Call(0, 0)
	if jobHandle == 0 {
		return
	}
	job := syscall.Handle(jobHandle)

	var info jobObjectExtendedLimitInformationStruct
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose

	ret, _, _ := setInfoJobObject.Call(
		uintptr(job),
		uintptr(jobObjectExtendedLimitInformation),
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if ret == 0 {
		syscall.CloseHandle(job)
		return
	}

	procHandle, err := syscall.OpenProcess(
		processAssignAccess,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		syscall.CloseHandle(job)
		return
	}

	assignProcessToJobObject.Call(uintptr(job), uintptr(procHandle))
	syscall.CloseHandle(procHandle)

	go func() {
		cmd.Process.Wait()
		syscall.CloseHandle(job)
	}()
}

func 检查linux运行环境(_ bool, _ bool) bool {
	return true
}
