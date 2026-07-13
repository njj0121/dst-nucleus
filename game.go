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
var ParasiteCmd []byte

type TargetShard uint8

const (
	RouteToMaster TargetShard = 1 << 0
	RouteToCaves  TargetShard = 1 << 1
	RouteToAll    TargetShard = 3
)

var MasterCmdPipe = make(chan []byte, 100)
var CavesCmdPipe = make(chan []byte, 100)

var MasterParasitePulse = make(chan struct{}, 1)
var CavesParasitePulse = make(chan struct{}, 1)

func EmitRawCmd(CmdStr string, CmdTarget TargetShard) {
	var FinalCmd []byte

	if len(CmdStr) > 0 && CmdStr[len(CmdStr)-1] != '\n' {
		FinalCmd = append(S2B(CmdStr), '\n')
	} else {
		FinalCmd = S2B(CmdStr)
	}

	ForcePushCmd := func(Chan chan []byte, Instr []byte) {
		select {
		case Chan <- Instr:
		default:
		}
	}

	switch CmdTarget {
	case RouteToMaster:
		ForcePushCmd(MasterCmdPipe, FinalCmd)
	case RouteToCaves:
		ForcePushCmd(CavesCmdPipe, FinalCmd)
	case RouteToAll:
		ForcePushCmd(MasterCmdPipe, FinalCmd)
		ForcePushCmd(CavesCmdPipe, FinalCmd)
	}
}

var wg sync.WaitGroup

func BootMaster(LifeCtx context.Context, KillSwitch context.CancelFunc) {
	defer wg.Done()
	defer KillSwitch()
	defer GlobalConf.ProcState.MasterEpoch.Store(0)
	defer InitGameState()
	defer GlobalConf.AtomicGate.MasterReadyGate.Store(false)

	if GlobalConf.Section2.CavesEpochLink != "" {
		go RemoteEpochSensor(LifeCtx, GlobalConf.Section2.CavesEpochLink, Action_CavesCrash, "master", &GlobalConf.AtomicGate.MasterReadyGate)
	}

	shardCtx, cancelShard := context.WithCancel(context.Background())
	defer cancelShard()
	MasterProc := exec.CommandContext(shardCtx, GameBinPath, append(GlobalConf.Section1.CommonBootArgs, "-shard", "Master")...)
	MasterProc.Dir = GlobalConf.Section1.GameBinDir
	BindProcLifetime(MasterProc)

	MasterStdin, _ := MasterProc.StdinPipe()

	MasterStdout, _ := MasterProc.StdoutPipe()
	go LiveStream(Node_Master, "[Master] ", MasterStdout, &GlobalConf.AtomicGate.MasterReadyGate)

	MasterStderr, _ := MasterProc.StderrPipe()
	go LiveStream(Node_MasterErr, "[Master_ERR] ", MasterStderr, nil)

	if err := MasterProc.Start(); err != nil {
		LogOutLn(S2B("[fatal] master boot failed: "), E2B(err))
		select {
		case ActionBus <- Action_MasterCrash:
		default:
		}
		return
	} else {
		GlobalConf.ProcState.MasterEpoch.Store(time.Now().UnixNano())
	}
	SetProcExitSig(MasterProc)
	ProcExitSig := make(chan struct{})
	defer vKillProcess(MasterProc, "Master", &MasterCmdPipe, ProcExitSig, cancelShard, &GlobalConf.AtomicGate.MasterReadyGate)
	for {
		old := GlobalConf.ProcState.PID.Load()
		newPID := (old & 0x00000000FFFFFFFF) | (uint64(uint32(MasterProc.Process.Pid)) << 32)
		if GlobalConf.ProcState.PID.CompareAndSwap(old, newPID) {
			break
		}
	}

	defer func() {
		for {
			old := GlobalConf.ProcState.PID.Load()
			newPID := old & 0x00000000FFFFFFFF
			if GlobalConf.ProcState.PID.CompareAndSwap(old, newPID) {
				break
			}
		}
	}()

	go func() {
		FlushCmdPipe(MasterCmdPipe)
		GlobalConf.AtomicGate.MasterRxAlive.Store(true)
		defer FlushCmdPipe(MasterCmdPipe)
		defer GlobalConf.AtomicGate.MasterRxAlive.Store(false)
		go MasterRadar(LifeCtx)
		for {
			select {
			case <-ProcExitSig:
				return
			case vCmd := <-MasterCmdPipe:
				if RawPipe, CastOk := MasterStdin.(interface{ SetWriteDeadline(time.Time) error }); CastOk {
					RawPipe.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
				}
				MasterStdin.Write(vCmd)
			}
		}
	}()

	QuitChan := make(chan error, 1)
	go func() {
		QuitChan <- MasterProc.Wait()
		close(ProcExitSig)
	}()
	debug.FreeOSMemory()
	select {
	case <-LifeCtx.Done():
		LogOutLn(S2B("[sys] master execution aborted by context cancel"))
		return
	case err := <-QuitChan:
		LogOutLn(S2B("[sys] master crashed or terminated: "), E2B(err))
		select {
		case ActionBus <- Action_MasterCrash:
		default:
		}
		return
	}
}

func BootCaves(LifeCtx context.Context, KillSwitch context.CancelFunc) {
	defer wg.Done()
	defer KillSwitch()
	defer GlobalConf.ProcState.CurrCavesEpoch.Store(0)
	defer GlobalConf.AtomicGate.CavesReadyGate.Store(false)
	if !GlobalConf.Section2.EnableMaster.Load() {
		defer InitGameState()
	}

	if GlobalConf.Section2.MasterEpochLink != "" {
		go RemoteEpochSensor(LifeCtx, GlobalConf.Section2.MasterEpochLink, Action_MasterCrash, "caves", &GlobalConf.AtomicGate.CavesReadyGate)
	}

	shardCtx, cancelShard := context.WithCancel(context.Background())
	defer cancelShard()
	CavesProc := exec.CommandContext(shardCtx, GameBinPath, append(GlobalConf.Section1.CommonBootArgs, "-shard", "Caves")...)
	CavesProc.Dir = GlobalConf.Section1.GameBinDir
	BindProcLifetime(CavesProc)

	CavesStdin, _ := CavesProc.StdinPipe()

	CavesStdout, _ := CavesProc.StdoutPipe()
	go LiveStream(Node_Caves, "[Caves] ", CavesStdout, &GlobalConf.AtomicGate.CavesReadyGate)

	CavesStderr, _ := CavesProc.StderrPipe()
	go LiveStream(Node_CavesErr, "[Caves_ERR] ", CavesStderr, nil)

	if err := CavesProc.Start(); err != nil {
		LogOutLn(S2B("[fatal] caves boot failed: "), E2B(err))
		select {
		case ActionBus <- Action_CavesCrash:
		default:
		}
		return
	} else {
		GlobalConf.ProcState.CurrCavesEpoch.Store(time.Now().UnixNano())
	}
	SetProcExitSig(CavesProc)
	ProcExitSig := make(chan struct{})
	defer vKillProcess(CavesProc, "Caves", &CavesCmdPipe, ProcExitSig, cancelShard, &GlobalConf.AtomicGate.CavesReadyGate)
	for {
		old := GlobalConf.ProcState.PID.Load()
		newPID := (old & 0xFFFFFFFF00000000) | uint64(uint32(CavesProc.Process.Pid))
		if GlobalConf.ProcState.PID.CompareAndSwap(old, newPID) {
			break
		}
	}
	defer func() {
		for {
			old := GlobalConf.ProcState.PID.Load()
			newPID := old & 0xFFFFFFFF00000000
			if GlobalConf.ProcState.PID.CompareAndSwap(old, newPID) {
				break
			}
		}
	}()

	go func() {
		FlushCmdPipe(CavesCmdPipe)
		GlobalConf.AtomicGate.CavesRxAlive.Store(true)
		defer FlushCmdPipe(CavesCmdPipe)
		defer GlobalConf.AtomicGate.CavesRxAlive.Store(false)
		go CavesRadar(LifeCtx)
		for {
			select {
			case <-ProcExitSig:
				return
			case vCmd := <-CavesCmdPipe:
				if RawPipe, CastOk := CavesStdin.(interface{ SetWriteDeadline(time.Time) error }); CastOk {
					RawPipe.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
				}
				CavesStdin.Write(vCmd)
			}
		}
	}()

	QuitChan := make(chan error, 1)
	go func() {
		QuitChan <- CavesProc.Wait()
		close(ProcExitSig)
	}()
	debug.FreeOSMemory()
	select {
	case <-LifeCtx.Done():
		LogOutLn(S2B("[sys] caves execution aborted by context cancel"))
		return
	case err := <-QuitChan:
		LogOutLn(S2B("[sys] caves crashed or terminated: "), E2B(err))
		select {
		case ActionBus <- Action_CavesCrash:
		default:
		}
		return
	}
}

func RemoteEpochSensor(LifeCtx context.Context, RemoteApi string, CrashAction KernelAction, SelfName string, ReadyFlag *atomic.Bool) {
	var LastValidEpoch int64 = 0

	// 为了防止一端重启，另一端因为网络刚好断了一下不知道，等知道了再重启，这边又因为网络刚好断了一下，对方不知道，等知道了再重启的无限重启，延迟只能缓解问题，不能保证极端情况一定不会出现
	Stopwatch := time.NewTicker(1 * time.Second)
	for range 15 {
		if ReadyFlag.Load() {
			break
		}
		select {
		case <-LifeCtx.Done():
			Stopwatch.Stop()
			return
		case <-Stopwatch.C:
		}
	}
	Stopwatch.Stop()

	LogOutLn(S2B("[sys] "), S2B(SelfName), S2B(" causal link arming: "), S2B(RemoteApi))

	for {
		select {
		case <-LifeCtx.Done():
			return
		default:
		}

		ReqCtx, AbortReq := context.WithCancel(LifeCtx)

		req, err := http.NewRequestWithContext(ReqCtx, "GET", RemoteApi, nil)
		if err != nil {
			AbortReq()
			time.Sleep(2 * time.Second)
			continue
		}

		req.Header.Set("Accept", "text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			AbortReq()
			time.Sleep(2 * time.Second)
			continue
		}

		HeartbeatSignal := make(chan struct{}, 1)
		go func() {
			Watchdog := time.NewTimer(5 * time.Second)
			defer Watchdog.Stop()
			for {
				select {
				case <-ReqCtx.Done():
					return
				case <-HeartbeatSignal:
					if !Watchdog.Stop() {
						select {
						case <-Watchdog.C:
						default:
						}
					}
					Watchdog.Reset(5 * time.Second)
				case <-Watchdog.C:
					LogOutLn(S2B("[sys] "), S2B(SelfName), S2B(" sse heartbeat timeout (5s), dropping connection..."))
					AbortReq()
					return
				}
			}
		}()

		scanner := bufio.NewScanner(resp.Body)
		for {
			line := scanner.Bytes()

			select {
			case HeartbeatSignal <- struct{}{}:
			default:
			}

			if bytes.HasPrefix(line, []byte("data: ")) {
				vNumeric := bytes.TrimSpace(line[6:])
				CurrPeerEpoch, err := strconv.ParseInt(B2S(vNumeric), 10, 64)
				if err != nil {
					continue
				}

				if CurrPeerEpoch == 0 {
					if LastValidEpoch != 0 {
						LogOutLn(S2B("[sys] "), S2B(SelfName), S2B(" causal violation: remote epoch dropped to 0. trigger suicide."))
						select {
						case ActionBus <- CrashAction:
						default:
						}
						resp.Body.Close()
						AbortReq()
						return
					}
				} else {
					if LastValidEpoch == 0 {
						LastValidEpoch = CurrPeerEpoch
						LogOutLn(S2B("[sys] "), S2B(SelfName), S2B(" causal link locked to remote epoch: "), []byte(strconv.FormatInt(CurrPeerEpoch, 10)))
					} else if LastValidEpoch != CurrPeerEpoch {
						LogOutLn(S2B("[sys] "), S2B(SelfName), S2B(" causal violation: remote epoch mutated. trigger suicide."))
						select {
						case ActionBus <- CrashAction:
						default:
						}
						resp.Body.Close()
						AbortReq()
						return
					}
				}
			}
		}
	}
}

var SonarPingCmd = []byte(`local w=rawget(_G,"TheWorld"); if type(w)=="table" and w.state then print("DST_NUCLEUS_PING") end` + "\n")

func MasterRadar(LifeCtx context.Context) {
	IdleStopwatch := time.NewTicker(15 * time.Second)

	for !GlobalConf.AtomicGate.MasterReadyGate.Load() {
		select {
		case <-LifeCtx.Done():

			return

		case <-IdleStopwatch.C:
			select {
			case MasterCmdPipe <- SonarPingCmd:
			default:
			}
		}
	}
	IdleStopwatch.Stop()

	if !GlobalConf.Section2.EnableParasite.Load() {
		return
	}

	Watchdog := time.NewTimer(10 * time.Second)
	defer Watchdog.Stop()

	for {
		select {
		case <-LifeCtx.Done():
			return

		case <-MasterParasitePulse:
			if !Watchdog.Stop() {
				select {
				case <-Watchdog.C:
				default:
				}
			}
			Watchdog.Reset(10 * time.Second)

		case <-Watchdog.C:
			select {
			case MasterCmdPipe <- ParasiteCmd:
			default:
			}

			Watchdog.Reset(10 * time.Second)
		}
	}
}

func CavesRadar(LifeCtx context.Context) {
	IdleStopwatch := time.NewTicker(15 * time.Second)

	for !GlobalConf.AtomicGate.CavesReadyGate.Load() {
		select {
		case <-LifeCtx.Done():

			return

		case <-IdleStopwatch.C:
			select {
			case CavesCmdPipe <- SonarPingCmd:
			default:
			}
		}
	}
	IdleStopwatch.Stop()

	Watchdog := time.NewTimer(10 * time.Second)
	defer Watchdog.Stop()

	for {
		select {
		case <-LifeCtx.Done():
			return

		case <-CavesParasitePulse:
			if !Watchdog.Stop() {
				select {
				case <-Watchdog.C:
				default:
				}
			}
			Watchdog.Reset(10 * time.Second)

		case <-Watchdog.C:
			select {
			case CavesCmdPipe <- ParasiteCmd:
			default:
			}

			Watchdog.Reset(10 * time.Second)
		}
	}
}

var RebootingGate atomic.Uint32
var SaveAckSig = make(chan struct{}, 1)

func GracefulReboot(LifeCtx context.Context, PromptMsg string, CustomWaitTime *uint32) {
	if !GlobalConf.AtomicGate.RebootingGate.CompareAndSwap(false, true) {
		return
	}
	defer GlobalConf.AtomicGate.RebootingGate.Store(false)

	select {
	case <-SaveAckSig:
	default:
	}

	IsMasterEnabled := GlobalConf.Section2.EnableMaster.Load()
	洞穴是否开启 := GlobalConf.Section2.EnableCaves.Load()

	var i uint32
	if CustomWaitTime != nil {
		if *CustomWaitTime == 0 {
			i = 0
		} else {
			i = *CustomWaitTime
		}
	} else {
		i = uint32(GlobalConf.Section2.GraceWaitTime.Load())
	}
	if IsMasterEnabled {
		EmitRawCmd(`if #GetPlayerClientTable() == 0 then c_shutdown(true) end`, RouteToMaster)
	}
	if 洞穴是否开启 {
		EmitRawCmd(`if #GetPlayerClientTable() == 0 then c_shutdown(true) end`, RouteToCaves)
	}

	if i > 0 {
		EmitRawCmd(`c_save()`, RouteToMaster)

		Stopwatch := time.NewTicker(1 * time.Second)
		defer Stopwatch.Stop()

		NextAnnounce := i - (i % 5)

		for ; i > 0; i-- {
			if i == NextAnnounce {
				vCmd := make([]byte, 0, 128)

				vCmd = append(vCmd, "if #GetPlayerClientTable() == 0 then c_shutdown(true) else c_announce(\""...)
				vCmd = append(vCmd, PromptMsg...)
				vCmd = append(vCmd, AnnounceGraceRebootPrefix...)
				vCmd = strconv.AppendInt(vCmd, int64(i), 10)
				vCmd = append(vCmd, AnnounceGraceRebootSuffix...)
				vCmd = append(vCmd, "\") end\n"...)
				if IsMasterEnabled {
					select {
					case MasterCmdPipe <- vCmd:
					default:
					}
				} else {
					select {
					case CavesCmdPipe <- vCmd:
					default:
					}
				}

				NextAnnounce = NextAnnounce - 5
			}

			select {
			case <-LifeCtx.Done():
				return
			case <-Stopwatch.C:
			}
		}
	}
	LogOutLn(S2B("[core] dispatching final save"))
	if IsMasterEnabled {
		EmitRawCmd(`c_save(); print("K_SAVED")`, RouteToMaster)
	} else {
		EmitRawCmd(`c_save(); print("K_SAVED")`, RouteToCaves)
	}
	LogOutLn(S2B("[core] waiting for save ack"))

	TimeoutTicker := time.NewTimer(10 * time.Second)
	defer TimeoutTicker.Stop()

	select {
	case <-LifeCtx.Done():
		return
	case <-SaveAckSig:
		LogOutLn(S2B("[core] save ack received, shutting down"))
	case <-TimeoutTicker.C:
		LogOutLn(S2B("[warn] save ack timeout (10s), forcing shutdown..."))
	}

	const DiskFlushDelayMs = 500
	select {
	case <-LifeCtx.Done():
	case <-time.After(DiskFlushDelayMs * time.Millisecond):
	}
}

const (
	Node_Master uint8 = iota
	Node_Caves
	Node_MasterErr
	Node_CavesErr
	Node_ExtTool
)

var (
	Sig_ModOutdated = []byte(`is out of date and needs to be updated for new users to be able to join the server`)
	Sig_SimPaused   = []byte("Sim paused")
	Sig_Saved       = []byte("K_SAVED")
	Sig_PlayerChat  = []byte("KU_")
	EchoTaintHead   = []byte("RemoteCommandInput")
	RawProtoMagic   = []byte{0xCE, 0xDF}
)

var LogReaderPool = sync.Pool{
	New: func() any {
		return bufio.NewReaderSize(nil, 256*1024)
	},
}

func LiveStream(NodeType uint8, Prefix string, vReader io.Reader, ReadyFlag *atomic.Bool) {
	var TargetLogFile string
	var AllowConsoleOut bool
	var vCentralChan chan *BroadcastLogChunk
	var vConnCount *atomic.Int32

	switch NodeType {
	case Node_Master, Node_MasterErr:
		TargetLogFile = GlobalConf.LogState.MasterLogPath
		AllowConsoleOut = GlobalConf.LogState.MasterStdout.Load()
		vCentralChan = MasterCentralLogChan
		vConnCount = &MasterLogConnCount
	case Node_Caves, Node_CavesErr:
		TargetLogFile = GlobalConf.LogState.CavesLogPath
		AllowConsoleOut = GlobalConf.LogState.CavesStdout.Load()
		vCentralChan = CavesCentralLogChan
		vConnCount = &CavesLogConnCount
	case Node_ExtTool:
		TargetLogFile = ""
		AllowConsoleOut = true
	}
	var vWriteFile *os.File
	if TargetLogFile != "" {
		vWriteFile, _ = os.OpenFile(TargetLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if vWriteFile != nil {
			defer vWriteFile.Close()
		}
	}

	GuardedReader := LogReaderPool.Get().(*bufio.Reader)

	GuardedReader.Reset(vReader)

	defer func() {
		GuardedReader.Reset(nil)
		LogReaderPool.Put(GuardedReader)
	}()

	for {
		LineBytes, TruncateFlag, err := GuardedReader.ReadLine()
		if err != nil {
			break
		}
		if TruncateFlag {
			LogOutLn(S2B(Prefix), S2B("[pipe] toxic log >256KB truncated"))

			for TruncateFlag {
				_, TruncateFlag, err = GuardedReader.ReadLine()
				if err != nil {
					return
				}
			}

			continue
		}

		if bytes.Contains(LineBytes, EchoTaintHead) && bytes.Contains(LineBytes, S2B("NUCLEUS_ACTIVATE")) {
			continue
		}

		if Pos := bytes.Index(LineBytes, RawProtoMagic); Pos != -1 {
			ParseRawPacket(LineBytes[Pos+2:])

			switch NodeType {
			case Node_Master:
				select {
				case MasterParasitePulse <- struct{}{}:
				default:
				}
			case Node_Caves:
				select {
				case CavesParasitePulse <- struct{}{}:
				default:
				}
			}

			continue
		}

		if AllowConsoleOut {
			LogOutLn(S2B(Prefix), LineBytes)
		}
		if vWriteFile != nil {
			vWriteFile.Write(LineBytes)
			vWriteFile.Write(ConsoleNL)
		}

		if vConnCount != nil && vConnCount.Load() > 0 {
			vChunk := LogBroadcastPool.Get().(*BroadcastLogChunk)
			vChunk.vData = append(vChunk.vData[:0], "data: "...)
			vChunk.vData = append(vChunk.vData, LineBytes...)
			vChunk.vData = append(vChunk.vData, "\n\n"...)
			vChunk.RefCount.Store(1)

			select {
			case vCentralChan <- vChunk:
			default:
				LogBroadcastPool.Put(vChunk)
			}
		}

		if bytes.Contains(LineBytes, EchoTaintHead) {
			continue
		}

		if ReadyFlag != nil && !ReadyFlag.Load() {
			if bytes.Contains(LineBytes, Sig_SimPaused) {
				ReadyFlag.Store(true)
				LogOutLn(S2B(Prefix), S2B("sim paused, server ready"))
			}
			if bytes.Contains(LineBytes, S2B("DST_NUCLEUS_PING")) {
				ReadyFlag.Store(true)
				LogOutLn(S2B(Prefix), S2B("sonar ack received, server ready"))
			}
		}

		if NodeType == Node_Master {
			if bytes.Contains(LineBytes, Sig_Saved) {
				select {
				case SaveAckSig <- struct{}{}:
				default:
				}
			}

			if RebootingGate.Load() == 1 {
				continue
			}

			Pos := bytes.Index(LineBytes, Sig_ModOutdated)
			if Pos != -1 {
				PrefixChunk := LineBytes[:Pos]
				if bytes.Contains(PrefixChunk, Sig_PlayerChat) {
					LogOutLn(S2B("[sys] ignoring chat message"))
					continue
				}
				select {
				case ActionBus <- Action_ModHotUpdate:
				default:
				}
			}
		}
	}
}

func vKillProcess(cmd *exec.Cmd, ProcName string, CmdPipePtr *chan []byte, DoomSensor <-chan struct{}, KillSwitch context.CancelFunc, ReadyFlag *atomic.Bool) {
	if cmd == nil || cmd.Process == nil {
		return
	}

	if !ReadyFlag.Load() {
		KillSwitch()
		<-DoomSensor
		LogOutLn(S2B("[core] "), S2B(ProcName), S2B(" has been purged from the OS."))
		return
	}

	select {
	case <-DoomSensor:
		return
	default:
	}

	if CmdPipePtr != nil {
		select {
		case *CmdPipePtr <- S2B("c_shutdown(true)\n"):
			LogOutLn(S2B("[core] "), S2B(ProcName), S2B(" c_shutdown dispatched."))
		default:
			LogOutLn(S2B("[warn] "), S2B(ProcName), S2B(" command channel is full, deploying SIGKILL immediately..."))
			KillSwitch()
			<-DoomSensor
			return
		}
	} else {
		LogOutLn(S2B("[warn] "), S2B(ProcName), S2B(" command channel missing, deploying SIGKILL immediately..."))
		KillSwitch()
		<-DoomSensor
		return
	}

	TimeoutStopwatch := time.NewTimer(10 * time.Second)
	defer TimeoutStopwatch.Stop()

	select {
	case <-DoomSensor:
		LogOutLn(S2B("[core] "), S2B(ProcName), S2B(" gracefully saved and terminated."))
	case <-TimeoutStopwatch.C:
		LogOutLn(S2B("[warn] "), S2B(ProcName), S2B(" shutdown timeout (10s)! deploying SIGKILL..."))
		KillSwitch()
		<-DoomSensor
		LogOutLn(S2B("[core] "), S2B(ProcName), S2B(" has been purged from the OS."))
	}
}

func FlushCmdPipe(Chan chan []byte) {
	for {
		select {
		case <-Chan:
		default:
			return
		}
	}
}
