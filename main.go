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

var 动作总线 = make(chan 核心执行动作, 1)
var 全局生命周期 context.Context
var 全局生命周期终结 context.CancelFunc

type CancelFunc context.CancelFunc

var 全局局部生命周期终结 atomic.Pointer[CancelFunc]

type 核心执行动作 uint8

const (
	动作_无动作 核心执行动作 = iota
	动作_主世界崩溃
	动作_洞穴崩溃
	动作_执行游戏本体更新
	动作_执行模组热更新
	动作_执行API强制关闭
	动作_执行系统关机
)

func main() {
	全局生命周期, 全局生命周期终结 = context.WithCancel(context.Background())
	defer 全局生命周期终结()

	go func() {
		信号通道 := make(chan os.Signal, 1)
		signal.Notify(信号通道, os.Interrupt, syscall.SIGTERM)

		<-信号通道

		全局生命周期终结()
	}()

	初始化核心中枢(全局配置)
	初始化游戏内状态()

	参数 := os.Args
	长度 := len(参数)
	for i := 1; i < 长度; i++ {
		if 参数[i] == "-c" && i+1 < 长度 {
			配置文件目录 = 参数[i+1]
			break
		}
	}

	_, err := os.Stat(配置文件目录)

	if os.IsNotExist(err) {
		原子写文件(配置文件目录, 默认配置文件)
		控制台合并输出换行(S2B(配置文件目录), S2B("[init] default config generated. manual edit required before start."))
		os.Exit(0)
	}

	if 当前盘面内容, err := os.ReadFile(配置文件目录); err == nil {
		if 校验配置是否为原始默认版本(当前盘面内容, 默认配置文件) {
			控制台合并输出换行(S2B("[warn] config: unmodified default configuration detected"))
			控制台合并输出换行(S2B("[warn] config: running with factory defaults. review settings in "), S2B(配置文件目录))
		}
	}

	加载配置(全局配置, 配置文件目录)

	if !检查linux运行环境(全局配置.配置区1.跳过linux自检, 全局配置.配置区1.跳过root自检) {
		os.Exit(1)
	}

	公告语言初始化()

	if 全局配置.配置区1.游戏程序目录 == "" {
		全局配置.配置区1.游戏程序目录 = filepath.Join(全局配置.配置区1.SteamCmd路径, "steamapps", "common", "Don't Starve Together Dedicated Server", "bin64")
	}
	if 全局配置.配置区1.模组Lua更新文件目标路径 == "" {
		全局配置.配置区1.模组Lua更新文件目标路径 = filepath.Join(全局配置.配置区1.SteamCmd路径, "steamapps", "common", "Don't Starve Together Dedicated Server", "mods", "dedicated_server_mods_setup.lua")
	}
	if 全局配置.配置区1.模组Lua更新备份 == "" {
		全局配置.配置区1.模组Lua更新备份 = filepath.Join(全局配置.配置区1.SteamCmd路径, "steamapps", "common", "Don't Starve Together Dedicated Server", "mods", "dst-nucleus_backup.lua")
	}

	主世界mod配置路径 = filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Master", "modoverrides.lua")
	洞穴mod配置路径 = filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Caves", "modoverrides.lua")
	cluster路径 = filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "cluster.ini")
	主世界server配置路径 = filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Master", "server.ini")
	洞穴server配置路径 = filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Caves", "server.ini")
	主世界world配置路径 = filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Master", "worldgenoverride.lua")
	洞穴world配置路径 = filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Caves", "worldgenoverride.lua")
	游戏版本acf文件路径 = filepath.Join(全局配置.配置区1.SteamCmd路径, "steamapps", "appmanifest_343050.acf")
	mod版本acf文件路径 = filepath.Join(全局配置.配置区1.SteamCmd路径, "steamapps", "workshop", "appworkshop_322330.acf")
	steamcmd程序路径 = filepath.Join(全局配置.配置区1.SteamCmd路径, steamcmd文件名)
	游戏程序路径 = filepath.Join(全局配置.配置区1.游戏程序目录, 饥荒可执行文件名)
	mod更新配置文件路径集[0] = 全局配置.配置区1.模组Lua更新文件目标路径
	if 全局配置.配置区2.启用主世界.Load() {
		mod更新配置文件路径集[1] = 主世界mod配置路径
		写入mod配置文件路径集[0] = 主世界mod配置路径
	}
	if 全局配置.配置区2.启用洞穴.Load() {
		mod更新配置文件路径集[2] = 洞穴mod配置路径
		if 写入mod配置文件路径集[0] == "" {
			写入mod配置文件路径集[0] = 洞穴mod配置路径
		} else {
			写入mod配置文件路径集[1] = 洞穴mod配置路径
		}
	}

	if 全局配置.配置区1.启动后自动安装 {
		自动安装()
	}

	if 全局配置.配置区2.检查更新间隔.Load() < 60 {
		控制台合并输出换行(S2B("[warn] interval too short, auto-adjusted to 60s"))
		全局配置.配置区2.检查更新间隔.Store(60)
	}

	全局配置.配置区1.通用启动参数 = []string{}
	if 全局配置.配置区1.存档根目录 == "" {
		全局配置.配置区1.存档根目录 = 获取默认存档根目录()
		控制台合并输出换行(S2B("[init] storage path not set, falling back to default: "), S2B(全局配置.配置区1.存档根目录))
	} else {
		控制台合并输出换行(S2B("[init] storage path explicitly set to: "), S2B(全局配置.配置区1.存档根目录))
		全局配置.配置区1.通用启动参数 = append(全局配置.配置区1.通用启动参数, "-persistent_storage_root", 全局配置.配置区1.存档根目录)
	}
	全局配置.配置区1.通用启动参数 = append(全局配置.配置区1.通用启动参数, "-console", "-cluster", 全局配置.配置区1.存档名称)

	if 全局配置.配置区2.是否写入默认配置.Load() {
		原子写文件(filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Master", "server.ini"), S2B(默认主世界配置))
		原子写文件(filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Caves", "server.ini"), S2B(默认洞穴配置))
		原子写文件(filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "cluster.ini"), S2B(默认cluster))
	}

	go 监听控制台输入()

	if 全局配置.配置区1.http接口 != "" {
		go 启动本地api接口()
	}

	if 全局配置.配置区1.自动启动服务器 {
		全局配置.原子锁.允许服务器运行原子锁.Store(true)
	}

	for {
		if !全局配置.原子锁.允许服务器运行原子锁.Load() {
			控制台合并输出换行(S2B("[idle] waiting for /api/start signal..."))
			select {
			case <-API唤醒通道:
				控制台合并输出换行(S2B("[init] api signal received, starting boot sequence..."))
			case <-全局生命周期.Done():
				return
			}
		}

		for 全局配置.原子锁.游戏正在更新.Load() || 全局配置.原子锁.模组正在更新.Load() {
			控制台合并输出换行(S2B("[warn] race condition prevented: background update in progress, waiting for locks to release..."))
			time.Sleep(2 * time.Second)
		}

		if 探测游戏更新() == 0 {
			执行游戏更新()
		}

		复制文件(全局配置.配置区1.模组Lua更新备份, 全局配置.配置区1.模组Lua更新文件目标路径)
		复制文件(全局配置.配置区1.模组配置文件路径, filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Master", "modoverrides.lua"))
		复制文件(全局配置.配置区1.模组配置文件路径, filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Caves", "modoverrides.lua"))
		复制文件(全局配置.配置区1.主世界world配置文件路径, filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Master", "worldgenoverride.lua"))
		复制文件(全局配置.配置区1.洞穴world配置文件路径, filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "Caves", "worldgenoverride.lua"))
		if 全局配置.配置区2.cluster_token != "" {
			原子写文件(filepath.Join(全局配置.配置区1.存档根目录, "DoNotStarveTogether", 全局配置.配置区1.存档名称, "cluster_token.txt"), S2B(全局配置.配置区2.cluster_token))
		}

		if 探测模组更新() == 0 {
			执行模组更新()
		}

		生命周期, 终止生命周期 := context.WithCancel(全局生命周期)

		终止函数 := CancelFunc(终止生命周期)
		全局局部生命周期终结.Store(&终止函数)

		select {
		case <-动作总线:
		default:
		}

		if 全局配置.配置区2.启用主世界.Load() {
			wg.Add(1)
			go 运行主世界(生命周期, 终止生命周期)
		}
		if 全局配置.配置区2.启用洞穴.Load() {
			wg.Add(1)
			go 运行洞穴(生命周期, 终止生命周期)
		}
		if 全局配置.配置区2.启用自动更新.Load() {
			go 运行版本监控(生命周期)
		}

		var 触发动作 核心执行动作
		select {
		case 触发动作 = <-动作总线:
		case <-全局生命周期.Done():
			触发动作 = 动作_执行系统关机
		}

		switch 触发动作 {
		case 动作_执行游戏本体更新:
			执行优雅重启(生命周期, 公告_游戏更新, nil)
		case 动作_执行模组热更新:
			控制台合并输出换行(S2B("[core] mod hot-update supported, background downloading..."), nil)
			执行模组更新()
			执行优雅重启(生命周期, 公告_模组更新, nil)
		default:
		}

		终止生命周期()
		wg.Wait()
		控制台合并输出换行(S2B("[core] process group destroyed, fd locks released."))

		switch 触发动作 {
		case 动作_执行游戏本体更新:
			控制台合并输出换行(S2B("[core] overwriting game binaries..."))
			执行游戏更新()
		case 动作_主世界崩溃, 动作_洞穴崩溃:
			控制台合并输出换行(S2B("[core] cleanup done, sleep 3s before dual-shard boot..."))
			if !全局配置.配置区2.启用崩溃重启.Load() {
				return
			}
			time.Sleep(3 * time.Second)
		case 动作_执行API强制关闭:
			控制台合并输出换行(S2B("[core] server terminated forcefully."))
			if !全局配置.配置区2.启用崩溃重启.Load() {
				return
			}
			continue
		case 动作_执行系统关机:
			控制台合并输出换行(S2B("[core] system exit(0)."))
			return
		}
	}
}

func 监听控制台输入() {
	var 物理缓冲 [4096]byte
	var 尾部游标 int

	const 洞穴命令前缀 = "caves:"
	const 洞穴命令前缀长度 = len(洞穴命令前缀)

	启用主世界 := 全局配置.配置区2.启用主世界.Load()

	for {
		n, err := os.Stdin.Read(物理缓冲[尾部游标:])
		if err != nil || n == 0 {
			break
		}
		尾部游标 += n

		处理游标 := 0
		for i := 0; i < 尾部游标; i++ {
			if 物理缓冲[i] == '\n' {
				指令流 := 物理缓冲[处理游标:i]

				if len(指令流) > 0 && 指令流[len(指令流)-1] == '\r' {
					指令流 = 指令流[:len(指令流)-1]
				}

				if string(指令流) == `igegjfmwdhb` {
					debug.FreeOSMemory()
					处理游标 = i + 1
					continue
				}

				if !启用主世界 {
					发送纯文本指令(B2S(指令流), 发往洞穴)
				} else if len(指令流) >= 洞穴命令前缀长度 && string(指令流[:洞穴命令前缀长度]) == 洞穴命令前缀 {
					发送纯文本指令(B2S(指令流[洞穴命令前缀长度:]), 发往洞穴)
				} else {
					发送纯文本指令(B2S(指令流), 发往主世界)
				}
				处理游标 = i + 1
			}
		}

		if 处理游标 < 尾部游标 {
			copy(物理缓冲[:], 物理缓冲[处理游标:尾部游标])
			尾部游标 -= 处理游标
		} else {
			尾部游标 = 0
		}
	}
}
