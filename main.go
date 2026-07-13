package main

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"time"
)

var ActionBus = make(chan KernelAction, 1)
var GlobalCtx context.Context
var GlobalKillSwitch context.CancelFunc

type CancelFunc context.CancelFunc

var EpochKillSwitch atomic.Pointer[CancelFunc]

type KernelAction uint8

const (
	Action_Noop KernelAction = iota
	Action_MasterCrash
	Action_CavesCrash
	Action_GameUpdate
	Action_ModHotUpdate
	Action_ApiHalt
	Action_SystemHalt
)

func main() {
	GlobalCtx, GlobalKillSwitch = context.WithCancel(context.Background())
	defer GlobalKillSwitch()

	go Watchdog()

	BootNucleus()
	InitGameState()

	vArgs := os.Args
	vLen := len(vArgs)
	for i := 1; i < vLen; i++ {
		if vArgs[i] == "-c" && i+1 < vLen {
			ConfDir = vArgs[i+1]
			break
		}
	}

	_, err := os.Stat(ConfDir)

	if os.IsNotExist(err) {
		AtomicWriteFile(ConfDir, DefConfFile)
		LogOutLn(S2B(ConfDir), S2B("[init] default config generated. manual edit required before start."))
		os.Exit(0)
	}

	if CurrDiskContent, err := os.ReadFile(ConfDir); err == nil {
		if IsFactoryDefault(CurrDiskContent, DefConfFile) {
			LogOutLn(S2B("[warn] config: unmodified default configuration detected"))
			LogOutLn(S2B("[warn] config: running with factory defaults. review settings in "), S2B(ConfDir))
		}
	}

	LoadConfig(GlobalConf, ConfDir)

	if !CheckLinuxEnv(GlobalConf.Section1.SkipLinuxCheck, GlobalConf.Section1.SkipRootCheck) {
		os.Exit(1)
	}

	InitAnnounceLang()

	if GlobalConf.Section1.GameBinDir == "" {
		GlobalConf.Section1.GameBinDir = filepath.Join(GlobalConf.Section1.SteamCmdPath, "steamapps", "common", "Don't Starve Together Dedicated Server", "bin64")
	}
	if GlobalConf.Section1.ModLuaTarget == "" {
		GlobalConf.Section1.ModLuaTarget = filepath.Join(GlobalConf.Section1.SteamCmdPath, "steamapps", "common", "Don't Starve Together Dedicated Server", "mods", "dedicated_server_mods_setup.lua")
	}
	if GlobalConf.Section1.ModLuaBackup == "" {
		GlobalConf.Section1.ModLuaBackup = filepath.Join(GlobalConf.Section1.SteamCmdPath, "steamapps", "common", "Don't Starve Together Dedicated Server", "mods", "dst-nucleus_backup.lua")
	}

	InitPaths()

	if GlobalConf.Section1.AutoInstallOnBoot {
		AutoInstall()
	}

	if GlobalConf.Section2.UpdateCheckInterval.Load() < 60 {
		LogOutLn(S2B("[warn] interval too short, auto-adjusted to 60s"))
		GlobalConf.Section2.UpdateCheckInterval.Store(60)
	}
	AutoUpdateTickerInterval = time.Duration(GlobalConf.Section2.UpdateCheckInterval.Load()) * time.Second

	GlobalConf.Section1.CommonBootArgs = []string{}
	if GlobalConf.Section1.StorageRoot == "" {
		GlobalConf.Section1.StorageRoot = FetchDefStorageRoot()
		LogOutLn(S2B("[init] storage path not set, falling back to default: "), S2B(GlobalConf.Section1.StorageRoot))
	} else {
		LogOutLn(S2B("[init] storage path explicitly set to: "), S2B(GlobalConf.Section1.StorageRoot))
		GlobalConf.Section1.CommonBootArgs = append(GlobalConf.Section1.CommonBootArgs, "-persistent_storage_root", GlobalConf.Section1.StorageRoot)
	}
	GlobalConf.Section1.CommonBootArgs = append(GlobalConf.Section1.CommonBootArgs, "-console", "-cluster", GlobalConf.Section1.ClusterName)

	if GlobalConf.Section2.WriteDefaultConf.Load() {
		AtomicWriteFile(filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Master", "server.ini"), S2B(DefMasterConf))
		AtomicWriteFile(filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Caves", "server.ini"), S2B(DefCavesConf))
		AtomicWriteFile(filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "cluster.ini"), S2B(DefCluster))
	}

	go ListenConsole()

	if GlobalConf.Section1.HttpBind != "" {
		go BootLocalApi()
	}

	if GlobalConf.Section1.AutoStartServer {
		GlobalConf.AtomicGate.ServerRunGate.Store(true)
	}

	for {
		if !GlobalConf.AtomicGate.ServerRunGate.Load() {
			LogOutLn(S2B("[idle] waiting for /api/start signal..."))
			select {
			case <-ApiWakeChan:
				LogOutLn(S2B("[init] api signal received, starting boot sequence..."))
			case <-GlobalCtx.Done():
				return
			}
		}

		for GlobalConf.AtomicGate.GameUpdatingGate.Load() || GlobalConf.AtomicGate.ModBusyGate.Load() {
			LogOutLn(S2B("[warn] race condition prevented: background update in progress, waiting for locks to release..."))
			time.Sleep(2 * time.Second)
		}

		if ProbeGameUpdate() == 0 {
			ExecGameUpdate()
		}

		CloneFile(GlobalConf.Section1.ModLuaBackup, GlobalConf.Section1.ModLuaTarget)
		CloneFile(GlobalConf.Section1.ModConfPath, filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Master", "modoverrides.lua"))
		CloneFile(GlobalConf.Section1.ModConfPath, filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Caves", "modoverrides.lua"))
		CloneFile(GlobalConf.Section1.MasterWorldConfPath, filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Master", "worldgenoverride.lua"))
		CloneFile(GlobalConf.Section1.CavesWorldConfPath, filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Caves", "worldgenoverride.lua"))
		if GlobalConf.Section2.cluster_token != "" {
			AtomicWriteFile(filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "cluster_token.txt"), S2B(GlobalConf.Section2.cluster_token))
		}

		if ProbeModUpdate() == 0 {
			ExecModUpdate()
		}

		LifeCtx, KillSwitch := context.WithCancel(GlobalCtx)

		SeverancePayload := CancelFunc(KillSwitch)
		EpochKillSwitch.Store(&SeverancePayload)

		select {
		case <-ActionBus:
		default:
		}

		if GlobalConf.Section2.EnableMaster.Load() {
			wg.Add(1)
			go BootMaster(LifeCtx, KillSwitch)
		}
		if GlobalConf.Section2.EnableCaves.Load() {
			wg.Add(1)
			go BootCaves(LifeCtx, KillSwitch)
		}
		go VersionProbe(LifeCtx)

		var FireAction KernelAction
		select {
		case FireAction = <-ActionBus:
		case <-GlobalCtx.Done():
			FireAction = Action_SystemHalt
		}

		switch FireAction {
		case Action_GameUpdate:
			GracefulReboot(LifeCtx, AnnounceGameUpdate, nil)
		case Action_ModHotUpdate:
			LogOutLn(S2B("[core] mod hot-update supported, background downloading..."), nil)
			ExecModUpdate()
			GracefulReboot(LifeCtx, AnnounceModUpdate, nil)
		default:
		}

		KillSwitch()
		EpochKillSwitch.Store(nil)
		wg.Wait()
		LogOutLn(S2B("[core] process group destroyed, fd locks released."))

		switch FireAction {
		case Action_GameUpdate:
			LogOutLn(S2B("[core] overwriting game binaries..."))
			ExecGameUpdate()
		case Action_MasterCrash, Action_CavesCrash:
			LogOutLn(S2B("[core] cleanup done, sleep 3s before dual-shard boot..."))
			if !GlobalConf.Section2.EnableCrashReboot.Load() {
				GlobalConf.AtomicGate.ServerRunGate.Store(false)
			}
			time.Sleep(3 * time.Second)
		case Action_ApiHalt:
			LogOutLn(S2B("[core] server terminated forcefully."))
			if !GlobalConf.Section2.EnableCrashReboot.Load() {
				GlobalConf.AtomicGate.ServerRunGate.Store(false)
			}
		case Action_SystemHalt:
			LogOutLn(S2B("[core] system exit(0)."))
			return
		}
	}
}

func Watchdog() {
	SignalPipe := make(chan os.Signal, 1)
	signal.Notify(SignalPipe, os.Interrupt, syscall.SIGTERM)

	<-SignalPipe

	GlobalKillSwitch()

	<-time.After(5 * time.Second)
	LogOutLn(S2B("[watchdog] graceful shutdown timeout reached, force-exit in 5s..."))

	<-time.After(5 * time.Second)
	os.Exit(1)
}

func ListenConsole() {
	var PhysBuffer [4096]byte
	var TailCursor int

	const cavesCommandPrefix = "caves:"
	const cavesCommandPrefixLen = len(cavesCommandPrefix)

	EnableMaster := GlobalConf.Section2.EnableMaster.Load()

	for {
		n, err := os.Stdin.Read(PhysBuffer[TailCursor:])
		if err != nil || n == 0 {
			break
		}
		TailCursor += n

		ScanCursor := 0
		for i := 0; i < TailCursor; i++ {
			if PhysBuffer[i] == '\n' {
				InstrStream := PhysBuffer[ScanCursor:i]

				if len(InstrStream) > 0 && InstrStream[len(InstrStream)-1] == '\r' {
					InstrStream = InstrStream[:len(InstrStream)-1]
				}

				if string(InstrStream) == `igegjfmwdhb` {
					debug.FreeOSMemory()
					ScanCursor = i + 1
					continue
				}

				if !EnableMaster {
					EmitRawCmd(B2S(InstrStream), RouteToCaves)
				} else if len(InstrStream) >= cavesCommandPrefixLen && string(InstrStream[:cavesCommandPrefixLen]) == cavesCommandPrefix {
					EmitRawCmd(B2S(InstrStream[cavesCommandPrefixLen:]), RouteToCaves)
				} else {
					EmitRawCmd(B2S(InstrStream), RouteToMaster)
				}
				ScanCursor = i + 1
			}
		}

		if ScanCursor < TailCursor {
			copy(PhysBuffer[:], PhysBuffer[ScanCursor:TailCursor])
			TailCursor -= ScanCursor
		} else {
			TailCursor = 0
		}
	}
}
