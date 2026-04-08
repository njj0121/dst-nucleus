package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	状态_执行成功  uint8 = 0
	状态_无资源更新 uint8 = 1
	状态_执行失败  uint8 = 2
	状态_锁拦截   uint8 = 3
)

func 自动安装() {
	for {
		if _, err := os.Stat(steamcmd程序路径); err == nil {
			break
		} else if !os.IsNotExist(err) {
			控制台合并输出换行(S2B("[fatal] steamcmd fs corruption: "), E2B(err))
		}

		控制台合并输出换行(S2B("[init] steamcmd not found at: "), S2B(全局配置.配置区1.SteamCmd路径), S2B(". bootstrapping..."))

		if err := 安装SteamCMD(全局配置.配置区1.SteamCmd路径); err != 0 {
			控制台合并输出换行(S2B("[warn] network or extract error. retrying in 5s..."))
			time.Sleep(5 * time.Second)
			continue
		}

		控制台合并输出换行(S2B("[init] steamcmd bootstrap complete."))
		break
	}

安装游戏:
	for {
		if _, err := os.Stat(游戏程序路径); err == nil {
			break 安装游戏
		} else if !os.IsNotExist(err) {
			控制台合并输出换行(S2B("[fatal] game binary fs corruption: "), E2B(err))
		}
		控制台合并输出换行(S2B("[init] game binary missing ("), S2B(饥荒可执行文件名), S2B("). initiating fresh install..."))
		控制台合并输出换行(S2B("[init] massive payload incoming. expect tcp timeout or long wait."))

		执行游戏更新()

		if _, err := os.Stat(游戏程序路径); err == nil {
			控制台合并输出换行(S2B("[init] game binary verification passed."))
			break
		} else {
			控制台合并输出换行(S2B("[warn] game binary hash mismatch. retrying in 3s..."))
			time.Sleep(3 * time.Second)
		}
	}
}

func 运行版本监控(生命周期 context.Context) {
	检查间隔 := 全局配置.配置区2.检查更新间隔.Load()
	if 检查间隔 < 60 {
		检查间隔 = 60
	}

	定时器 := time.NewTicker(time.Duration(检查间隔) * time.Second)
	defer 定时器.Stop()

	for {
		select {
		case <-生命周期.Done():
			return

		case <-定时器.C:
			if 探测游戏更新() == 状态_执行成功 {
				select {
				case 动作总线 <- 动作_执行游戏本体更新:
				default:
				}
				return
			}

			if 探测模组更新() == 状态_执行成功 {
				select {
				case 动作总线 <- 动作_执行模组热更新:
				default:
				}
				return
			}
		}
	}
}

func 探测游戏更新() uint8 {
	本地版本 := 读取文件获取游戏版本()
	远程版本 := 获取远程版本号()

	if 本地版本 == "" || 远程版本 == "" {
		控制台合并输出换行(S2B("[error] game version probe failed: local("), S2B(本地版本), S2B(") remote("), S2B(远程版本), S2B("), network or permission issue"))
		return 状态_执行失败
	}

	if 本地版本 != 远程版本 {
		控制台合并输出换行(S2B("[info] game update available: local("), S2B(本地版本), S2B(") -> remote("), S2B(远程版本), S2B(")"))
		return 状态_执行成功
	}

	return 状态_无资源更新
}

var 全局目标模组缓存 = make([]uint64, 0, 256)

func 探测模组更新() uint8 {
	if !全局配置.原子锁.模组正在繁忙.CompareAndSwap(false, true) {
		控制台合并输出换行(S2B("[core] mod update lock denied. already in progress."))
		return 状态_锁拦截
	}
	defer 全局配置.原子锁.模组正在繁忙.Store(false)
	模组列表, err := 读取行()
	if err != 0 {
		控制台合并输出换行(S2B("[sys] mod.txt io error: "), S2B(全局配置.配置区1.模组Lua更新文件目标路径))
		return 状态_执行失败
	}

	目标模组 := 全局目标模组缓存[:0]
	for _, m := range 模组列表 {
		if m != 0 {
			目标模组 = append(目标模组, m)
		}
	}

	if len(目标模组) == 0 {
		return 状态_无资源更新
	}

	本地时间表, _ := 获取本地模组时间表(目标模组)
	远程时间表, err := 获取远程模组更新时间(目标模组)

	if err != 0 {
		控制台合并输出换行(S2B("[sys] mod steam api query failed."))
		return 状态_执行失败
	}

	发现更新 := false
	var 栈缓冲 [64]byte
	var id栈缓冲 [24]byte

	for _, 模组ID := range 目标模组 {
		本地时间 := 本地时间表[模组ID]
		远程时间, 存在 := 远程时间表[模组ID]

		if 存在 && 远程时间 > 本地时间 {
			发现更新 = true

			载荷 := 栈缓冲[:0]
			载荷 = strconv.AppendInt(载荷, 本地时间, 10)
			载荷 = append(载荷, '-')
			载荷 = append(载荷, '>')
			载荷 = strconv.AppendInt(载荷, 远程时间, 10)

			ID := strconv.AppendUint(id栈缓冲[:0], 模组ID, 10)
			控制台合并输出换行(S2B("[core] mod update triggered: ["), ID, S2B("] "), 载荷)

		}
	}

	if 发现更新 {
		return 状态_执行成功
	}

	return 状态_无资源更新
}

func 执行游戏更新() uint8 {
	if !全局配置.原子锁.游戏正在更新.CompareAndSwap(false, true) {
		控制台合并输出换行(S2B("[core] game update lock denied. already in progress."))
		return 状态_锁拦截
	}
	defer 全局配置.原子锁.游戏正在更新.Store(false)

	控制台合并输出换行(S2B("[core] dispatching steamcmd for game update..."))

	steamcmd进程 := exec.CommandContext(全局生命周期, steamcmd程序路径, "+login", "anonymous", "+app_update", "343050", "validate", "+quit")
	绑定子进程生命周期(steamcmd进程)

	steamcmd进程.Stdout = os.Stdout
	steamcmd进程.Stderr = os.Stderr

	if err := steamcmd进程.Start(); err != nil {
		控制台合并输出换行(S2B("[fatal] steamcmd spawn failed: "), E2B(err))
		return 状态_执行失败
	}

	设置进程退出信号(steamcmd进程)

	if err := steamcmd进程.Wait(); err != nil {
		控制台合并输出换行(S2B("[fatal] steamcmd tcp stream broken: "), E2B(err))
		return 状态_执行失败
	}

	复制文件(全局配置.配置区1.模组Lua更新备份, 全局配置.配置区1.模组Lua更新文件目标路径)

	控制台合并输出换行(S2B("[core] game binary overwritten."))
	return 状态_执行成功
}

func 执行模组更新() uint8 {
	if !全局配置.原子锁.模组正在繁忙.CompareAndSwap(false, true) {
		控制台合并输出换行(S2B("[core] mod update lock denied. already in progress."))
		return 状态_锁拦截
	}
	defer 全局配置.原子锁.模组正在繁忙.Store(false)
	全局配置.原子锁.模组正在更新.Store(true)
	defer 全局配置.原子锁.模组正在更新.Store(false)

	模组列表, err := 读取行()
	if err != 0 {
		控制台合并输出换行(S2B("[fatal] mod update aborted. io error: "), S2B(全局配置.配置区1.模组Lua更新文件目标路径))
		return 状态_执行失败
	}

	var 目标模组 []uint64
	for _, m := range 模组列表 {
		if m != 0 {
			目标模组 = append(目标模组, m)
		}
	}

	if len(目标模组) == 0 {
		控制台合并输出换行(S2B("[core] mod.txt empty. bypassing mod update."))
		return 状态_无资源更新
	}

	最终更新名单 := 目标模组

	本地时间表, err1 := 获取本地模组时间表(目标模组)
	远程时间表, err2 := 获取远程模组更新时间(目标模组)

	if err1 == 0 && err2 == 0 {
		var 差异模组 []uint64
		for _, modID := range 目标模组 {
			本地时间 := 本地时间表[modID]
			远程时间, 存在 := 远程时间表[modID]

			if 存在 && 远程时间 > 本地时间 {
				差异模组 = append(差异模组, modID)
			}
		}

		if len(差异模组) > 0 {
			最终更新名单 = 差异模组
			控制台合并输出换行(S2B("[core] diff logic matched "), strconv.AppendInt(make([]byte, 0, 8), int64(len(最终更新名单)), 10), S2B(" outdated mods, performing incremental update..."))
		} else {
			控制台合并输出换行(S2B("[warn] diff logic failed. forcing full fallback update..."))
		}
	} else {
		控制台合并输出换行(S2B("[warn] steam api rejected query. downgrading to full blind update..."))
	}

	参数 := []string{"+login", "anonymous"}
	for _, m := range 最终更新名单 {
		参数 = append(参数, "+workshop_download_item", "322330", strconv.FormatUint(m, 10))
	}
	参数 = append(参数, "+quit")

	var 栈缓冲 [8]byte
	数量缓存 := strconv.AppendInt(栈缓冲[:0], int64(len(最终更新名单)), 10)
	控制台合并输出换行(S2B("[core] injecting "), 数量缓存, S2B(" mod instructions to steamcmd..."))

	steamcmd进程 := exec.CommandContext(全局生命周期, steamcmd程序路径, 参数...)
	绑定子进程生命周期(steamcmd进程)

	steamcmd进程.Stdout = os.Stdout
	steamcmd进程.Stderr = os.Stderr

	if err := steamcmd进程.Start(); err != nil {
		控制台合并输出换行(S2B("[fatal] steamcmd spawn failed: "), E2B(err))
		return 状态_执行失败
	}

	设置进程退出信号(steamcmd进程)

	if err := steamcmd进程.Wait(); err != nil {
		控制台合并输出换行(S2B("[fatal] steamcmd process killed mid-flight: "), E2B(err))
		return 状态_执行失败
	}

	控制台合并输出换行(S2B("[core] mod queue processed."))
	return 状态_执行成功
}

func 读取文件获取游戏版本() string {
	const 最大重试次数 = 3
	target := S2B(`"TargetBuildID"`)

	for i := 0; i < 最大重试次数; i++ {
		读前状态, err := os.Stat(游戏版本acf文件路径)
		if err != nil {
			return ""
		}

		f, err := os.Open(游戏版本acf文件路径)
		if err != nil {
			return ""
		}

		var buf [32 * 1024]byte
		tailLen := 0
		var match string

		for {
			n, err := f.Read(buf[tailLen:])
			total := tailLen + n
			data := buf[:total]

			offset := 0
			for {
				idx := bytes.Index(data[offset:], target)
				if idx == -1 {
					break
				}

				absIdx := offset + idx
				if total-absIdx > 128 || err != nil {
					match = 解析版本号(data[absIdx+len(target):])
					if match != "" {
						break
					}
					offset = absIdx + len(target)
				} else {
					break
				}
			}

			if match != "" || err != nil {
				break
			}

			if total > 128 {
				tailLen = 128
				copy(buf[:tailLen], buf[total-128:total])
			} else {
				tailLen = total
			}
		}
		f.Close()

		读后状态, err := os.Stat(游戏版本acf文件路径)
		if err != nil {
			return ""
		}

		if 读前状态.ModTime().Equal(读后状态.ModTime()) && 读前状态.Size() == 读后状态.Size() {
			return match
		}

		time.Sleep(50 * time.Millisecond)
	}

	return ""
}

func 解析版本号(data []byte) string {
	p := 0
	长度 := len(data)

	for p < 长度 && (data[p] == ' ' || data[p] == '\t') {
		p++
	}

	if p < 长度 && data[p] == '"' {
		p++
		start := p
		for p < 长度 && data[p] >= '0' && data[p] <= '9' {
			p++
		}
		if p > start && p < 长度 && data[p] == '"' {
			return B2S(data[start:p])
		}
	}
	return ""
}

func 获取文件快照(文件路径 string) ([]byte, uint8) {
	const 最大重试次数 = 3
	const 防内存爆炸大小 = 1024 * 1024

	for i := 0; i < 最大重试次数; i++ {
		读前状态, err := os.Stat(文件路径)
		if err != nil {
			return nil, 128
		}

		if 读前状态.Size() > 防内存爆炸大小 {
			控制台合并输出换行(S2B("[warn] refusing to buffer massive payload (>1MB): "), S2B(文件路径))
			return nil, 132
		}

		内容, err := os.ReadFile(文件路径)
		if err != nil {
			return nil, 129
		}

		读后状态, err := os.Stat(文件路径)
		if err != nil {
			return nil, 130
		}

		if 读前状态.ModTime().Equal(读后状态.ModTime()) && 读前状态.Size() == 读后状态.Size() {
			return 内容, 0
		}

		time.Sleep(50 * time.Millisecond)
	}

	控制台合并输出换行(S2B("[fatal] concurrent io tear detected during snapshot."))
	return nil, 131
}

func 获取远程版本号() string {
	版本号, err := 通过HTTP获取远程游戏版本()
	if err == 0 && 版本号 != "" {
		return 版本号
	}
	控制台合并输出换行(S2B("[warn] http probe failed/timeout. fallback to steamcmd local query."))

	steamcmd进程 := exec.CommandContext(全局生命周期, steamcmd程序路径, "+login", "anonymous", "+app_info_update", "1", "+app_info_print", "343050", "+quit")
	绑定子进程生命周期(steamcmd进程)

	stdoutPipe, err1 := steamcmd进程.StdoutPipe()
	if err1 != nil {
		控制台合并输出换行(S2B("[sys] steamcmd pipe creation failed."))
		return ""
	}

	if err := steamcmd进程.Start(); err != nil {
		控制台合并输出换行(S2B("[sys] steamcmd start failed."))
		return ""
	}

	设置进程退出信号(steamcmd进程)

	输出, err2 := io.ReadAll(stdoutPipe)

	if err := steamcmd进程.Wait(); err != nil || err2 != nil {
		控制台合并输出换行(S2B("[sys] steamcmd query failed."))
		return ""
	}

	节点锚点 := bytes.Index(输出, []byte(`"public"`))
	if 节点锚点 == -1 {
		return ""
	}

	目标偏移 := bytes.Index(输出[节点锚点:], []byte(`"buildid"`))
	if 目标偏移 == -1 {
		return ""
	}

	游标 := 节点锚点 + 目标偏移 + len(`"buildid"`)

	for 游标 < len(输出) && (输出[游标] < '0' || 输出[游标] > '9') {
		游标++
	}
	数字起点 := 游标

	for 游标 < len(输出) && 输出[游标] >= '0' && 输出[游标] <= '9' {
		游标++
	}

	if 游标 > 数字起点 {
		return B2S(输出[数字起点:游标])
	}

	return ""
}

func 通过HTTP获取远程游戏版本() (string, uint8) {
	apiURL := "https://api.steamcmd.net/v1/info/343050"

	body := 发起竞速请求(apiURL, "")
	if body == nil {
		return "", 128
	}

	分支锚点 := bytes.Index(body, []byte(`"branches"`))
	if 分支锚点 == -1 {
		控制台合并输出换行(S2B("[sys] ast parser: missing 'branches' node"))
		return "", 130
	}

	节点锚点 := bytes.Index(body[分支锚点:], []byte(`"public"`))
	if 节点锚点 == -1 {
		控制台合并输出换行(S2B("[sys] ast parser: missing 'public' node"))
		return "", 130
	}
	绝对起始点 := 分支锚点 + 节点锚点

	目标偏移 := bytes.Index(body[绝对起始点:], []byte(`"buildid"`))
	if 目标偏移 == -1 {
		控制台合并输出换行(S2B("[sys] ast parser: missing 'buildid' node"))
		return "", 130
	}

	游标 := 绝对起始点 + 目标偏移 + len(`"buildid"`)

	for 游标 < len(body) && (body[游标] < '0' || body[游标] > '9') {
		游标++
	}
	数字起点 := 游标

	for 游标 < len(body) && body[游标] >= '0' && body[游标] <= '9' {
		游标++
	}

	if 游标 > 数字起点 {
		return B2S(body[数字起点:游标]), 0
	}

	控制台合并输出换行(S2B("[sys] ast parser: buildid overflow"))
	return "", 130
}

var (
	代理客户端 = &http.Client{
		Timeout: 15 * time.Second,
	}
	直连客户端 = &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
)

// 由于国内网络环境极差，为了确保环境变量配置了代理，但有时代理可能失效的情况，同时发起代理和直连请求，哪个快看哪个
func 发起竞速请求(目标URL string, 原始载荷 string) []byte {
	竞速通道 := make(chan []byte, 2)

	发起请求 := func(探针 *http.Client) {
		var req *http.Request
		var err error

		if len(原始载荷) > 0 {
			req, err = http.NewRequest("POST", 目标URL, strings.NewReader(原始载荷))
			if err == nil {
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
		} else {
			req, err = http.NewRequest("GET", 目标URL, nil)
		}

		if err != nil {
			竞速通道 <- nil
			return
		}

		resp, err := 探针.Do(req)
		if err != nil {
			竞速通道 <- nil
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			竞速通道 <- nil
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			竞速通道 <- nil
			return
		}

		竞速通道 <- body
	}

	go 发起请求(代理客户端)
	go 发起请求(直连客户端)

	失败计数 := 0
	for {
		select {
		case 响应 := <-竞速通道:
			if 响应 != nil {
				return 响应
			}
			失败计数++
			if 失败计数 == 2 {
				return nil
			}
		case <-time.After(5 * time.Second):
			return nil
		}
	}
}

var 字符合法表 = [256]byte{
	'0': 1, '1': 1, '2': 1, '3': 1, '4': 1, '5': 1, '6': 1, '7': 1, '8': 1, '9': 1,
	'A': 1, 'B': 1, 'C': 1, 'D': 1, 'E': 1, 'F': 1, 'G': 1, 'H': 1, 'I': 1, 'J': 1,
	'K': 1, 'L': 1, 'M': 1, 'N': 1, 'O': 1, 'P': 1, 'Q': 1, 'R': 1, 'S': 1, 'T': 1,
	'U': 1, 'V': 1, 'W': 1, 'X': 1, 'Y': 1, 'Z': 1,
	'a': 1, 'b': 1, 'c': 1, 'd': 1, 'e': 1, 'f': 1, 'g': 1, 'h': 1, 'i': 1, 'j': 1,
	'k': 1, 'l': 1, 'm': 1, 'n': 1, 'o': 1, 'p': 1, 'q': 1, 'r': 1, 's': 1, 't': 1,
	'u': 1, 'v': 1, 'w': 1, 'x': 1, 'y': 1, 'z': 1,
	'_': 1,
}

func 是否为变量字符(b byte) bool {
	//经过实际压力测试，查找表法比位图法快1倍以上
	return 字符合法表[b] == 1
}

// dedicated_server_mods_setup.lua
func 解析Setup(内容 []byte, 结果 *[]uint64, 排重字典 map[uint64]struct{}, setup独有 map[uint64]struct{}) {
	长度 := len(内容)
	游标 := 0

	for 游标 < 长度 {
		c := 内容[游标]

		if c == '-' && 游标+1 < 长度 && 内容[游标+1] == '-' {
			游标 += 2
			for 游标 < 长度 && 内容[游标] != '\n' {
				游标++
			}
			continue
		}

		if c == '"' || c == '\'' {
			引号 := c
			游标++
			for 游标 < 长度 {
				if 内容[游标] == '\\' {
					游标 += 2
					continue
				}
				if 内容[游标] == 引号 {
					游标++
					break
				}
				游标++
			}
			continue
		}

		命中 := false
		if 游标+16 <= 长度 && (string(内容[游标:游标+16]) == "ServerModSetup(\"" || string(内容[游标:游标+16]) == "ServerModSetup('") {
			游标 += 16
			命中 = true
		} else if 游标+9 <= 长度 && string(内容[游标:游标+9]) == "workshop-" {
			游标 += 9
			命中 = true
		}

		if 命中 {
			数字起点 := 游标
			for 游标 < 长度 && 内容[游标] >= '0' && 内容[游标] <= '9' {
				游标++
			}
			if 游标 > 数字起点 {
				ID := 字节转Uint64(内容[数字起点:游标])
				if _, 存在 := 排重字典[ID]; !存在 {
					排重字典[ID] = struct{}{}
					*结果 = append(*结果, ID)
				}
				setup独有[ID] = struct{}{}
			}

			for 游标 < 长度 && (内容[游标] == '"' || 内容[游标] == '\'' || 内容[游标] == ')') {
				游标++
			}

			continue
		}
		游标++
	}
}

// modoverrides.lua
func 解析Modoverrides(内容 []byte, 结果 *[]uint64, 排重字典 map[uint64]struct{}) {
	长度 := len(内容)
	游标 := 0
	深度 := 0

	var 当前ID uint64
	当前ID被禁用 := false

	for 游标 < 长度 {
		c := 内容[游标]

		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			游标++
			continue
		}

		if c == '-' && 游标+1 < 长度 && 内容[游标+1] == '-' {
			游标 += 2
			for 游标 < 长度 && 内容[游标] != '\n' {
				游标++
			}
			continue
		}

		if c == '"' || c == '\'' {
			引号 := c
			游标++
			for 游标 < 长度 {
				if 内容[游标] == '\\' {
					游标 += 2
					continue
				}
				if 内容[游标] == 引号 {
					游标++
					break
				}
				游标++
			}
			continue
		}

		if c == '{' {
			深度++
			游标++
			continue
		}

		if c == '}' {
			if 深度 == 2 && 当前ID != 0 {
				if !当前ID被禁用 {
					if _, 存在 := 排重字典[当前ID]; !存在 {
						排重字典[当前ID] = struct{}{}
						*结果 = append(*结果, 当前ID)
					}
				}
				当前ID = 0
			}
			深度--
			游标++
			continue
		}

		if 深度 == 1 && c == '[' {
			临时游标 := 游标 + 1
			for 临时游标 < 长度 && (内容[临时游标] == ' ' || 内容[临时游标] == '\t' || 内容[临时游标] == '"' || 内容[临时游标] == '\'') {
				临时游标++
			}

			if 临时游标+9 <= 长度 && string(内容[临时游标:临时游标+9]) == "workshop-" {
				临时游标 += 9
				数字起点 := 临时游标
				for 临时游标 < 长度 && 内容[临时游标] >= '0' && 内容[临时游标] <= '9' {
					临时游标++
				}
				if 临时游标 > 数字起点 {
					可能ID := 字节转Uint64(内容[数字起点:临时游标])
					for 临时游标 < 长度 && (内容[临时游标] == ' ' || 内容[临时游标] == '\t' || 内容[临时游标] == '"' || 内容[临时游标] == '\'') {
						临时游标++
					}
					if 临时游标 < 长度 && 内容[临时游标] == ']' {
						当前ID = 可能ID
						当前ID被禁用 = false
						游标 = 临时游标 + 1
						continue
					}
				}
			}
		}

		if 深度 == 2 && 当前ID != 0 && c == 'e' {
			if 游标+7 <= 长度 && string(内容[游标:游标+7]) == "enabled" {
				前缀干净 := 游标 == 0 || !是否为变量字符(内容[游标-1])
				后缀干净 := 游标+7 == 长度 || !是否为变量字符(内容[游标+7])

				if 前缀干净 && 后缀干净 {
					检测游标 := 游标 + 7
					找到等号 := false
					for 检测游标 < 长度 {
						k := 内容[检测游标]
						if k == ' ' || k == '\t' || k == '\r' || k == '\n' {
							检测游标++
							continue
						}
						if k == '=' {
							找到等号 = true
							检测游标++
							break
						}
						break
					}

					if 找到等号 {
						for 检测游标 < 长度 {
							k := 内容[检测游标]
							if k == ' ' || k == '\t' || k == '\r' || k == '\n' {
								检测游标++
								continue
							}
							if k == 'f' || k == 'F' {
								当前ID被禁用 = true
							}
							break
						}
						游标 = 检测游标
						continue
					}
				}
			}
		}

		游标++
	}
}

var 模组字典池 = sync.Pool{
	New: func() any {
		return make(map[uint64]struct{}, 64)
	},
}

func 归还模组字典池(池 *sync.Pool, m map[uint64]struct{}) {
	clear(m)
	池.Put(m)
}

func 读取行() ([]uint64, uint8) {
	排重字典 := 模组字典池.Get().(map[uint64]struct{})
	setup独有字典 := 模组字典池.Get().(map[uint64]struct{})

	defer 归还模组字典池(&模组字典池, 排重字典)
	defer 归还模组字典池(&模组字典池, setup独有字典)

	var 结果 []uint64

	for 文件索引, 路径 := range mod更新配置文件路径集 {
		if 路径 == "" {
			continue
		}

		内容, err := 获取文件快照(路径)
		if err != 0 || len(内容) == 0 {
			continue
		}

		if 文件索引 == 0 {
			解析Setup(内容, &结果, 排重字典, setup独有字典)
		} else {
			解析Modoverrides(内容, &结果, 排重字典)
		}
	}

	var 缺失的配置 []byte

	for _, ID := range 结果 {
		if _, 存在 := setup独有字典[ID]; !存在 {
			缺失的配置 = append(缺失的配置, "ServerModSetup(\""...)
			缺失的配置 = strconv.AppendUint(缺失的配置, ID, 10)
			缺失的配置 = append(缺失的配置, "\")\n"...)
		}
	}

	if len(缺失的配置) > 0 {
		f, err := os.OpenFile(mod更新配置文件路径集[0], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			f.Write(缺失的配置)
			f.Sync()
			f.Close()
			控制台合并输出换行(S2B("[sys] unhandled mod detected. hot-patching setup.lua with "), strconv.AppendInt(make([]byte, 0, 4), int64(bytes.Count(缺失的配置, []byte("\n"))), 10), S2B(" payloads."))
		}
	}

	return 结果, 0
}

var (
	全局本地模组时间表 = make(map[uint64]int64, 128)
	全局远程模组时间表 = make(map[uint64]int64, 128)
)

func 获取本地模组时间表(目标模组列表 []uint64) (map[uint64]int64, uint8) {
	for k := range 全局本地模组时间表 {
		delete(全局本地模组时间表, k)
	}
	acf文件目录 := filepath.Join(全局配置.配置区1.SteamCmd路径, "steamapps", "workshop", "appworkshop_322330.acf")

	内容, err := 获取文件快照(acf文件目录)
	if err != 0 {
		return make(map[uint64]int64), err
	}

	扫描器 := bufio.NewScanner(bytes.NewReader(内容))

	目标集合 := make(map[uint64]bool)
	for _, id := range 目标模组列表 {
		目标集合[id] = true
	}

	var currentModID uint64
	特征时间前缀 := []byte(`"timeupdated"`)

	for 扫描器.Scan() {
		行 := bytes.TrimSpace(扫描器.Bytes())

		if len(行) > 2 && 行[0] == '"' && 行[len(行)-1] == '"' {
			可能ID := 行[1 : len(行)-1]

			是纯数字 := true
			for i := 0; i < len(可能ID); i++ {
				if 可能ID[i] < '0' || 可能ID[i] > '9' {
					是纯数字 = false
					break
				}
			}

			if 是纯数字 {
				可能ID_num := 字节转Uint64(可能ID)
				if 目标集合[可能ID_num] {
					currentModID = 可能ID_num
				} else {
					currentModID = 0
				}
				continue
			}
		}

		if currentModID != 0 && bytes.HasPrefix(行, 特征时间前缀) {
			右引号位置 := bytes.LastIndexByte(行, '"')
			if 右引号位置 > 0 {
				左引号位置 := bytes.LastIndexByte(行[:右引号位置], '"')
				if 左引号位置 > 0 {
					ts, _ := strconv.ParseInt(B2S(行[左引号位置+1:右引号位置]), 10, 64)
					全局本地模组时间表[currentModID] = ts

					currentModID = 0
				}
			}
		}
	}
	return 全局本地模组时间表, 0
}

var (
	全局模组API缓冲锁 sync.Mutex
	全局API缓冲    [65536]byte
)

func 获取远程模组更新时间(模组列表 []uint64) (map[uint64]int64, uint8) {
	for k := range 全局远程模组时间表 {
		delete(全局远程模组时间表, k)
	}
	if len(模组列表) == 0 {
		return nil, 0
	}

	const apiURL = "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/"

	全局模组API缓冲锁.Lock()
	游标 := 0

	游标 += copy(全局API缓冲[游标:], "itemcount=")
	数字串 := strconv.AppendInt(全局API缓冲[游标:游标], int64(len(模组列表)), 10)
	游标 += len(数字串)

	for 索引, 模组ID := range 模组列表 {
		全局API缓冲[游标] = '&'
		游标++
		游标 += copy(全局API缓冲[游标:], "publishedfileids[")
		数字串 = strconv.AppendInt(全局API缓冲[游标:游标], int64(索引), 10)
		游标 += len(数字串)
		游标 += copy(全局API缓冲[游标:], "]=")
		ID := strconv.AppendUint(全局API缓冲[游标:游标], 模组ID, 10)
		游标 += len(ID)
	}

	请求载荷 := string(全局API缓冲[:游标])
	全局模组API缓冲锁.Unlock()

	body := 发起竞速请求(apiURL, 请求载荷)
	if body == nil {
		return nil, 128
	}

	偏移量 := 0
	载荷长度 := len(body)

	特征ID := []byte(`"publishedfileid":"`)
	特征时间 := []byte(`"time_updated":`)

	for 偏移量 < 载荷长度 {
		id锚点 := bytes.Index(body[偏移量:], 特征ID)
		if id锚点 == -1 {
			break
		}
		偏移量 += id锚点 + len(特征ID)

		右引号 := bytes.IndexByte(body[偏移量:], '"')
		if 右引号 == -1 {
			break
		}
		modID := 字节转Uint64(body[偏移量 : 偏移量+右引号])
		偏移量 += 右引号 + 1

		时间锚点 := bytes.Index(body[偏移量:], 特征时间)
		if 时间锚点 == -1 {
			break
		}
		偏移量 += 时间锚点 + len(特征时间)

		数字开始 := 偏移量
		for 数字开始 < 载荷长度 && body[数字开始] == ' ' {
			数字开始++
		}
		数字结束 := 数字开始
		for 数字结束 < 载荷长度 && body[数字结束] >= '0' && body[数字结束] <= '9' {
			数字结束++
		}

		时间数字 := string(body[数字开始:数字结束])
		ts, _ := strconv.ParseInt(时间数字, 10, 64)
		全局远程模组时间表[modID] = ts

		偏移量 = 数字结束
	}

	return 全局远程模组时间表, 0
}

func 复制文件(源路径, 目标路径 string) (int64, uint8) {
	const 最大重试次数 = 3

	for i := 0; i < 最大重试次数; i++ {
		读前状态, err := os.Stat(源路径)
		if err != nil {
			return 0, 128
		}

		源文件, err := os.Open(源路径)
		if err != nil {
			return 0, 129
		}

		目标目录 := filepath.Dir(目标路径)
		os.MkdirAll(目标目录, 0755)

		临时文件, err := os.CreateTemp(目标目录, "tmp_clone_*")
		if err != nil {
			源文件.Close()
			return 0, 130
		}
		临时路径 := 临时文件.Name()

		拷贝字节数, err := io.Copy(临时文件, 源文件)
		源文件.Close()

		if err != nil {
			临时文件.Close()
			os.Remove(临时路径)
			控制台合并输出换行(S2B("[sys] kernel copy interrupted: "), E2B(err))
			return 0, 128
		}

		临时文件.Sync()
		临时文件.Close()

		读后状态, err := os.Stat(源路径)
		if err != nil {
			os.Remove(临时路径)
			控制台合并输出换行(E2B(err))
			return 0, 129
		}

		if !读前状态.ModTime().Equal(读后状态.ModTime()) || 读前状态.Size() != 读后状态.Size() {
			os.Remove(临时路径)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		var renameErr error
		for j := 0; j < 5; j++ {
			renameErr = os.Rename(临时路径, 目标路径)
			if renameErr == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if renameErr != nil {
			os.Remove(临时路径)
			控制台合并输出换行(S2B("[sys] atomic rename access denied: "), E2B(renameErr))
			return 0, 130
		}

		return 拷贝字节数, 0
	}

	控制台合并输出换行(S2B("[fatal] continuous physical concurrency tear during clone."))
	return 0, 131
}

func 字节转Uint64(b []byte) uint64 {
	var n uint64
	for _, v := range b {
		n = n*10 + uint64(v-'0')
	}
	return n
}
