package main

import (
	"sync/atomic"
)

var 全局配置 = &系统配置{}

func init() {
	全局配置.配置区1.Magic = 0x6E6A6A5F6473746E //6E7473642D6A6A6E
	全局配置.游戏内状态.Magic = 0x6E6A6A2D6473746E
}

type 系统配置 struct {
	_    [64]byte
	配置区1 struct {
		Magic          uint64
		通用启动参数         []string
		存档根目录          string `yaml:"storage_root"`
		游戏程序目录         string `yaml:"game_dir"`
		存档名称           string `yaml:"cluster_name"`
		SteamCmd路径     string `yaml:"steamcmd_path"`
		模组Lua更新备份      string `yaml:"mod_setup_backup"`
		模组Lua更新文件目标路径  string `yaml:"mod_setup_target"`
		模组配置文件路径       string `yaml:"mod_overrides_path"`
		主世界world配置文件路径 string `yaml:"master_server_lua"`
		洞穴world配置文件路径  string `yaml:"caves_server_lua"`
		http接口         string `yaml:"http_api_listen"`
		公告语言           string `yaml:"announcement_language"`
		启动后自动安装        bool   `yaml:"auto_bootstrap"`
		跳过linux自检      bool   `yaml:"skip_linux_lib32_check"`
		跳过root自检       bool   `yaml:"permit_root_usage"`
		自动启动服务器        bool   `yaml:"auto_start"`
	}
	_ [64]byte
	/////////////////////////////////////////
	配置区2 struct {
		启用自动更新        atomic.Bool   `yaml:"auto_update"`
		检查更新间隔        atomic.Uint64 `yaml:"update_interval"`
		优雅重启等待时间      atomic.Uint64 `yaml:"graceful_restart_wait"`
		是否写入默认配置      atomic.Bool   `yaml:"auto_gen_default_configs"`
		启用崩溃重启        atomic.Bool   `yaml:"crash_restart"`
		启用洞穴          atomic.Bool   `yaml:"enable_caves"`
		启用主世界         atomic.Bool   `yaml:"enable_master"`
		启用寄生          atomic.Bool   `yaml:"enable_parasite"`
		洞穴世代同步链路      string        `yaml:"caves_epoch_url"`
		主世界世代同步链路     string        `yaml:"master_epoch_url"`
		洞穴状态上报端点      string        `yaml:"caves_state_report_url"`
		cluster_token string        `yaml:"cluster_token"`
	}
	_ [64]byte
	/////////////////////////////////////////
	原子锁 struct {
		允许服务器运行原子锁 atomic.Bool
		游戏正在更新     atomic.Bool
		模组正在更新     atomic.Bool
		主世界就绪原子锁   atomic.Bool
		洞穴就绪原子锁    atomic.Bool
		正在手动关闭原子锁  atomic.Bool
		正在重启原子锁    atomic.Bool
		主世界接收器存活   atomic.Bool
		洞穴接收器存活    atomic.Bool
	}
	_ [64]byte
	/////////////////////////////////////////
	全服监控态 struct {
		采样间隔      atomic.Uint32
		_         [64]byte
		MasterCPU atomic.Uint32
		MasterMem atomic.Uint64
		CavesCPU  atomic.Uint32
		CavesMem  atomic.Uint64
	}
	_ [64]byte
	/////////////////////////////////////////
	进程状态 struct {
		PID     atomic.Uint64
		主世界当前世代 atomic.Int64
		洞穴当前世代  atomic.Int64
	}
	_ [64]byte
	/////////////////////////////////////////
	日志状态 struct {
		主世界输出   atomic.Bool `yaml:"master_log"`
		主世界日志路径 string      `yaml:"master_log_file"`
		洞穴输出    atomic.Bool `yaml:"caves_log"`
		洞穴日志路径  string      `yaml:"caves_log_file"`
	}
	_ [64]byte
	/////////////////////////////////////////
	核心CPU指标 struct {
		启用CPU膨胀探测 atomic.Bool `yaml:"enable_cpu_inflation_probe"`
		_         [64]byte
		CPU频率     atomic.Uint64
		当前频率      atomic.Uint64
	}
	_ [64]byte
	/////////////////////////////////////////
	游戏内状态 struct {
		Magic  uint64
		在线玩家人数 atomic.Uint32
		世界天数   atomic.Uint32
		当前季节   atomic.Uint32 // 0:秋, 1:冬, 2:春, 3:夏
		昼夜阶段   atomic.Uint32 // 0:白天, 1:黄昏, 2:夜晚
		季节剩余天数 atomic.Uint32
		绝对温度   atomic.Int32
		是否下雨   atomic.Bool
		是否下雪   atomic.Bool
		月相状态   atomic.Uint32
		暴动状态   atomic.Uint32 // 0:none, 1:calm, 2:warn, 3:wild, 4:dawn
		天体唤醒   atomic.Bool

		巨鹿倒计时     atomic.Uint32
		熊大倒计时     atomic.Uint32
		大鹅倒计时     atomic.Uint32
		龙蝇倒计时     atomic.Uint32
		蜂后倒计时     atomic.Uint32
		克劳斯倒计时    atomic.Uint32
		蛤蟆倒计时     atomic.Uint32
		织影者倒计时    atomic.Uint32
		邪天翁倒计时    atomic.Uint32
		果蝇王倒计时    atomic.Uint32
		蚁狮踩踏分钟倒计时 atomic.Uint32
	}
	_ [64]byte
	/////////////////////////////////////////
}

var (
	主世界mod配置路径    string
	洞穴mod配置路径     string
	cluster路径     string
	主世界server配置路径 string
	洞穴server配置路径  string
	主世界world配置路径  string
	洞穴world配置路径   string
	游戏版本acf文件路径   string
	mod版本acf文件路径  string
	steamcmd程序路径  string
	游戏程序路径        string

	mod更新配置文件路径集 [3]string
	写入mod配置文件路径集 [2]string
)

func 初始化核心中枢(s *系统配置) {
	s.配置区1.存档名称 = "MyDediServer"
	s.配置区1.SteamCmd路径 = steamcmd默认路径
	s.配置区1.启动后自动安装 = false
	s.配置区1.跳过linux自检 = false
	s.配置区1.跳过root自检 = false
	s.配置区1.自动启动服务器 = true
	s.配置区2.启用自动更新.Store(true)
	s.配置区2.检查更新间隔.Store(600)
	s.配置区2.优雅重启等待时间.Store(480)
	s.配置区2.是否写入默认配置.Store(false)
	s.配置区2.启用崩溃重启.Store(true)
	s.配置区2.启用洞穴.Store(true)
	s.配置区2.启用主世界.Store(true)
	s.全服监控态.采样间隔.Store(500)

}

func 初始化游戏内状态() {
	全局配置.游戏内状态.在线玩家人数.Store(4294967295)
	全局配置.游戏内状态.世界天数.Store(4294967295)
	全局配置.游戏内状态.当前季节.Store(4294967295)
	全局配置.游戏内状态.昼夜阶段.Store(4294967295)
	全局配置.游戏内状态.季节剩余天数.Store(4294967295)
	全局配置.游戏内状态.绝对温度.Store(2147483647)
	全局配置.游戏内状态.月相状态.Store(4294967295)
	全局配置.游戏内状态.暴动状态.Store(4294967295)

	全局配置.游戏内状态.巨鹿倒计时.Store(4294967295)
	全局配置.游戏内状态.熊大倒计时.Store(4294967295)
	全局配置.游戏内状态.大鹅倒计时.Store(4294967295)
	全局配置.游戏内状态.龙蝇倒计时.Store(4294967295)
	全局配置.游戏内状态.蜂后倒计时.Store(4294967295)
	全局配置.游戏内状态.克劳斯倒计时.Store(4294967295)
	全局配置.游戏内状态.蛤蟆倒计时.Store(4294967295)
	全局配置.游戏内状态.织影者倒计时.Store(4294967295)
	全局配置.游戏内状态.邪天翁倒计时.Store(4294967295)
	全局配置.游戏内状态.果蝇王倒计时.Store(4294967295)
	全局配置.游戏内状态.蚁狮踩踏分钟倒计时.Store(4294967295)
}
