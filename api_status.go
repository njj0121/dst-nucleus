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

const RingBufDepth = 32
const AddrMask = RingBufDepth - 1
const SlotCap = 1024

var (
	// 为了防止windows报毒，如果不提前分配好所有内存，深度扫描会认定为无毒，但是动态分析可能会认为是木马
	ContigMemBlock [RingBufDepth * SlotCap]byte

	StateRingBuf [RingBufDepth][]byte
	RingCursor   uint32

	CurrStateSnap atomic.Pointer[[]byte]
)

func init() {
	for i := 0; i < RingBufDepth; i++ {
		StartPhysOffset := i * SlotCap
		StateRingBuf[i] = ContigMemBlock[StartPhysOffset : StartPhysOffset : StartPhysOffset+SlotCap]
	}

	ZeroState := []byte(`{"status":"loading"}`)
	CurrStateSnap.Store(&ZeroState)
}

// 18212226                64.72 ns/op            0 B/op          0 allocs/op
func FlushServerState() {
	NewCursor := atomic.AddUint32(&RingCursor, 1)
	CurrSlot := (NewCursor - 1) & AddrMask

	buf := StateRingBuf[CurrSlot][:0]

	buf = append(buf, `{"status":"success","locks":{"game_upd":`...)
	buf = strconv.AppendBool(buf, GlobalConf.AtomicGate.GameUpdatingGate.Load())

	buf = append(buf, `,"mod_upd":`...)
	buf = strconv.AppendBool(buf, GlobalConf.AtomicGate.ModUpdatingGate.Load())

	buf = append(buf, `,"master_rdy":`...)
	buf = strconv.AppendBool(buf, GlobalConf.AtomicGate.MasterReadyGate.Load())

	buf = append(buf, `,"caves_rdy":`...)
	buf = strconv.AppendBool(buf, GlobalConf.AtomicGate.CavesReadyGate.Load())

	buf = append(buf, `},"performance":{"baseline_cycles":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.CoreCpuMetrics.CPUFrequency.Load()), 10)

	buf = append(buf, `,"current_cycles":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.CoreCpuMetrics.CurrFrequency.Load()), 10)

	buf = append(buf, `},"data":{"players":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.OnlinePlayers.Load()), 10)

	buf = append(buf, `,"enable_caves":`...)
	buf = strconv.AppendBool(buf, GlobalConf.Section2.EnableCaves.Load())

	buf = append(buf, `,"enable_master":`...)
	buf = strconv.AppendBool(buf, GlobalConf.Section2.EnableMaster.Load())

	buf = append(buf, `,"cycles":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.WorldDays.Load()), 10)

	buf = append(buf, `,"season":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.CurrSeason.Load()), 10)

	buf = append(buf, `,"phase":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.DayPhase.Load()), 10)

	buf = append(buf, `,"rem_days":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.SeasonDaysLeft.Load()), 10)

	buf = append(buf, `,"temp":`...)
	buf = strconv.AppendInt(buf, int64(GlobalConf.GameState.AbsTemp.Load()), 10)

	buf = append(buf, `,"is_raining":`...)
	buf = strconv.AppendBool(buf, GlobalConf.GameState.IsRaining.Load())

	buf = append(buf, `,"is_snowing":`...)
	buf = strconv.AppendBool(buf, GlobalConf.GameState.IsSnowing.Load())

	buf = append(buf, `,"moon_phase":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.MoonState.Load()), 10)

	buf = append(buf, `,"nightmare":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.NightmareState.Load()), 10)

	buf = append(buf, `,"alter_awake":`...)
	buf = strconv.AppendBool(buf, GlobalConf.GameState.CelestialWake.Load())

	buf = append(buf, `,"boss_timers":{`...)

	buf = append(buf, `"deerclops":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.DeerclopsTimer.Load()), 10)

	buf = append(buf, `,"bearger":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.BeargerTimer.Load()), 10)

	buf = append(buf, `,"moose":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.MooseGooseTimer.Load()), 10)

	buf = append(buf, `,"dragonfly":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.DragonflyTimer.Load()), 10)

	buf = append(buf, `,"beequeen":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.BeeQueenTimer.Load()), 10)

	buf = append(buf, `,"klaus":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.KlausTimer.Load()), 10)

	buf = append(buf, `,"toadstool":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.ToadstoolTimer.Load()), 10)

	buf = append(buf, `,"fuelweaver":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.FuelweaverTimer.Load()), 10)

	buf = append(buf, `,"malbatross":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.MalbatrossTimer.Load()), 10)

	buf = append(buf, `,"lordfruitfly":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.FruitFlyLordTimer.Load()), 10)

	buf = append(buf, `,"antlion":`...)
	buf = strconv.AppendUint(buf, uint64(GlobalConf.GameState.AntlionStompMinTimer.Load()), 10)

	buf = append(buf, `}}}`...)

	StateRingBuf[CurrSlot] = buf
	TargetPtr := &StateRingBuf[CurrSlot]

	CurrStateSnap.Store(TargetPtr)

	SseObserverMatrix.Range(func(key, value any) bool {
		select {
		case key.(chan struct{}) <- struct{}{}:
		default:
		}
		return true
	})
}

type PacketCarrier struct {
	Buffer  []byte
	vReader *bytes.Reader
}

var PacketPool = sync.Pool{
	New: func() any {
		return &PacketCarrier{
			Buffer:  make([]byte, 0, 64),
			vReader: bytes.NewReader(nil),
		}
	},
}

func ParseRawPacket(Payload []byte) {
	if len(Payload) == 0 {
		return
	}

	var RealDataIdx uint32 = 0
	var DataStartCursor int = 0
	const Bit7MustBe1 = 0x80
	const Bit31ErrFlag = 31
	const StripBitmask = 0x7F
	const ExtCodeFF = 0xFF

	for DataStartCursor < len(Payload) {
		CurrByte := Payload[DataStartCursor]
		DataStartCursor++

		if CurrByte < Bit7MustBe1 {
			RealDataIdx |= (1 << Bit31ErrFlag)
			break
		}

		RealVal := uint32(CurrByte & StripBitmask)
		RealDataIdx += RealVal

		if CurrByte != ExtCodeFF {
			break
		}
	}

	PurePayload := Payload[DataStartCursor:]
	ActualLen := len(PurePayload)

	switch RealDataIdx {

	case 0:
		if ActualLen < 10 {
			return
		}

		Days := ((uint32(PurePayload[0]) & 0x7F) << 14) |
			((uint32(PurePayload[1]) & 0x7F) << 7) |
			(uint32(PurePayload[2]) & 0x7F)
		GlobalConf.GameState.WorldDays.Store(Days + 1)

		GlobalConf.GameState.OnlinePlayers.Store(uint32(PurePayload[3]) & 0x7F)

		PackedEnvByte := PurePayload[4]
		Season := uint32((PackedEnvByte >> 5) & 0x03)
		MoonPhase := uint32(PackedEnvByte & 0x1F)

		GlobalConf.GameState.CurrSeason.Store(Season)
		GlobalConf.GameState.MoonState.Store(MoonPhase)

		TempHi := int32(PurePayload[5])
		AbsTemp := ((TempHi & 0x3F) << 7) | int32(PurePayload[6]&0x7F)

		RealTemp := AbsTemp * (1 - ((TempHi & 0x40) >> 5))

		GlobalConf.GameState.AbsTemp.Store(RealTemp)

		PackedStateByte := uint32(PurePayload[7])

		DayPhase := (PackedStateByte >> 5) & 0x03

		IsRaining := (PackedStateByte >> 4) & 0x01

		IsSnowing := (PackedStateByte >> 3) & 0x01

		NightmarePhase := PackedStateByte & 0x07

		GlobalConf.GameState.DayPhase.Store(DayPhase)
		GlobalConf.GameState.IsRaining.Store(IsRaining == 1)
		GlobalConf.GameState.IsSnowing.Store(IsSnowing == 1)
		GlobalConf.GameState.NightmareState.Store(NightmarePhase)

		SeasonLeft := uint32(PurePayload[8] & 0x7F)
		GlobalConf.GameState.SeasonDaysLeft.Store(SeasonLeft)

		CelestialWake := (PurePayload[9] & 0x7F) == 1
		GlobalConf.GameState.CelestialWake.Store(CelestialWake)
	case 1:
		if ActualLen < 11 {
			return
		}

		Deerclops := uint32(PurePayload[0] & 0x7F)
		Bearger := uint32(PurePayload[1] & 0x7F)
		MooseGoose := uint32(PurePayload[2] & 0x7F)
		Dragonfly := uint32(PurePayload[3] & 0x7F)
		BeeQueen := uint32(PurePayload[4] & 0x7F)
		Klaus := uint32(PurePayload[5] & 0x7F)
		Malbatross := uint32(PurePayload[8] & 0x7F)
		FruitFlyLord := uint32(PurePayload[9] & 0x7F)
		AntlionStomp := uint32(PurePayload[10] & 0x7F)

		GlobalConf.GameState.DeerclopsTimer.Store(Deerclops)
		GlobalConf.GameState.BeargerTimer.Store(Bearger)
		GlobalConf.GameState.MooseGooseTimer.Store(MooseGoose)
		GlobalConf.GameState.DragonflyTimer.Store(Dragonfly)
		GlobalConf.GameState.BeeQueenTimer.Store(BeeQueen)
		GlobalConf.GameState.KlausTimer.Store(Klaus)
		GlobalConf.GameState.MalbatrossTimer.Store(Malbatross)
		GlobalConf.GameState.FruitFlyLordTimer.Store(FruitFlyLord)
		GlobalConf.GameState.AntlionStompMinTimer.Store(AntlionStomp)
	case 2:
		if ActualLen < 11 {
			return
		}

		Toadstool := uint32(PurePayload[6] & 0x7F)
		Fuelweaver := uint32(PurePayload[7] & 0x7F)

		GlobalConf.GameState.ToadstoolTimer.Store(Toadstool)
		GlobalConf.GameState.FuelweaverTimer.Store(Fuelweaver)

		if !GlobalConf.Section2.EnableMaster.Load() {
			url := GlobalConf.Section2.CavesStateEndpoint

			go PostCavesTelemetry(Toadstool, Fuelweaver, url)
		}
	default:
		return
	}
}

var GatedHttpClient = &http.Client{
	Timeout: 2 * time.Second,
}

func PostCavesTelemetry(toad, fw uint32, targetURL string) {
	if targetURL == "" {
		return
	}

	载体 := PacketPool.Get().(*PacketCarrier)
	defer PacketPool.Put(载体)
	Payload := 载体.Buffer[:0]

	Payload = append(Payload, "toadstool="...)
	Payload = strconv.AppendUint(Payload, uint64(toad), 10)
	Payload = append(Payload, "&fuelweaver="...)
	Payload = strconv.AppendUint(Payload, uint64(fw), 10)

	载体.Buffer = Payload

	载体.vReader.Reset(Payload)

	req, err := http.NewRequest("POST", targetURL, 载体.vReader)
	if err != nil {
		PacketPool.Put(载体)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := GatedHttpClient.Do(req)
	if err != nil {
		return
	}

	if resp != nil && resp.Body != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}
