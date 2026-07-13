package main

import (
	"path/filepath"
	"sync/atomic"
)

var GlobalConf = &SysConf{}

func init() {
	GlobalConf.Section1.Magic = 0x6E6A6A5F6473746E  //6E7473645F6A6A6E
	GlobalConf.GameState.Magic = 0x6E6A6A2D6473746E //6E7473642D6A6A6E
}

type SysConf struct {
	_        [64]byte
	Section1 struct {
		Magic               uint64
		CommonBootArgs      []string
		StorageRoot         string `yaml:"storage_root"`
		GameBinDir          string `yaml:"game_dir"`
		ClusterName         string `yaml:"cluster_name"`
		SteamCmdPath        string `yaml:"steamcmd_path"`
		ModLuaBackup        string `yaml:"mod_setup_backup"`
		ModLuaTarget        string `yaml:"mod_setup_target"`
		ModConfPath         string `yaml:"mod_overrides_path"`
		MasterWorldConfPath string `yaml:"master_server_lua"`
		CavesWorldConfPath  string `yaml:"caves_server_lua"`
		HttpBind            string `yaml:"http_api_listen"`
		AnnounceLang        string `yaml:"announcement_language"`
		AutoInstallOnBoot   bool   `yaml:"auto_bootstrap"`
		SkipLinuxCheck      bool   `yaml:"skip_linux_lib32_check"`
		SkipRootCheck       bool   `yaml:"permit_root_usage"`
		AutoStartServer     bool   `yaml:"auto_start"`
	}
	_ [64]byte
	/////////////////////////////////////////
	Section2 struct {
		EnableAutoUpdate    atomic.Bool   `yaml:"auto_update"`
		UpdateCheckInterval atomic.Uint64 `yaml:"update_interval"`
		GraceWaitTime       atomic.Uint64 `yaml:"graceful_restart_wait"`
		WriteDefaultConf    atomic.Bool   `yaml:"auto_gen_default_configs"`
		EnableCrashReboot   atomic.Bool   `yaml:"crash_restart"`
		EnableCaves         atomic.Bool   `yaml:"enable_caves"`
		EnableMaster        atomic.Bool   `yaml:"enable_master"`
		EnableParasite      atomic.Bool   `yaml:"enable_parasite"`
		CavesEpochLink      string        `yaml:"caves_epoch_url"`
		MasterEpochLink     string        `yaml:"master_epoch_url"`
		CavesStateEndpoint  string        `yaml:"caves_state_report_url"`
		cluster_token       string        `yaml:"cluster_token"`
	}
	_ [64]byte
	/////////////////////////////////////////
	AtomicGate struct {
		ServerRunGate    atomic.Bool
		GameUpdatingGate atomic.Bool
		ModUpdatingGate  atomic.Bool
		ModBusyGate      atomic.Bool
		MasterReadyGate  atomic.Bool
		CavesReadyGate   atomic.Bool
		ManualHaltGate   atomic.Bool
		RebootingGate    atomic.Bool
		MasterRxAlive    atomic.Bool
		CavesRxAlive     atomic.Bool
	}
	_ [64]byte
	/////////////////////////////////////////
	ClusterMonState struct {
		SampleInterval atomic.Uint32
		_              [64]byte
		MasterCPU      atomic.Uint32
		MasterMem      atomic.Uint64
		CavesCPU       atomic.Uint32
		CavesMem       atomic.Uint64
	}
	_ [64]byte
	/////////////////////////////////////////
	ProcState struct {
		PID            atomic.Uint64
		MasterEpoch    atomic.Int64
		CurrCavesEpoch atomic.Int64
	}
	_ [64]byte
	/////////////////////////////////////////
	LogState struct {
		MasterStdout  atomic.Bool `yaml:"master_log"`
		MasterLogPath string      `yaml:"master_log_file"`
		CavesStdout   atomic.Bool `yaml:"caves_log"`
		CavesLogPath  string      `yaml:"caves_log_file"`
	}
	_ [64]byte
	/////////////////////////////////////////
	CoreCpuMetrics struct {
		EnableCpuInflationProbe atomic.Bool `yaml:"enable_cpu_inflation_probe"`
		_                       [64]byte
		CPUFrequency            atomic.Uint64
		CurrFrequency           atomic.Uint64
	}
	_ [64]byte
	/////////////////////////////////////////
	GameState struct {
		Magic          uint64
		OnlinePlayers  atomic.Uint32
		WorldDays      atomic.Uint32
		CurrSeason     atomic.Uint32 // 0:秋, 1:冬, 2:春, 3:夏
		DayPhase       atomic.Uint32 // 0:白天, 1:黄昏, 2:夜晚
		SeasonDaysLeft atomic.Uint32
		AbsTemp        atomic.Int32
		IsRaining      atomic.Bool
		IsSnowing      atomic.Bool
		MoonState      atomic.Uint32
		NightmareState atomic.Uint32 // 0:none, 1:calm, 2:warn, 3:wild, 4:dawn
		CelestialWake  atomic.Bool

		DeerclopsTimer       atomic.Uint32
		BeargerTimer         atomic.Uint32
		MooseGooseTimer      atomic.Uint32
		DragonflyTimer       atomic.Uint32
		BeeQueenTimer        atomic.Uint32
		KlausTimer           atomic.Uint32
		ToadstoolTimer       atomic.Uint32
		FuelweaverTimer      atomic.Uint32
		MalbatrossTimer      atomic.Uint32
		FruitFlyLordTimer    atomic.Uint32
		AntlionStompMinTimer atomic.Uint32
	}
	_ [64]byte
	/////////////////////////////////////////
}

var (
	MasterModConfPath    string
	CavesModConfPath     string
	ClusterPath          string
	MasterServerConfPath string
	CavesServerConfPath  string
	MasterWorldConfPath  string
	CavesWorldConfPath   string
	GameVerAcfPath       string
	ModVerAcfPath        string
	WhitelistPath        string
	BlacklistPath        string
	AdminListPath        string
	SteamCmdBinPath      string
	GameBinPath          string

	ModUpdateConfPaths [3]string
	WriteModConfPaths  [2]string
)

func BootNucleus() {
	GlobalConf.Section1.ClusterName = "MyDediServer"
	GlobalConf.Section1.SteamCmdPath = SteamCmdDefPath
	GlobalConf.Section1.AutoInstallOnBoot = false
	GlobalConf.Section1.SkipLinuxCheck = false
	GlobalConf.Section1.SkipRootCheck = false
	GlobalConf.Section1.AutoStartServer = true
	GlobalConf.Section2.EnableAutoUpdate.Store(true)
	GlobalConf.Section2.UpdateCheckInterval.Store(600)
	GlobalConf.Section2.GraceWaitTime.Store(480)
	GlobalConf.Section2.WriteDefaultConf.Store(false)
	GlobalConf.Section2.EnableCrashReboot.Store(true)
	GlobalConf.Section2.EnableCaves.Store(true)
	GlobalConf.Section2.EnableMaster.Store(true)
	GlobalConf.ClusterMonState.SampleInterval.Store(500)

}

func InitGameState() {
	GlobalConf.GameState.OnlinePlayers.Store(4294967295)
	GlobalConf.GameState.WorldDays.Store(4294967295)
	GlobalConf.GameState.CurrSeason.Store(4294967295)
	GlobalConf.GameState.DayPhase.Store(4294967295)
	GlobalConf.GameState.SeasonDaysLeft.Store(4294967295)
	GlobalConf.GameState.AbsTemp.Store(2147483647)
	GlobalConf.GameState.MoonState.Store(4294967295)
	GlobalConf.GameState.NightmareState.Store(4294967295)

	GlobalConf.GameState.DeerclopsTimer.Store(4294967295)
	GlobalConf.GameState.BeargerTimer.Store(4294967295)
	GlobalConf.GameState.MooseGooseTimer.Store(4294967295)
	GlobalConf.GameState.DragonflyTimer.Store(4294967295)
	GlobalConf.GameState.BeeQueenTimer.Store(4294967295)
	GlobalConf.GameState.KlausTimer.Store(4294967295)
	GlobalConf.GameState.ToadstoolTimer.Store(4294967295)
	GlobalConf.GameState.FuelweaverTimer.Store(4294967295)
	GlobalConf.GameState.MalbatrossTimer.Store(4294967295)
	GlobalConf.GameState.FruitFlyLordTimer.Store(4294967295)
	GlobalConf.GameState.AntlionStompMinTimer.Store(4294967295)
}

func InitPaths() {
	MasterModConfPath = filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Master", "modoverrides.lua")
	CavesModConfPath = filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Caves", "modoverrides.lua")
	ClusterPath = filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "cluster.ini")
	WhitelistPath = filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "whitelist.txt")
	BlacklistPath = filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "blocklist.txt")
	AdminListPath = filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "adminlist.txt")
	MasterServerConfPath = filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Master", "server.ini")
	CavesServerConfPath = filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Caves", "server.ini")
	MasterWorldConfPath = filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Master", "worldgenoverride.lua")
	CavesWorldConfPath = filepath.Join(GlobalConf.Section1.StorageRoot, "DoNotStarveTogether", GlobalConf.Section1.ClusterName, "Caves", "worldgenoverride.lua")
	GameVerAcfPath = filepath.Join(GlobalConf.Section1.SteamCmdPath, "steamapps", "appmanifest_343050.acf")
	ModVerAcfPath = filepath.Join(GlobalConf.Section1.SteamCmdPath, "steamapps", "workshop", "appworkshop_322330.acf")
	SteamCmdBinPath = filepath.Join(GlobalConf.Section1.SteamCmdPath, SteamCmdName)
	GameBinPath = filepath.Join(GlobalConf.Section1.GameBinDir, DSTExecName)
	ModUpdateConfPaths[0] = GlobalConf.Section1.ModLuaTarget
	if GlobalConf.Section2.EnableMaster.Load() {
		ModUpdateConfPaths[1] = MasterModConfPath
		WriteModConfPaths[0] = MasterModConfPath
	}
	if GlobalConf.Section2.EnableCaves.Load() {
		ModUpdateConfPaths[2] = CavesModConfPath
		if WriteModConfPaths[0] == "" {
			WriteModConfPaths[0] = CavesModConfPath
		} else {
			WriteModConfPaths[1] = CavesModConfPath
		}
	}
}
