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

const DSTExecName = `dontstarve_dedicated_server_nullrenderer_x64`
const SteamCmdUrl = `https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz`
const SteamCmdName = "steamcmd.sh"

var SteamCmdDefPath = filepath.Join(os.Getenv("HOME"), "Steam")

var (
	ProbePool = sync.Pool{
		New: func() any {
			b := make([]byte, 1024)
			return &b
		},
	}
)

var PlatformAttrs *syscall.SysProcAttr

func init() {
	PlatformAttrs = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGKILL,
	}
}

var ConsoleNL = []byte{'\n'}

func LogOut(FragGroup ...[]byte) {
	var vLocalIovecs [64]syscall.Iovec

	var ActualVecs int
	for _, Frag := range FragGroup {
		vLen := len(Frag)
		if vLen > 0 && ActualVecs < 64 {
			vLocalIovecs[ActualVecs].Base = &Frag[0]
			vLocalIovecs[ActualVecs].SetLen(vLen)
			ActualVecs++
		}
	}

	if ActualVecs > 0 {
		syscall.RawSyscall(
			syscall.SYS_WRITEV,
			uintptr(1),
			uintptr(unsafe.Pointer(&vLocalIovecs[0])),
			uintptr(ActualVecs),
		)
	}
}

func LogOutLn(FragGroup ...[]byte) {
	var vLocalIovecs [64]syscall.Iovec

	var ActualVecs int
	for _, Frag := range FragGroup {
		vLen := len(Frag)
		if vLen > 0 && ActualVecs < 63 {
			vLocalIovecs[ActualVecs].Base = &Frag[0]
			vLocalIovecs[ActualVecs].SetLen(vLen)
			ActualVecs++
		}
	}

	vLocalIovecs[ActualVecs].Base = &ConsoleNL[0]
	vLocalIovecs[ActualVecs].SetLen(len(ConsoleNL))
	ActualVecs++

	syscall.RawSyscall(
		syscall.SYS_WRITEV,
		uintptr(1),
		uintptr(unsafe.Pointer(&vLocalIovecs[0])),
		uintptr(ActualVecs),
	)
}

func CheckLinuxEnv(SkipCheck bool, SkipRoot bool) bool {
	if os.Getuid() == 0 {
		if !SkipRoot {
			LogOutLn(S2B("[fatal] root privilege detected. lua sandbox breakout risk."))
			LogOutLn(S2B("[info] set 'permit_root_usage: true' in config to bypass this security lock."))
			return true
		}
	}
	if SkipCheck {
		return true
	}

	const LoaderPath = `/lib/ld-linux.so.2`

	_, err := os.Stat(LoaderPath)
	HasLoader := !os.IsNotExist(err)

	if !HasLoader {
		LogOutLn(S2B("[fatal] missing 32-bit ld-linux.so.2 loader. steamcmd requires lib32gcc-s1."))
		LogOutLn(S2B("[info] ubuntu: sudo dpkg --add-architecture i386 && sudo apt update && sudo apt install lib32gcc-s1"))
		LogOutLn(S2B("[info] debian: sudo dpkg --add-architecture i386 && sudo apt update && sudo apt install lib32gcc-s1"))
		LogOutLn(S2B("[info] set 'skip_linux_lib32_check: true' in config if you know what you are doing."))
		return false
	}
	return true
}

var SteamCmdUnzipPool_Linux = sync.Pool{
	New: func() any {
		b := make([]byte, 64*1024)
		return &b
	},
}

func InstallSteamCmd(TargetDir string) uint8 {
	if err := os.MkdirAll(TargetDir, 0755); err != nil {
		LogOutLn(S2B("[sys] steamcmd_setup: mkdir failed: "), E2B(err))
		return 128
	}

	LogOutLn(S2B("[init] steamcmd_setup: pulling payload..."))

	vClient := &http.Client{
		Timeout: 5 * time.Minute,
	}

	Resp, err := vClient.Get(SteamCmdUrl)
	if err != nil {
		LogOutLn(S2B("[sys] steamcmd_setup: tcp request failed: "), E2B(err))
		return 129
	}
	defer Resp.Body.Close()

	if Resp.StatusCode != 200 {
		LogOutLn(S2B("[sys] steamcmd_setup: payload download rejected (status != 200)"))
		return 130
	}

	UnzipStream, err := gzip.NewReader(Resp.Body)
	if err != nil {
		LogOutLn(S2B("[sys] steamcmd_setup: gzip reader init failed: "), E2B(err))
		return 131
	}
	defer UnzipStream.Close()

	BufPtr := SteamCmdUnzipPool_Linux.Get().(*[]byte)
	defer SteamCmdUnzipPool_Linux.Put(BufPtr)

	ArchiveReader := tar.NewReader(UnzipStream)
	AbsTargetDir := filepath.Clean(TargetDir) + string(os.PathSeparator)

	var UnzippedTotal int64 = 0
	const UnzipFuseLimit int64 = 20 * 1024 * 1024

	for {
		vHeader, err := ArchiveReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			LogOutLn(S2B("[sys] steamcmd_setup: tar stream corrupted: "), E2B(err))
			return 132
		}

		UnzipTarget := filepath.Join(TargetDir, vHeader.Name)

		if !strings.HasPrefix(UnzipTarget, AbsTargetDir) {
			LogOutLn(S2B("[fatal] steamcmd_setup: path traversal payload detected, aborted: "), E2B(err))
			return 133
		}

		switch vHeader.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(UnzipTarget, 0755)
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(UnzipTarget), 0755); err != nil {
				LogOutLn(S2B("[sys] steamcmd_setup: mkdir failed: "), E2B(err))
				return 134
			}

			OutFile, err := os.OpenFile(UnzipTarget, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(vHeader.Mode))
			if err != nil {
				LogOutLn(S2B("[sys] steamcmd_setup: fd create failed: "), E2B(err))
				return 135
			}

			ThrottleReader := io.LimitReader(ArchiveReader, UnzipFuseLimit-UnzippedTotal+1)

			WrittenBytes, copyErr := io.CopyBuffer(OutFile, ThrottleReader, *BufPtr)

			UnzippedTotal += WrittenBytes

			closeErr := OutFile.Close()

			if UnzippedTotal > UnzipFuseLimit {
				LogOutLn(S2B("[fatal] steamcmd_setup: tar bomb triggered (>20MB). aborting tcp & wiping disk."))
				os.RemoveAll(TargetDir)
				return 140
			}

			if copyErr != nil {
				LogOutLn(S2B("[sys] steamcmd_setup: buffer flush failed: "), E2B(copyErr))
				return 136
			}
			if closeErr != nil {
				LogOutLn(S2B("[sys] steamcmd_setup: fd close failed: "), E2B(closeErr))
				return 137
			}
		}
	}

	LogOutLn(S2B("[init] steamcmd_setup: tarball extracted."))
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
	HomeDir, err := os.UserHomeDir()
	if err != nil {
		return "/root/.klei"
	}

	return filepath.Join(HomeDir, ".klei")
}

type ProbeSnap struct {
	_             [64]byte
	MasterTicks   uint64
	CavesTicks    uint64
	LastTimestamp int64
	_             [64]byte
}

var LastSysSnapshot ProbeSnap

func ClusterProbeTask() {
	PackedPID := GlobalConf.ProcState.PID.Load()
	if PackedPID == 0 {
		return
	}

	masterPID := int32(PackedPID >> 32)
	cavesPID := int32(PackedPID & 0xFFFFFFFF)

	BufPtr := ProbePool.Get().(*[]byte)
	defer ProbePool.Put(BufPtr)
	Buffer := *BufPtr

	vNow := time.Now().UnixNano()
	DeltaNanos := vNow - LastSysSnapshot.LastTimestamp
	if DeltaNanos <= 0 {
		DeltaNanos = 1
	}
	var NewMasterTicks, NewCavesTicks uint64

	if masterPID > 0 {
		cpu, mem, CurrTicks := PollProcMetrics(masterPID, Buffer, LastSysSnapshot.MasterTicks, DeltaNanos)
		GlobalConf.ClusterMonState.MasterCPU.Store(cpu)
		GlobalConf.ClusterMonState.MasterMem.Store(mem)
		NewMasterTicks = CurrTicks
	}

	if cavesPID > 0 {
		cpu, mem, CurrTicks := PollProcMetrics(cavesPID, Buffer, LastSysSnapshot.CavesTicks, DeltaNanos)
		GlobalConf.ClusterMonState.CavesCPU.Store(cpu)
		GlobalConf.ClusterMonState.CavesMem.Store(mem)
		NewCavesTicks = CurrTicks
	}
	LastSysSnapshot.MasterTicks = NewMasterTicks
	LastSysSnapshot.CavesTicks = NewCavesTicks
	LastSysSnapshot.LastTimestamp = vNow

}

func PollProcMetrics(pid int32, Buffer []byte, LastTicks uint64, NanoDelta int64) (CpuBps uint32, ByteMem uint64, CurrTick uint64) {
	var (
		fd   uintptr
		n    uintptr
		err1 syscall.Errno
	)

	JoinProcPath(Buffer, pid, "/statm")
	fd, _, err1 = syscall.RawSyscall6(syscall.SYS_OPENAT, uintptr(0xffffff9c), uintptr(unsafe.Pointer(&Buffer[0])), uintptr(syscall.O_RDONLY), 0, 0, 0)

	if err1 == 0 {
		n, _, err1 = syscall.RawSyscall(syscall.SYS_READ, fd, uintptr(unsafe.Pointer(&Buffer[0])), uintptr(len(Buffer)))
		syscall.RawSyscall(syscall.SYS_CLOSE, fd, 0, 0)

		if n > 0 {
			vCursor := 0
			ReadLen := int(n)
			for vCursor < ReadLen && Buffer[vCursor] != ' ' {
				vCursor++
			}
			for vCursor < ReadLen && Buffer[vCursor] == ' ' {
				vCursor++
			}

			var rss uint64
			for vCursor < ReadLen && Buffer[vCursor] >= '0' && Buffer[vCursor] <= '9' {
				rss = rss*10 + uint64(Buffer[vCursor]-'0')
				vCursor++
			}
			ByteMem = rss * 4096
		}
	}

	JoinProcPath(Buffer, pid, "/stat")
	fd, _, err1 = syscall.RawSyscall6(syscall.SYS_OPENAT, uintptr(0xffffff9c), uintptr(unsafe.Pointer(&Buffer[0])), uintptr(syscall.O_RDONLY), 0, 0, 0)

	if err1 == 0 {
		n, _, err1 = syscall.RawSyscall(syscall.SYS_READ, fd, uintptr(unsafe.Pointer(&Buffer[0])), uintptr(len(Buffer)))
		syscall.RawSyscall(syscall.SYS_CLOSE, fd, 0, 0)

		if err1 == 0 && n > 0 {
			ValidData := Buffer[:n]
			utime := ExtractStat(ValidData, 12)
			stime := ExtractStat(ValidData, 13)
			CurrTotalTicks := utime + stime
			CurrTick = CurrTotalTicks

			if LastTicks > 0 && CurrTotalTicks >= LastTicks {
				TickDelta := CurrTotalTicks - LastTicks
				CpuBps = uint32((TickDelta * 100000000000) / uint64(NanoDelta))
			}
		}
	}

	return CpuBps, ByteMem, CurrTick
}

func JoinProcPath(Buffer []byte, pid int32, Suffix string) {
	copy(Buffer[0:6], "/proc/")
	vCursor := 6

	var Temp [16]byte
	i := 15
	for n := uint32(pid); n > 0; n /= 10 {
		Temp[i] = byte('0' + (n % 10))
		i--
	}
	vLen := 15 - i
	copy(Buffer[vCursor:], Temp[i+1:])
	vCursor += vLen

	copy(Buffer[vCursor:], Suffix)
	vCursor += len(Suffix)

	Buffer[vCursor] = 0
}

func ExtractStat(vData []byte, TargetIdx int) uint64 {
	BracketEnd := bytes.LastIndexByte(vData, ')')
	if BracketEnd == -1 {
		return 0
	}

	CurrIdx := 0
	vCursor := BracketEnd + 2
	var Result uint64

	for vCursor < len(vData) {
		if vData[vCursor] == ' ' {
			vCursor++
			continue
		}

		CurrIdx++
		if CurrIdx == TargetIdx {
			for vCursor < len(vData) && vData[vCursor] >= '0' && vData[vCursor] <= '9' {
				Result = Result*10 + uint64(vData[vCursor]-'0')
				vCursor++
			}
			return Result
		}

		for vCursor < len(vData) && vData[vCursor] != ' ' {
			vCursor++
		}
	}
	return 0
}

func RawListen(BindAddr string, Router http.Handler) error {
	var Protocol, RealPath string
	var EnableLoadBalance bool

	if strings.HasPrefix(BindAddr, "/") || strings.HasPrefix(BindAddr, "./") || strings.HasSuffix(BindAddr, ".sock") {
		Protocol = "unix"
		RealPath = BindAddr
		EnableLoadBalance = false
		syscall.Unlink(RealPath)
	} else {
		Protocol = "tcp"
		RealPath = BindAddr
		EnableLoadBalance = true
	}

	NumCores := runtime.NumCPU()
	if !EnableLoadBalance {
		NumCores = 1
	}

	ErrPipe := make(chan error, NumCores)

	for i := 0; i < NumCores; i++ {
		go func() {
			Config := net.ListenConfig{
				Control: func(network, address string, c syscall.RawConn) error {
					var err error
					c.Control(func(fd uintptr) {
						if EnableLoadBalance {
							err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 15, 1)
						}
					})
					return err
				},
			}

			RawListener, err := Config.Listen(context.Background(), Protocol, RealPath)
			if err != nil {
				ErrPipe <- err
				return
			}

			if Protocol == "unix" {
				os.Chmod(RealPath, 0666)
			}

			ErrPipe <- http.Serve(RawListener, Router)
		}()
	}

	return <-ErrPipe
}

func BindProcLifetime(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

func SetProcExitSig(cmd *exec.Cmd) {
}
