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

const DSTExecName = `dontstarve_dedicated_server_nullrenderer_x64.exe`
const SteamCmdUrl = `https://steamcdn-a.akamaihd.net/client/installer/steamcmd.zip`
const SteamCmdName = "steamcmd.exe"
const SteamCmdDefPath = `C:\steamcmd`

var StdoutHandle syscall.Handle

const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000

const (
	JobObjectExtLimit = 9
	PdeathSigFlag     = 0x2000
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject = kernel32.NewProc("SetInformationJobObject")
)

var JobGroupHandle uintptr

func init() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleOutputCP := kernel32.NewProc("SetConsoleOutputCP")
	setConsoleOutputCP.Call(uintptr(65001))
	vHandle, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err == nil {
		StdoutHandle = vHandle
	}
	h, _, _ := procCreateJobObject.Call(0, 0)
	JobGroupHandle = h
	type ExtLimitStruct struct {
		_           [48]byte
		SuicideFlag uint32
		_           [16]byte
		_           [96]byte
	}
	var Info ExtLimitStruct
	Info.SuicideFlag = PdeathSigFlag
	procSetInformationJobObject.Call(
		JobGroupHandle,
		JobObjectExtLimit,
		uintptr(unsafe.Pointer(&Info)),
		uintptr(unsafe.Sizeof(Info)),
	)

}

var ConsoleNL = []byte{'\r', '\n'}
var OutputBufferPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &b
	},
}

func LogOut(FragGroup ...[]byte) {
	if StdoutHandle == 0 {
		return
	}

	poolPtr := OutputBufferPool.Get().(*[]byte)
	buf := (*poolPtr)[:0]

	for _, Frag := range FragGroup {
		if len(Frag) > 0 {
			buf = append(buf, Frag...)
		}
	}

	if len(buf) > 0 {
		var ActualWritten uint32
		syscall.WriteFile(StdoutHandle, buf, &ActualWritten, nil)
	}

	if cap(buf) <= 64*1024 {
		*poolPtr = buf
		OutputBufferPool.Put(poolPtr)
	}
}

func LogOutLn(FragGroup ...[]byte) {
	if StdoutHandle == 0 {
		return
	}

	poolPtr := OutputBufferPool.Get().(*[]byte)
	buf := (*poolPtr)[:0]

	for _, Frag := range FragGroup {
		if len(Frag) > 0 {
			buf = append(buf, Frag...)
		}
	}

	buf = append(buf, ConsoleNL...)

	var ActualWritten uint32
	syscall.WriteFile(StdoutHandle, buf, &ActualWritten, nil)

	if cap(buf) <= 64*1024 {
		*poolPtr = buf
		OutputBufferPool.Put(poolPtr)
	}
}

var SteamCmdDlPool = sync.Pool{
	New: func() any {
		b := make([]byte, 2*1024*1024)
		return &b
	},
}

func InstallSteamCmd(TargetDir string) uint {
	if err := os.MkdirAll(TargetDir, 0755); err != nil {
		LogOutLn(S2B("[sys] steamcmd_setup: mkdir failed: "), E2B(err))
		return 128
	}

	LogOutLn(S2B("[init] steamcmd_setup: pulling payload..."))

	Resp, err := http.Get(SteamCmdUrl)
	if err != nil {
		LogOutLn(S2B("[sys] steamcmd_setup: tcp request failed: "), E2B(err))
		return 129
	}
	defer Resp.Body.Close()

	if Resp.StatusCode != 200 {
		LogOutLn(S2B("[sys] steamcmd_setup: payload download rejected (status != 200)"))
		return 130
	}

	BufPtr := SteamCmdDlPool.Get().(*[]byte)

	defer func() {
		if cap(*BufPtr) <= 2*1024*1024 {
			SteamCmdDlPool.Put(BufPtr)
		}
	}()

	DlData := (*BufPtr)[:0]

	for {
		if len(DlData) == cap(DlData) {
			NewCap := cap(DlData) * 2
			if NewCap > 20*1024*1024 {
				NewCap = 20*1024*1024 + 1
			}
			vNewBuffer := make([]byte, len(DlData), NewCap)
			copy(vNewBuffer, DlData)
			DlData = vNewBuffer
			*BufPtr = DlData
		}

		n, err := Resp.Body.Read(DlData[len(DlData):cap(DlData)])
		DlData = DlData[:len(DlData)+n]

		if len(DlData) > 20*1024*1024 {
			LogOutLn(S2B("[fatal] steamcmd_setup: mitm defense triggered. payload >20MB. aborting tcp."))
			return 139
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			LogOutLn(S2B("[sys] steamcmd_setup: tcp stream read failed: "), E2B(err))
			return 131
		}
	}

	MemReader := bytes.NewReader(DlData)
	ZipReader, err := zip.NewReader(MemReader, int64(len(DlData)))
	if err != nil {
		LogOutLn(S2B("[sys] steamcmd_setup: zip parsing failed: "), E2B(err))
		return 132
	}

	AbsTargetDir := filepath.Clean(TargetDir) + string(os.PathSeparator)

	LogOutLn(S2B("[init] steamcmd_setup: extracting payload..."))

	var UnzippedTotal int64 = 0
	const UnzipFuseLimit int64 = 20 * 1024 * 1024

	for _, vFile := range ZipReader.File {
		FilePath := filepath.Join(TargetDir, vFile.Name)

		if !strings.HasPrefix(FilePath, AbsTargetDir) {
			LogOutLn(S2B("[fatal] steamcmd_setup: path traversal payload detected, aborted: "), S2B(FilePath))
			return 133
		}

		if vFile.FileInfo().IsDir() {
			os.MkdirAll(FilePath, os.ModePerm)
			continue
		}

		if err = os.MkdirAll(filepath.Dir(FilePath), os.ModePerm); err != nil {
			LogOutLn(S2B("[sys] steamcmd_setup: mkdir failed: "), E2B(err))
			return 134
		}

		OutFile, err := os.OpenFile(FilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, vFile.Mode())
		if err != nil {
			LogOutLn(S2B("[sys] steamcmd_setup: fd create failed: "), E2B(err))
			return 135
		}

		DeflatedData, err := vFile.Open()
		if err != nil {
			OutFile.Close()
			LogOutLn(S2B("[sys] steamcmd_setup: zip entry open failed: "), E2B(err))
			return 136
		}

		ThrottleReader := io.LimitReader(DeflatedData, UnzipFuseLimit-UnzippedTotal+1)
		WrittenBytes, err := io.Copy(OutFile, ThrottleReader)

		UnzippedTotal += WrittenBytes

		OutFile.Close()
		DeflatedData.Close()

		if UnzippedTotal > UnzipFuseLimit {
			LogOutLn(S2B("[fatal] steamcmd_setup: zip bomb triggered (>20MB). aborting & wiping disk."))
			os.RemoveAll(TargetDir)
			return 140
		}

		if err != nil {
			LogOutLn(S2B("[sys] steamcmd_setup: fd write failed: "), E2B(err))
			return 137
		}
	}

	LogOutLn(S2B("[init] steamcmd_setup: executing dry-run to bootstrap environment..."))

	BinPath := filepath.Join(TargetDir, SteamCmdName)
	vCmd := exec.Command(BinPath, "+quit")

	vCmd.Stdout = os.Stdout
	vCmd.Stderr = os.Stderr

	if err := vCmd.Run(); err != nil {
		LogOutLn(S2B("[sys] steamcmd_setup: dry-run failed: "), E2B(err))
		return 138
	}
	LogOutLn(S2B("[init] steamcmd_setup: deployment complete."))
	return 0
}

func FetchDefStorageRoot() string {
	PsScript := `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; $path = (Get-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Explorer\User Shell Folders').Personal; [Environment]::ExpandEnvironmentVariables($path)`

	vCmd := exec.Command("powershell", "-NoProfile", "-Command", PsScript)
	vStdout, err := vCmd.Output()

	RealDocPath := ""

	if err == nil {
		RealDocPath = strings.TrimSpace(string(vStdout))
	}

	if RealDocPath == "" {
		home, _ := os.UserHomeDir()
		RealDocPath = filepath.Join(home, "Documents")
	}

	return filepath.Join(RealDocPath, "Klei")
}

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetProcessTimes         = modkernel32.NewProc("GetProcessTimes")
	procGetSystemTimeAsFileTime = modkernel32.NewProc("GetSystemTimeAsFileTime")
	procOpenProcess             = modkernel32.NewProc("OpenProcess")
	procGetProcessMemoryInfo    = modkernel32.NewProc("K32GetProcessMemoryInfo")
)

type ProbeSnap struct {
	_            [64]byte
	MasterKernel uint64
	MasterUser   uint64
	CavesKernel  uint64
	CavesUser    uint64
	LastSysTime  uint64
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

var LastSysSnapshot ProbeSnap

func ClusterProbeTask() {
	PackedPID := GlobalConf.ProcState.PID.Load()
	if PackedPID == 0 {
		return
	}

	masterPID := uint32(PackedPID >> 32)
	cavesPID := uint32(PackedPID & 0xFFFFFFFF)

	var SysTime uint64
	procGetSystemTimeAsFileTime.Call(uintptr(unsafe.Pointer(&SysTime)))
	TimeDelta := SysTime - LastSysSnapshot.LastSysTime
	if TimeDelta <= 0 {
		TimeDelta = 1
	}

	if masterPID > 0 {
		cpu, mem, k, u := PollWin32Metrics(masterPID, LastSysSnapshot.MasterKernel, LastSysSnapshot.MasterUser, TimeDelta)
		GlobalConf.ClusterMonState.MasterCPU.Store(cpu)
		GlobalConf.ClusterMonState.MasterMem.Store(uint64(mem))
		LastSysSnapshot.MasterKernel = k
		LastSysSnapshot.MasterUser = u
	}

	if cavesPID > 0 {
		cpu, mem, k, u := PollWin32Metrics(cavesPID, LastSysSnapshot.CavesKernel, LastSysSnapshot.CavesUser, TimeDelta)
		GlobalConf.ClusterMonState.CavesCPU.Store(cpu)
		GlobalConf.ClusterMonState.CavesMem.Store(uint64(mem))
		LastSysSnapshot.CavesKernel = k
		LastSysSnapshot.CavesUser = u
	}

	LastSysSnapshot.LastSysTime = SysTime
}

func PollWin32Metrics(pid uint32, LastKTime, LastUTime, SysTimeDelta uint64) (CpuBps uint32, ByteMem uintptr, NewKTime, NewUTime uint64) {
	h, _, _ := procOpenProcess.Call(0x1000, 0, uintptr(pid))
	if h == 0 {
		return
	}
	defer syscall.CloseHandle(syscall.Handle(h))

	var c, e, k, u uint64
	ret, _, _ := procGetProcessTimes.Call(h, uintptr(unsafe.Pointer(&c)), uintptr(unsafe.Pointer(&e)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))

	if ret != 0 {
		NewKTime, NewUTime = k, u
		if LastKTime > 0 || LastUTime > 0 {
			ProcDelta := (k - LastKTime) + (u - LastUTime)
			CpuBps = uint32((ProcDelta * 10000) / SysTimeDelta)
		}
	}

	var MemGauge PROCESS_MEMORY_COUNTERS
	MemGauge.CB = uint32(unsafe.Sizeof(MemGauge))

	retMem, _, _ := procGetProcessMemoryInfo.Call(h, uintptr(unsafe.Pointer(&MemGauge)), uintptr(MemGauge.CB))
	if retMem != 0 {
		ByteMem = MemGauge.WorkingSetSize
	}

	return
}

func RawListen(Port string, Router http.Handler) error {
	vNetProtocol := "tcp"
	if strings.HasSuffix(Port, ".sock") {
		vNetProtocol = "unix"
		os.Remove(Port)
	}

	vListener, err := net.Listen(vNetProtocol, Port)
	if err != nil {
		return err
	}

	return http.Serve(vListener, Router)
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

func BindProcLifetime(cmd *exec.Cmd) {
}

func SetProcExitSig(cmd *exec.Cmd) {
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

func CheckLinuxEnv(_ bool, _ bool) bool {
	return true
}
