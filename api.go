package main

import (
	"bytes"
	"context"
	_ "embed"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type StateLock struct {
	_         [64]byte
	LockState atomic.Uint32
	_         [64]byte
}

var ApiWakeChan = make(chan struct{})

var FileIOGate StateLock

var RxPool = sync.Pool{
	New: func() any {
		b := make([]byte, 10*1024)
		return &b
	},
}

var JsonHead = []string{"application/json; charset=utf-8"}
var PlainHead = []string{"text/plain; charset=utf-8"}
var HtmlHead = []string{"text/html; charset=utf-8"}

type SonicGateway struct{}

func (SonicGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/events", "/api/epoch/master", "/api/epoch/caves", "/api/log/master", "/api/log/caves":
	default:
		Controller := http.NewResponseController(w)
		vTimeout := time.Now().Add(10 * time.Second)

		Controller.SetReadDeadline(vTimeout)
		Controller.SetWriteDeadline(vTimeout)
	}

	switch r.URL.Path {
	case "/":
		api_ui(w, r)
	case "/api/status":
		api_status(w, r)
	case "/api/events":
		api_events(w, r)
	case "/api/epoch/master":
		api_epoch_master(w, r)
	case "/api/epoch/caves":
		api_epoch_caves(w, r)
	case "/api/command":
		api_command(w, r)
	case "/api/start":
		api_start(w, r)
	case "/api/stop":
		api_stop(w, r)
	case "/api/restart":
		api_restart(w, r)
	case "/api/file/read":
		api_file_read(w, r)
	case "/api/file/write":
		api_file_write(w, r)
	case "/api/update/state":
		api_update_state(w, r)
	case "/api/log/master":
		api_log_master(w, r)
	case "/api/log/caves":
		api_log_caves(w, r)
	case "/api/checkupdate":
		api_checkupdate(w, r)
	default:
		w.WriteHeader(404)
		w.Write(api_ui404)
	}
}

func BootLocalApi() {
	ApiGuardCtx, CancelProbe := context.WithCancel(context.Background())
	defer CancelProbe()
	go BootSysProbe(ApiGuardCtx)
	if GlobalConf.Section2.EnableMaster.Load() != GlobalConf.Section2.EnableCaves.Load() {
		go GlobalEpochPulse(ApiGuardCtx)
	}

	go MasterLogBroadcastHub(ApiGuardCtx)
	go CavesLogBroadcastHub(ApiGuardCtx)

	BindAddr := GlobalConf.Section1.HttpBind

	if strings.HasPrefix(BindAddr, "/") || strings.HasPrefix(BindAddr, "./") || strings.HasSuffix(BindAddr, ".sock") {
		LogOutLn(S2B("[api] gateway listening on unix: "), S2B(BindAddr))
	} else {
		ViewAddr := BindAddr
		if strings.HasPrefix(ViewAddr, ":") {
			ViewAddr = "127.0.0.1" + ViewAddr
		} else if strings.HasPrefix(ViewAddr, "0.0.0.0:") {
			ViewAddr = strings.Replace(ViewAddr, "0.0.0.0:", "127.0.0.1:", 1)
		}

		LogOutLn(S2B("[api] gateway listening on: http://"), S2B(ViewAddr))
	}

	var FlatGateway SonicGateway

	if err := RawListen(GlobalConf.Section1.HttpBind, FlatGateway); err != nil {
		LogOutLn(S2B("[fatal] gateway crashed: "), S2B(GlobalConf.Section1.HttpBind))
	}
}

//go:embed ui.html
var PanelHTML []byte

var api_ui404 = []byte("404 page not found\n")

func api_ui(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		w.WriteHeader(404)
		w.Write(api_ui404)
		return
	}

	w.Header()["Content-Type"] = HtmlHead
	w.Write(PanelHTML)
}

var sse1 = []byte("data: ")
var sse2 = []byte("\n\n")
var post405 = []byte(`{"status":"error", "message":"method not allowed (POST required)"}`)
var get400 = []byte(`{"status":"error", "message":"method not allowed (GET required)"}`)
var success200 = []byte(`{"status": "success"}`)
var error4xx = []byte(`{"status":"error"}`)

var api_status0 = []byte(`{"status":"loading", "message":"probe warming up"}`)

// 80697765                13.62 ns/op            0 B/op          0 allocs/op
func api_status(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		HttpErr(w, 400, get400)
		return
	}

	PacketPtr := CurrStateSnap.Load()
	if PacketPtr == nil {
		w.Write(api_status0)
		return
	}

	w.Header()["Content-Type"] = JsonHead
	w.Write(*PacketPtr)
}

var SseHead = []string{"text/event-stream; charset=utf-8"}
var NoCacheHead = []string{"no-cache"}
var KeepAliveHead = []string{"keep-alive"}
var CorsHead = []string{"*"}

var SseObserverMatrix sync.Map

var api_events500 = []byte(`{"status":"error", "message":"flusher not supported"}`)

func api_events(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = SseHead
	w.Header()["Cache-Control"] = NoCacheHead
	w.Header()["Connection"] = KeepAliveHead
	w.Header()["Access-Control-Allow-Origin"] = CorsHead

	flusher, CastOk := w.(http.Flusher)
	if !CastOk {
		HttpErr(w, 500, api_events500)
		return
	}

	ClientTxPipe := make(chan struct{}, 1)
	SseObserverMatrix.Store(ClientTxPipe, struct{}{})

	defer SseObserverMatrix.Delete(ClientTxPipe)

	ForceDropClient := r.Context().Done()

	for {
		select {
		case <-ForceDropClient:
			return
		case <-ClientTxPipe:
			GlobalPacketPtr := CurrStateSnap.Load()
			if GlobalPacketPtr == nil {
				continue
			}
			w.Write(sse1)
			w.Write(*GlobalPacketPtr)
			w.Write(sse2)
			flusher.Flush()
		}
	}
}

func api_epoch_master(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = SseHead
	w.Header()["Cache-Control"] = NoCacheHead
	w.Header()["Connection"] = KeepAliveHead
	w.Header()["Access-Control-Allow-Origin"] = CorsHead

	vFlusher, CastOk := w.(http.Flusher)
	if !CastOk {
		return
	}

	EventBus := make(chan int64, 1)
	MasterEpochWatcher.Store(EventBus, struct{}{})
	defer MasterEpochWatcher.Delete(EventBus)

	ForceDropClient := r.Context().Done()

	for {
		select {
		case <-ForceDropClient:
			return
		case Epoch := <-EventBus:
			w.Write(sse1)
			w.Write(strconv.AppendInt(nil, Epoch, 10))
			w.Write(sse2)
			vFlusher.Flush()
		}
	}
}

func api_epoch_caves(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = SseHead
	w.Header()["Cache-Control"] = NoCacheHead
	w.Header()["Connection"] = KeepAliveHead
	w.Header()["Access-Control-Allow-Origin"] = CorsHead

	vFlusher, CastOk := w.(http.Flusher)
	if !CastOk {
		return
	}

	EventBus := make(chan int64, 1)
	CavesEpochWatcher.Store(EventBus, struct{}{})
	defer CavesEpochWatcher.Delete(EventBus)

	ForceDropClient := r.Context().Done()

	for {
		select {
		case <-ForceDropClient:
			return
		case Epoch := <-EventBus:
			w.Write(sse1)
			w.Write(strconv.AppendInt(nil, Epoch, 10))
			w.Write(sse2)
			vFlusher.Flush()
		}
	}
}

var ApiCmdGate StateLock

var api_command413 = []byte(`{"status":"error", "message":"payload too large"}`)
var api_command400_1 = []byte(`{"status":"error", "message":"bad request or payload too large"}`)
var api_command400_2 = []byte(`{"status":"error", "message":"empty payload"}`)
var api_command503_1 = []byte(`{"status":"warning", "message":"master ack, caves dropped (congestion)"}`)
var api_command503_2 = []byte(`{"status":"caves ack, master dropped (congestion)"}`)
var api_command400_3 = []byte(`{"status":"error", "message":"invalid target"}`)
var api_command503_3 = []byte(`{"status":"error", "message":"pipeline congested, payload dropped"}`)

func api_command(w http.ResponseWriter, r *http.Request) {
	if !ApiCmdGate.LockState.CompareAndSwap(0, 1) {
		HttpErr(w, 423, error4xx)
		return
	}
	defer ApiCmdGate.LockState.Store(0)

	if r.Method != "POST" {
		HttpErr(w, 405, post405)
		return
	}

	CmdTarget := r.URL.Query().Get("target")
	if CmdTarget != "" {
		CmdTarget = strings.ToLower(CmdTarget)
	}

	PooledPtr := RxPool.Get().(*[]byte)
	TmpBuffer := *PooledPtr

	defer func() {
		clear(TmpBuffer)
		RxPool.Put(PooledPtr)
	}()

	TotalRead := 0
	for {
		if TotalRead == len(TmpBuffer) {
			n, err := r.Body.Read(TmpBuffer[:1])

			if n > 0 || (err != nil && err != io.EOF) {
				HttpErr(w, 413, api_command413)
				return
			}

			break
		}

		n, err := r.Body.Read(TmpBuffer[TotalRead:])
		TotalRead += n

		if err != nil {
			if err == io.EOF {
				break
			}
			HttpErr(w, 400, api_command400_1)
			return
		}
	}

	if TotalRead == 0 {
		RxPool.Put(PooledPtr)
		HttpErr(w, 400, api_command400_2)
		return
	}

	MissingNL := TmpBuffer[TotalRead-1] != '\n'
	FinalLen := TotalRead
	if MissingNL {
		FinalLen++
	}

	FinalCmd := make([]byte, FinalLen)
	copy(FinalCmd, TmpBuffer[:TotalRead])
	if MissingNL {
		FinalCmd[FinalLen-1] = '\n'
	}

	clear(TmpBuffer[:TotalRead])
	RxPool.Put(PooledPtr)

	DeliverOk := false
	switch CmdTarget {
	case "caves":
		select {
		case CavesCmdPipe <- FinalCmd:
			DeliverOk = true
		default:
		}
	case "all":
		MasterOk := false
		CavesOk := false

		select {
		case MasterCmdPipe <- FinalCmd:
			MasterOk = true
		default:
		}

		select {
		case CavesCmdPipe <- FinalCmd:
			CavesOk = true
		default:
		}

		if MasterOk && CavesOk {
			DeliverOk = true
		} else if MasterOk && !CavesOk {
			HttpErr(w, 503, api_command503_1)
			return
		} else if !MasterOk && CavesOk {
			HttpErr(w, 503, api_command503_2)
			return
		} else {
			DeliverOk = false
		}
	case "master", "":
		select {
		case MasterCmdPipe <- FinalCmd:
			DeliverOk = true
		default:
		}
	default:
		HttpErr(w, 400, api_command400_3)
		return
	}

	if !DeliverOk {
		HttpErr(w, 503, api_command503_3)
		return
	}

	w.Header()["Content-Type"] = JsonHead
	w.Write(success200)
}

var api_start409 = []byte(`{"status":"error", "message":"start blocked"}`)

func api_start(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		HttpErr(w, 405, post405)
		return
	}
	select {
	case ApiWakeChan <- struct{}{}:
		GlobalConf.AtomicGate.ServerRunGate.Store(true)
	default:
		HttpErr(w, 409, api_start409)
		return
	}
	w.Header()["Content-Type"] = JsonHead
	w.Write(success200)
}

func api_stop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		HttpErr(w, 405, post405)
		return
	}
	GlobalConf.AtomicGate.ServerRunGate.Store(false)

	select {
	case ActionBus <- Action_ApiHalt:
	default:
	}

	StaleArtifact := EpochKillSwitch.Load()
	if StaleArtifact != nil && EpochKillSwitch.CompareAndSwap(StaleArtifact, nil) {
		(*StaleArtifact)()
	}

	w.Header()["Content-Type"] = JsonHead
	w.Write(success200)
}

func api_restart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		HttpErr(w, 405, post405)
		return
	}
	GlobalConf.AtomicGate.ServerRunGate.Store(true)

	select {
	case ActionBus <- Action_ApiHalt:
	default:
	}

	StaleArtifact := EpochKillSwitch.Load()
	if StaleArtifact != nil && EpochKillSwitch.CompareAndSwap(StaleArtifact, nil) {
		(*StaleArtifact)()
	}

	select {
	case ApiWakeChan <- struct{}{}:
	default:
	}

	w.Header()["Content-Type"] = JsonHead
	w.Write(success200)
}

var api_file_readHeaderByte [32]byte
var api_file_readHeader = make([]string, 1)
var targetHeader = []byte("target=")
var api_file_read400 = []byte(`{"status":"error", "message":"invalid target parameter"}`)
var api_file_read413 = []byte(`{"status":"error", "message":"file > 1MB, stream rejected"}`)
var api_file_readNone []byte

func api_file_read(w http.ResponseWriter, r *http.Request) {
	if !FileIOGate.LockState.CompareAndSwap(0, 1) {
		HttpErr(w, 423, error4xx)
		return
	}
	defer FileIOGate.LockState.Store(0)

	if r.Method != "GET" {
		HttpErr(w, 400, get400)
		return
	}

	queryBytes := S2B(r.URL.RawQuery)

	var Target []byte
	Remaining := queryBytes

	for len(Remaining) > 0 {
		var 当前块 []byte
		ChunkEnd := bytes.IndexByte(Remaining, '&')
		if ChunkEnd == -1 {
			当前块 = Remaining
			Remaining = nil
		} else {
			当前块 = Remaining[:ChunkEnd]
			Remaining = Remaining[ChunkEnd+1:]
		}

		if bytes.HasPrefix(当前块, targetHeader) {
			Target = 当前块[len(targetHeader):]
			break
		}
	}

	if len(Target) == 0 || len(Target) > 32 {
		HttpErr(w, 400, api_file_read400)
		return
	}

	var StackBuf [32]byte
	vLen := copy(StackBuf[:], Target)

	for i := 0; i < vLen; i++ {
		if StackBuf[i] >= 'A' && StackBuf[i] <= 'Z' {
			StackBuf[i] += 'a' - 'A'
		}
	}

	var FilePath string

	switch string(StackBuf[:vLen]) {
	case "cluster":
		FilePath = ClusterPath
	case "whitelist":
		FilePath = WhitelistPath
	case "blacklist":
		FilePath = BlacklistPath
	case "adminlist":
		FilePath = AdminListPath
	case "master_server":
		FilePath = MasterServerConfPath
	case "caves_server":
		FilePath = CavesServerConfPath
	case "master_world":
		FilePath = MasterWorldConfPath
	case "caves_world":
		FilePath = CavesWorldConfPath
	case "master_mod":
		FilePath = MasterModConfPath
	case "caves_mod":
		FilePath = CavesModConfPath
	case "setup":
		FilePath = GlobalConf.Section1.ModLuaTarget
	default:
		HttpErr(w, 400, api_file_read400)
		return
	}

	f, err := os.Open(FilePath)
	if err != nil {
		w.Header()["Content-Type"] = PlainHead
		w.Write(api_file_readNone)
		return
	}
	defer f.Close()
	w.Header()["Content-Type"] = PlainHead
	if stat, err := f.Stat(); err == nil {
		if stat.Size() > 1024*1024 {
			HttpErr(w, 413, api_file_read413)
			return
		}

		api_file_readHeader[0] = B2S(strconv.AppendInt(api_file_readHeaderByte[:0], stat.Size(), 10))
		w.Header()["Content-Length"] = api_file_readHeader
	}
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)
}

var api_file_write400_1 = []byte(`{"status":"error", "message":"empty payload rejected"}`)
var api_file_write400_2 = []byte(`{"status":"error", "message":"invalid target"}`)
var api_file_write400_3 = []byte(`{"status":"error", "message":"unconfigured target"}`)
var api_file_write500_1 = []byte(`{"status":"error", "message":"primary write failed"}`)
var api_file_write500_2 = []byte(`{"status":"error", "message":"clone to secondary failed"}`)
var api_file_write200 = []byte(`{"status":"success", "message":"zero-copy stream write complete"}`)

func api_file_write(w http.ResponseWriter, r *http.Request) {
	if !FileIOGate.LockState.CompareAndSwap(0, 1) {
		HttpErr(w, 423, error4xx)
		return
	}
	defer FileIOGate.LockState.Store(0)

	if r.Method != "POST" {
		HttpErr(w, 405, post405)
		return
	}

	if r.ContentLength == 0 {
		HttpErr(w, 400, api_file_write400_1)
		return
	}

	if r.ContentLength > 1024*1024 || r.ContentLength < 0 {
		HttpErr(w, 413, api_file_read413)
		return
	}

	queryBytes := S2B(r.URL.RawQuery)

	var Target []byte
	Remaining := queryBytes

	for len(Remaining) > 0 {
		var 当前块 []byte
		ChunkEnd := bytes.IndexByte(Remaining, '&')
		if ChunkEnd == -1 {
			当前块 = Remaining
			Remaining = nil
		} else {
			当前块 = Remaining[:ChunkEnd]
			Remaining = Remaining[ChunkEnd+1:]
		}

		if bytes.HasPrefix(当前块, targetHeader) {
			Target = 当前块[len(targetHeader):]
			break
		}
	}

	if len(Target) == 0 || len(Target) > 32 {
		HttpErr(w, 400, api_file_read400)
		return
	}

	var StackBuf [32]byte
	vLen := copy(StackBuf[:], Target)

	for i := 0; i < vLen; i++ {
		if StackBuf[i] >= 'A' && StackBuf[i] <= 'Z' {
			StackBuf[i] += 'a' - 'A'
		}
	}
	var TargetWritePath [2]string

	switch string(StackBuf[:vLen]) {
	case "cluster":
		TargetWritePath[0] = ClusterPath
	case "whitelist":
		TargetWritePath[0] = WhitelistPath
	case "blacklist":
		TargetWritePath[0] = BlacklistPath
	case "adminlist":
		TargetWritePath[0] = AdminListPath
	case "master_server":
		TargetWritePath[0] = MasterServerConfPath
	case "caves_server":
		TargetWritePath[0] = CavesServerConfPath
	case "master_world":
		TargetWritePath[0] = MasterWorldConfPath
	case "caves_world":
		TargetWritePath[0] = CavesWorldConfPath
	case "mod":
		TargetWritePath = WriteModConfPaths
	case "setup":
		TargetWritePath[0] = GlobalConf.Section1.ModLuaTarget

	default:
		HttpErr(w, 400, api_file_write400_2)
		return
	}

	if TargetWritePath[0] == "" {
		HttpErr(w, 400, api_file_write400_3)
		return
	}

	if err := AtomicWriteStream(TargetWritePath[0], r.Body, 1024*1024); err != 0 {
		LogOutLn(S2B("[sys] write failed "), S2B(TargetWritePath[0]))
		HttpErr(w, 500, api_file_write500_1)
		return
	}

	if TargetWritePath[1] != "" {
		if _, err := CloneFile(TargetWritePath[0], TargetWritePath[1]); err != 0 {
			LogOutLn(S2B("[sys] clone failed "), S2B(TargetWritePath[1]))
			HttpErr(w, 500, api_file_write500_2)
			return
		}
	}

	w.Header()["Content-Type"] = JsonHead
	w.Write(api_file_write200)
}

var api_update_state413 = []byte(`{"status":"error", "message":"Payload Too Large"}`)

func api_update_state(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		HttpErr(w, 405, post405)
		return
	}

	PooledPtr := RxPool.Get().(*[]byte)
	TmpBuffer := *PooledPtr

	defer func() {
		clear(TmpBuffer)
		RxPool.Put(PooledPtr)
	}()

	TotalRead := 0
	for {
		if TotalRead == len(TmpBuffer) {
			n, err := r.Body.Read(TmpBuffer[:1])

			if n > 0 || (err != nil && err != io.EOF) {
				HttpErr(w, 413, api_update_state413)
				return
			}

			break
		}

		n, err := r.Body.Read(TmpBuffer[TotalRead:])
		TotalRead += n
		if err != nil {
			break
		}
	}

	if TotalRead == 0 {
		HttpErr(w, 400, error4xx)
		return
	}

	RawData := TmpBuffer[:TotalRead]
	vCursor := 0
	TotalLen := TotalRead

	for vCursor < TotalLen {
		KeyStart := vCursor
		for vCursor < TotalLen && RawData[vCursor] != '=' {
			vCursor++
		}
		if vCursor >= TotalLen {
			break
		}
		key := RawData[KeyStart:vCursor]
		vCursor++

		ValStart := vCursor
		for vCursor < TotalLen && RawData[vCursor] != '&' && RawData[vCursor] != ';' && RawData[vCursor] != '\n' {
			vCursor++
		}
		val := RawData[ValStart:vCursor]
		vCursor++

		if len(key) == 0 || len(val) == 0 {
			continue
		}

		switch string(key) {
		case "players":
			GlobalConf.GameState.OnlinePlayers.Store(ParseApiUint(val))
		case "cycles":
			GlobalConf.GameState.WorldDays.Store(ParseApiUint(val))
		case "season":
			GlobalConf.GameState.CurrSeason.Store(ParseApiUint(val))
		case "phase":
			GlobalConf.GameState.DayPhase.Store(ParseApiUint(val))
		case "rem_days":
			GlobalConf.GameState.SeasonDaysLeft.Store(ParseApiUint(val))
		case "temp":
			GlobalConf.GameState.AbsTemp.Store(ParseApiInt(val))

		// bool
		case "is_raining":
			GlobalConf.GameState.IsRaining.Store(len(val) > 0 && (val[0] == '1' || val[0] == 't' || val[0] == 'T'))
		case "is_snowing":
			GlobalConf.GameState.IsSnowing.Store(len(val) > 0 && (val[0] == '1' || val[0] == 't' || val[0] == 'T'))
		case "alter_awake":
			GlobalConf.GameState.CelestialWake.Store(len(val) > 0 && (val[0] == '1' || val[0] == 't' || val[0] == 'T'))

		case "moon_phase":
			GlobalConf.GameState.MoonState.Store(ParseApiUint(val))
		case "nightmare":
			GlobalConf.GameState.NightmareState.Store(ParseApiUint(val))

		// boss
		case "deerclops":
			GlobalConf.GameState.DeerclopsTimer.Store(ParseApiUint(val))
		case "bearger":
			GlobalConf.GameState.BeargerTimer.Store(ParseApiUint(val))
		case "moose":
			GlobalConf.GameState.MooseGooseTimer.Store(ParseApiUint(val))
		case "dragonfly":
			GlobalConf.GameState.DragonflyTimer.Store(ParseApiUint(val))
		case "beequeen":
			GlobalConf.GameState.BeeQueenTimer.Store(ParseApiUint(val))
		case "klaus":
			GlobalConf.GameState.KlausTimer.Store(ParseApiUint(val))
		case "toadstool":
			GlobalConf.GameState.ToadstoolTimer.Store(ParseApiUint(val))
		case "fuelweaver":
			GlobalConf.GameState.FuelweaverTimer.Store(ParseApiUint(val))
		case "malbatross":
			GlobalConf.GameState.MalbatrossTimer.Store(ParseApiUint(val))
		case "lordfruitfly":
			GlobalConf.GameState.FruitFlyLordTimer.Store(ParseApiUint(val))
		case "antlion":
			GlobalConf.GameState.AntlionStompMinTimer.Store(ParseApiUint(val))
		}
	}

	w.Header()["Content-Type"] = JsonHead
	w.Write(success200)
}

type BroadcastLogChunk struct {
	_        [64]byte
	RefCount atomic.Int32
	_        [64]byte
	vData    []byte
}

var LogBroadcastPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 1024)
		return &BroadcastLogChunk{vData: b}
	},
}

var MasterLogConnCount atomic.Int32
var MasterCentralLogChan = make(chan *BroadcastLogChunk, 1024)
var MasterLogSubChan = make(chan chan *BroadcastLogChunk, 128)
var MasterLogUnsubChan = make(chan chan *BroadcastLogChunk, 128)

var CavesLogConnCount atomic.Int32
var CavesCentralLogChan = make(chan *BroadcastLogChunk, 1024)
var CavesLogSubChan = make(chan chan *BroadcastLogChunk, 128)
var CavesLogUnsubChan = make(chan chan *BroadcastLogChunk, 128)

func api_log_master(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = SseHead
	w.Header()["Cache-Control"] = NoCacheHead
	w.Header()["Connection"] = KeepAliveHead
	w.Header()["Access-Control-Allow-Origin"] = CorsHead

	vFlusher, CastOk := w.(http.Flusher)
	if !CastOk {
		return
	}

	EventBus := make(chan *BroadcastLogChunk, 256)
	MasterLogConnCount.Add(1)

	MasterLogSubChan <- EventBus

	w.WriteHeader(200)
	vFlusher.Flush()

	defer func() {
		MasterLogConnCount.Add(-1)

		MasterLogUnsubChan <- EventBus

		for vChunk := range EventBus {
			if vChunk.RefCount.Add(-1) == 0 {
				LogBroadcastPool.Put(vChunk)
			}
		}
	}()

	ForceDropClient := r.Context().Done()

	rc := http.NewResponseController(w)

	for {
		select {
		case <-ForceDropClient:
			return
		case vChunk := <-EventBus:
			rc.SetWriteDeadline(time.Now().Add(2 * time.Second))

			_, err := w.Write(vChunk.vData)

			if vChunk.RefCount.Add(-1) == 0 {
				LogBroadcastPool.Put(vChunk)
			}

			if err != nil {
				return
			}
			vFlusher.Flush()
		}
	}
}

func api_log_caves(w http.ResponseWriter, r *http.Request) {
	w.Header()["Content-Type"] = SseHead
	w.Header()["Cache-Control"] = NoCacheHead
	w.Header()["Connection"] = KeepAliveHead
	w.Header()["Access-Control-Allow-Origin"] = CorsHead

	vFlusher, CastOk := w.(http.Flusher)
	if !CastOk {
		return
	}

	EventBus := make(chan *BroadcastLogChunk, 256)
	CavesLogConnCount.Add(1)

	CavesLogSubChan <- EventBus

	w.WriteHeader(200)
	vFlusher.Flush()

	defer func() {
		CavesLogConnCount.Add(-1)

		CavesLogUnsubChan <- EventBus

		for vChunk := range EventBus {
			if vChunk.RefCount.Add(-1) == 0 {
				LogBroadcastPool.Put(vChunk)
			}
		}
	}()

	ForceDropClient := r.Context().Done()

	rc := http.NewResponseController(w)

	for {
		select {
		case <-ForceDropClient:
			return
		case vChunk := <-EventBus:
			rc.SetWriteDeadline(time.Now().Add(2 * time.Second))

			_, err := w.Write(vChunk.vData)

			if vChunk.RefCount.Add(-1) == 0 {
				LogBroadcastPool.Put(vChunk)
			}

			if err != nil {
				return
			}
			vFlusher.Flush()
		}
	}
}

var api_checkupdate409 = []byte(`{"status":"error", "message":"invalid state"}`)

func api_checkupdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		HttpErr(w, 405, post405)
		return
	}
	select {
	case TriggerUpdateCheck <- struct{}{}:
	default:
		HttpErr(w, 409, api_checkupdate409)
		return
	}
	w.Header()["Content-Type"] = JsonHead
	w.Write(success200)
}

func ParseApiUint(Payload []byte) uint32 {
	if len(Payload) == 0 {
		return 4294967295
	}

	解析结果, err := strconv.ParseUint(B2S(Payload), 10, 32)
	if err != nil {
		return 4294967295
	}

	return uint32(解析结果)
}

func ParseApiInt(Payload []byte) int32 {
	if len(Payload) == 0 {
		return 2147483647
	}

	解析结果, err := strconv.ParseInt(B2S(Payload), 10, 32)
	if err != nil {
		return 2147483647
	}

	return int32(解析结果)
}

func HttpErr(w http.ResponseWriter, vStatusCode int, RespBody []byte) {
	w.Header()["Content-Type"] = JsonHead
	w.WriteHeader(vStatusCode)
	w.Write(RespBody)
}

var AtomicWriteStreamPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

func AtomicWriteStream(TargetPath string, SrcStream io.Reader, 最大字节数 int64) uint8 {
	TargetDir := filepath.Dir(TargetPath)
	os.MkdirAll(TargetDir, 0755)

	TempFile, err := os.CreateTemp(TargetDir, "tmp_stream_*")
	if err != nil {
		LogOutLn(E2B(err))
		return 128
	}
	TmpPath := TempFile.Name()

	defer os.Remove(TmpPath)

	BufPtr := AtomicWriteStreamPool.Get().(*[]byte)
	TmpBuffer := *BufPtr

	var TotalWritten int64
	var StreamErr error

	for {
		读取量, 读错误 := SrcStream.Read(TmpBuffer)
		if 读取量 > 0 {
			TotalWritten += int64(读取量)
			if TotalWritten > 最大字节数 {
				StreamErr = io.ErrShortBuffer
				break
			}

			写入量, 写错误 := TempFile.Write(TmpBuffer[:读取量])
			if 写错误 != nil || 写入量 < 读取量 {
				StreamErr = 写错误
				break
			}
		}

		if 读错误 != nil {
			if 读错误 != io.EOF {
				StreamErr = 读错误
			}
			break
		}
	}

	AtomicWriteStreamPool.Put(BufPtr)

	if StreamErr != nil {
		TempFile.Close()
		LogOutLn(S2B("[sys] stream copy interrupted: "), E2B(StreamErr))
		return 129
	}

	err = TempFile.Sync()
	if err != nil {
		TempFile.Close()
		LogOutLn(E2B(err))
		return 130
	}

	err = TempFile.Close()
	if err != nil {
		LogOutLn(E2B(err))
		return 131
	}

	var renameErr error
	for i := 0; i < 5; i++ {
		renameErr = os.Rename(TmpPath, TargetPath)
		if renameErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if renameErr != nil {
		LogOutLn(S2B("[sys] atomic rename rejected (5 retries): "), E2B(renameErr))
		return 132
	}

	return 0
}

func BootSysProbe(LifeCtx context.Context) {
	CurrInterval := GlobalConf.ClusterMonState.SampleInterval.Load()
	if CurrInterval < 100 {
		CurrInterval = 100
	}
	Timer := time.NewTicker(time.Duration(CurrInterval) * time.Millisecond)
	defer Timer.Stop()

	EnableCpuInflationProbe := GlobalConf.CoreCpuMetrics.EnableCpuInflationProbe.Load()

	for {
		select {
		case <-LifeCtx.Done():
			return
		case <-Timer.C:
			FlushServerState()
			if EnableCpuInflationProbe {
				CalcSchedBloat()
			}
			//全服资源探针任务()
		}
	}
}

var MasterEpochWatcher sync.Map
var CavesEpochWatcher sync.Map

func GlobalEpochPulse(LifeCtx context.Context) {
	Stopwatch := time.NewTicker(2 * time.Second)
	defer Stopwatch.Stop()

	for {
		select {
		case <-LifeCtx.Done():
			return
		case <-Stopwatch.C:
			CurrMasterEpoch := GlobalConf.ProcState.MasterEpoch.Load()
			MasterEpochWatcher.Range(func(key, value any) bool {
				Chan := key.(chan int64)
				select {
				case Chan <- CurrMasterEpoch:
				default:
				}
				return true
			})

			CurrCavesEpoch := GlobalConf.ProcState.CurrCavesEpoch.Load()
			CavesEpochWatcher.Range(func(key, value any) bool {
				Chan := key.(chan int64)
				select {
				case Chan <- CurrCavesEpoch:
				default:
				}
				return true
			})
		}
	}
}

func MasterLogBroadcastHub(LifeCtx context.Context) {
	vSubscribers := make(map[chan *BroadcastLogChunk]struct{})

	for {
		select {
		case <-LifeCtx.Done():
			return

		case ch := <-MasterLogSubChan:
			vSubscribers[ch] = struct{}{}

		case ch := <-MasterLogUnsubChan:
			delete(vSubscribers, ch)
			close(ch)

		case vChunk := <-MasterCentralLogChan:
			for ch := range vSubscribers {
				vChunk.RefCount.Add(1)
				select {
				case ch <- vChunk:
				default:
					vChunk.RefCount.Add(-1)
				}
			}
			if vChunk.RefCount.Add(-1) == 0 {
				LogBroadcastPool.Put(vChunk)
			}
		}
	}
}

func CavesLogBroadcastHub(LifeCtx context.Context) {
	vSubscribers := make(map[chan *BroadcastLogChunk]struct{})

	for {
		select {
		case <-LifeCtx.Done():
			return

		case ch := <-CavesLogSubChan:
			vSubscribers[ch] = struct{}{}

		case ch := <-CavesLogUnsubChan:
			delete(vSubscribers, ch)
			close(ch)

		case vChunk := <-CavesCentralLogChan:
			for ch := range vSubscribers {
				vChunk.RefCount.Add(1)
				select {
				case ch <- vChunk:
				default:
					vChunk.RefCount.Add(-1)
				}
			}
			if vChunk.RefCount.Add(-1) == 0 {
				LogBroadcastPool.Put(vChunk)
			}
		}
	}
}

/*
Running 10s test @ http://127.0.0.1:20888/api/status
  8 threads and 1000 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     1.82ms    2.16ms  28.69ms   86.27%
    Req/Sec    94.26k    14.96k  133.60k    64.88%
  7513740 requests in 10.04s, 2.87GB read
Requests/sec: 748147.46
Transfer/sec:    292.53MB
*/
