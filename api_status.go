package main

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const 环形缓冲深度 = 32
const 寻址掩码 = 环形缓冲深度 - 1
const 单槽位容量 = 1024

var (
	// 为了防止windows报毒，如果不提前分配好所有内存，深度扫描会认定为无毒，但是动态分析可能会认为是木马
	连续内存块 [环形缓冲深度 * 单槽位容量]byte

	状态环形阵列 [环形缓冲深度][]byte
	环形游标   uint32

	当前状态快照 atomic.Pointer[[]byte]
)

func init() {
	for i := 0; i < 环形缓冲深度; i++ {
		起始物理偏移 := i * 单槽位容量
		状态环形阵列[i] = 连续内存块[起始物理偏移 : 起始物理偏移 : 起始物理偏移+单槽位容量]
	}

	初始空状态 := []byte(`{"status":"loading"}`)
	当前状态快照.Store(&初始空状态)
}

// 18212226                64.72 ns/op            0 B/op          0 allocs/op
func 刷新服务器状态() {
	新游标 := atomic.AddUint32(&环形游标, 1)
	当前槽位 := (新游标 - 1) & 寻址掩码

	buf := 状态环形阵列[当前槽位][:0]

	buf = append(buf, `{"status":"success","locks":{"game_upd":`...)
	buf = strconv.AppendBool(buf, 全局配置.原子锁.游戏正在更新.Load())

	buf = append(buf, `,"mod_upd":`...)
	buf = strconv.AppendBool(buf, 全局配置.原子锁.模组正在更新.Load())

	buf = append(buf, `,"master_rdy":`...)
	buf = strconv.AppendBool(buf, 全局配置.原子锁.主世界就绪原子锁.Load())

	buf = append(buf, `,"caves_rdy":`...)
	buf = strconv.AppendBool(buf, 全局配置.原子锁.洞穴就绪原子锁.Load())

	buf = append(buf, `},"performance":{"baseline_cycles":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.核心CPU指标.CPU频率.Load()), 10)

	buf = append(buf, `,"current_cycles":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.核心CPU指标.当前频率.Load()), 10)

	buf = append(buf, `},"data":{"players":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.在线玩家人数.Load()), 10)

	buf = append(buf, `,"enable_caves":`...)
	buf = strconv.AppendBool(buf, 全局配置.配置区2.启用洞穴.Load())

	buf = append(buf, `,"enable_master":`...)
	buf = strconv.AppendBool(buf, 全局配置.配置区2.启用主世界.Load())

	buf = append(buf, `,"cycles":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.世界天数.Load()), 10)

	buf = append(buf, `,"season":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.当前季节.Load()), 10)

	buf = append(buf, `,"phase":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.昼夜阶段.Load()), 10)

	buf = append(buf, `,"rem_days":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.季节剩余天数.Load()), 10)

	buf = append(buf, `,"temp":`...)
	buf = strconv.AppendInt(buf, int64(全局配置.游戏内状态.绝对温度.Load()), 10)

	buf = append(buf, `,"is_raining":`...)
	buf = strconv.AppendBool(buf, 全局配置.游戏内状态.是否下雨.Load())

	buf = append(buf, `,"is_snowing":`...)
	buf = strconv.AppendBool(buf, 全局配置.游戏内状态.是否下雪.Load())

	buf = append(buf, `,"moon_phase":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.月相状态.Load()), 10)

	buf = append(buf, `,"nightmare":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.暴动状态.Load()), 10)

	buf = append(buf, `,"alter_awake":`...)
	buf = strconv.AppendBool(buf, 全局配置.游戏内状态.天体唤醒.Load())

	buf = append(buf, `,"boss_timers":{`...)

	buf = append(buf, `"deerclops":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.巨鹿倒计时.Load()), 10)

	buf = append(buf, `,"bearger":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.熊大倒计时.Load()), 10)

	buf = append(buf, `,"moose":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.大鹅倒计时.Load()), 10)

	buf = append(buf, `,"dragonfly":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.龙蝇倒计时.Load()), 10)

	buf = append(buf, `,"beequeen":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.蜂后倒计时.Load()), 10)

	buf = append(buf, `,"klaus":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.克劳斯倒计时.Load()), 10)

	buf = append(buf, `,"toadstool":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.蛤蟆倒计时.Load()), 10)

	buf = append(buf, `,"fuelweaver":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.织影者倒计时.Load()), 10)

	buf = append(buf, `,"malbatross":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.邪天翁倒计时.Load()), 10)

	buf = append(buf, `,"lordfruitfly":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.果蝇王倒计时.Load()), 10)

	buf = append(buf, `,"antlion":`...)
	buf = strconv.AppendUint(buf, uint64(全局配置.游戏内状态.蚁狮踩踏分钟倒计时.Load()), 10)

	buf = append(buf, `}}}`...)

	状态环形阵列[当前槽位] = buf
	目标指针 := &状态环形阵列[当前槽位]

	当前状态快照.Store(目标指针)

	sse观察者矩阵.Range(func(key, value any) bool {
		select {
		case key.(chan struct{}) <- struct{}{}:
		default:
		}
		return true
	})
}

type 发包载体 struct {
	缓冲  []byte
	读取器 *bytes.Reader
}

var 发包载体池 = sync.Pool{
	New: func() any {
		return &发包载体{
			缓冲:  make([]byte, 0, 64),
			读取器: bytes.NewReader(nil),
		}
	},
}

func 解析二进制包(载荷 []byte) {
	if len(载荷) == 0 {
		return
	}

	var 真实数据编号 uint32 = 0
	var 数据起始游标 int = 0
	const 第7位必须是1 = 0x80
	const 第31位错误标记 = 31
	const 去除位掩码 = 0x7F
	const 延长码FF = 0xFF

	for 数据起始游标 < len(载荷) {
		当前字节 := 载荷[数据起始游标]
		数据起始游标++

		if 当前字节 < 第7位必须是1 {
			真实数据编号 |= (1 << 第31位错误标记)
			break
		}

		真实值 := uint32(当前字节 & 去除位掩码)
		真实数据编号 += 真实值

		if 当前字节 != 延长码FF {
			break
		}
	}

	纯数据载荷 := 载荷[数据起始游标:]
	实际长度 := len(纯数据载荷)

	switch 真实数据编号 {

	case 0:
		if 实际长度 < 10 {
			return
		}

		天数 := ((uint32(纯数据载荷[0]) & 0x7F) << 14) |
			((uint32(纯数据载荷[1]) & 0x7F) << 7) |
			(uint32(纯数据载荷[2]) & 0x7F)
		全局配置.游戏内状态.世界天数.Store(天数 + 1)

		全局配置.游戏内状态.在线玩家人数.Store(uint32(纯数据载荷[3]) & 0x7F)

		复合环境字节 := 纯数据载荷[4]
		季节 := uint32((复合环境字节 >> 5) & 0x03)
		月相 := uint32(复合环境字节 & 0x1F)

		全局配置.游戏内状态.当前季节.Store(季节)
		全局配置.游戏内状态.月相状态.Store(月相)

		温度高位 := int32(纯数据载荷[5])
		绝对温度 := ((温度高位 & 0x3F) << 7) | int32(纯数据载荷[6]&0x7F)

		真实温度 := 绝对温度 * (1 - ((温度高位 & 0x40) >> 5))

		全局配置.游戏内状态.绝对温度.Store(真实温度)

		复合状态字节 := uint32(纯数据载荷[7])

		昼夜阶段 := (复合状态字节 >> 5) & 0x03

		是否下雨 := (复合状态字节 >> 4) & 0x01

		是否下雪 := (复合状态字节 >> 3) & 0x01

		暴动阶段 := 复合状态字节 & 0x07

		全局配置.游戏内状态.昼夜阶段.Store(昼夜阶段)
		全局配置.游戏内状态.是否下雨.Store(是否下雨 == 1)
		全局配置.游戏内状态.是否下雪.Store(是否下雪 == 1)
		全局配置.游戏内状态.暴动状态.Store(暴动阶段)

		季节剩余 := uint32(纯数据载荷[8] & 0x7F)
		全局配置.游戏内状态.季节剩余天数.Store(季节剩余)

		天体唤醒 := (纯数据载荷[9] & 0x7F) == 1
		全局配置.游戏内状态.天体唤醒.Store(天体唤醒)
	case 1:
		if 实际长度 < 11 {
			return
		}

		巨鹿 := uint32(纯数据载荷[0] & 0x7F)
		熊大 := uint32(纯数据载荷[1] & 0x7F)
		大鹅 := uint32(纯数据载荷[2] & 0x7F)
		龙蝇 := uint32(纯数据载荷[3] & 0x7F)
		蜂后 := uint32(纯数据载荷[4] & 0x7F)
		克劳斯 := uint32(纯数据载荷[5] & 0x7F)
		邪天翁 := uint32(纯数据载荷[8] & 0x7F)
		果蝇王 := uint32(纯数据载荷[9] & 0x7F)
		蚁狮踩踏 := uint32(纯数据载荷[10] & 0x7F)

		全局配置.游戏内状态.巨鹿倒计时.Store(巨鹿)
		全局配置.游戏内状态.熊大倒计时.Store(熊大)
		全局配置.游戏内状态.大鹅倒计时.Store(大鹅)
		全局配置.游戏内状态.龙蝇倒计时.Store(龙蝇)
		全局配置.游戏内状态.蜂后倒计时.Store(蜂后)
		全局配置.游戏内状态.克劳斯倒计时.Store(克劳斯)
		全局配置.游戏内状态.邪天翁倒计时.Store(邪天翁)
		全局配置.游戏内状态.果蝇王倒计时.Store(果蝇王)
		全局配置.游戏内状态.蚁狮踩踏分钟倒计时.Store(蚁狮踩踏)
	case 2:
		if 实际长度 < 11 {
			return
		}

		蛤蟆 := uint32(纯数据载荷[6] & 0x7F)
		织影者 := uint32(纯数据载荷[7] & 0x7F)

		全局配置.游戏内状态.蛤蟆倒计时.Store(蛤蟆)
		全局配置.游戏内状态.织影者倒计时.Store(织影者)

		if !全局配置.配置区2.启用主世界.Load() {
			url := 全局配置.配置区2.洞穴状态上报端点

			go 发送洞穴参数(蛤蟆, 织影者, url)
		}
	default:
		return
	}
}

var 防超时客户端 = &http.Client{
	Timeout: 2 * time.Second,
}

func 发送洞穴参数(toad, fw uint32, targetURL string) {
	if targetURL == "" {
		return
	}

	载体 := 发包载体池.Get().(*发包载体)
	defer 发包载体池.Put(载体)
	载荷 := 载体.缓冲[:0]

	载荷 = append(载荷, "toadstool="...)
	载荷 = strconv.AppendUint(载荷, uint64(toad), 10)
	载荷 = append(载荷, "&fuelweaver="...)
	载荷 = strconv.AppendUint(载荷, uint64(fw), 10)

	载体.缓冲 = 载荷

	载体.读取器.Reset(载荷)

	req, err := http.NewRequest("POST", targetURL, 载体.读取器)
	if err != nil {
		发包载体池.Put(载体)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := 防超时客户端.Do(req)
	if err != nil {
		return
	}

	if resp != nil && resp.Body != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
