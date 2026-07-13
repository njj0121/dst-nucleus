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
	State_OK       uint8 = 0
	State_NoUpdate uint8 = 1
	State_Fail     uint8 = 2
	State_Locked   uint8 = 3
)

func AutoInstall() {
	for {
		if _, err := os.Stat(SteamCmdBinPath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			LogOutLn(S2B("[fatal] steamcmd fs corruption: "), E2B(err))
		}

		LogOutLn(S2B("[init] steamcmd not found at: "), S2B(GlobalConf.Section1.SteamCmdPath), S2B(". bootstrapping..."))

		if err := InstallSteamCmd(GlobalConf.Section1.SteamCmdPath); err != 0 {
			LogOutLn(S2B("[warn] network or extract error. retrying in 5s..."))
			time.Sleep(5 * time.Second)
			continue
		}

		LogOutLn(S2B("[init] steamcmd bootstrap complete."))
		break
	}

	for {
		if _, err := os.Stat(GameBinPath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			LogOutLn(S2B("[fatal] game binary fs corruption: "), E2B(err))
		}
		LogOutLn(S2B("[init] game binary missing ("), S2B(DSTExecName), S2B("). initiating fresh install..."))
		LogOutLn(S2B("[init] massive payload incoming. expect tcp timeout or long wait."))

		ExecGameUpdate()

		if _, err := os.Stat(GameBinPath); err == nil {
			LogOutLn(S2B("[init] game binary verification passed."))
			break
		} else {
			LogOutLn(S2B("[warn] game binary hash mismatch. retrying in 3s..."))
			time.Sleep(3 * time.Second)
		}
	}
}

var TriggerUpdateCheck = make(chan struct{})
var AutoUpdateTickerInterval time.Duration

func VersionProbe(LifeCtx context.Context) {
	Timer := time.NewTicker(AutoUpdateTickerInterval)
	defer Timer.Stop()
	if !GlobalConf.Section2.EnableAutoUpdate.Load() {
		Timer.Stop()
		Timer.C = nil
	}

	for {
		select {
		case <-LifeCtx.Done():
			return

		case <-Timer.C:
			if ProbeGameUpdate() == State_OK {
				select {
				case ActionBus <- Action_GameUpdate:
				default:
				}
				return
			}

			if ProbeModUpdate() == State_OK {
				select {
				case ActionBus <- Action_ModHotUpdate:
				default:
				}
				return
			}

		case <-TriggerUpdateCheck:
			if ProbeGameUpdate() == State_OK {
				select {
				case ActionBus <- Action_GameUpdate:
				default:
				}
				return
			}

			if ProbeModUpdate() == State_OK {
				select {
				case ActionBus <- Action_ModHotUpdate:
				default:
				}
				return
			}
			if Timer.C != nil {
				Timer.Reset(AutoUpdateTickerInterval)
			}
		}
	}
}

func ProbeGameUpdate() uint8 {
	LocalVer := ReadLocalVer()
	RemoteVer := FetchRemoteVer()

	if LocalVer == "" || RemoteVer == "" {
		LogOutLn(S2B("[error] game version probe failed: local("), S2B(LocalVer), S2B(") remote("), S2B(RemoteVer), S2B("), network or permission issue"))
		return State_Fail
	}

	if LocalVer != RemoteVer {
		LogOutLn(S2B("[info] game update available: local("), S2B(LocalVer), S2B(") -> remote("), S2B(RemoteVer), S2B(")"))
		return State_OK
	}

	return State_NoUpdate
}

var GlobalTargetModCache = make([]uint64, 0, 256)

func ProbeModUpdate() uint8 {
	if !GlobalConf.AtomicGate.ModBusyGate.CompareAndSwap(false, true) {
		LogOutLn(S2B("[core] mod update lock denied. already in progress."))
		return State_Locked
	}
	defer GlobalConf.AtomicGate.ModBusyGate.Store(false)
	ModList, err := ReadLines()
	if err != 0 {
		LogOutLn(S2B("[sys] mod.txt io error: "), S2B(GlobalConf.Section1.ModLuaTarget))
		return State_Fail
	}

	TargetMod := GlobalTargetModCache[:0]
	for _, m := range ModList {
		if m != 0 {
			TargetMod = append(TargetMod, m)
		}
	}

	if len(TargetMod) == 0 {
		return State_NoUpdate
	}

	LocalEpochMap, _ := FetchLocalModEpochs(TargetMod)
	RemoteEpochMap, err := FetchRemoteModEpochs(TargetMod)

	if err != 0 {
		LogOutLn(S2B("[sys] mod steam api query failed."))
		return State_Fail
	}

	UpdateAvailable := false
	var StackBuf [64]byte
	var IdStackBuf [24]byte

	for _, ModID := range TargetMod {
		LocalTime := LocalEpochMap[ModID]
		RemoteEpoch, Exists := RemoteEpochMap[ModID]

		if Exists && RemoteEpoch > LocalTime {
			UpdateAvailable = true

			Payload := StackBuf[:0]
			Payload = strconv.AppendInt(Payload, LocalTime, 10)
			Payload = append(Payload, '-')
			Payload = append(Payload, '>')
			Payload = strconv.AppendInt(Payload, RemoteEpoch, 10)

			ID := strconv.AppendUint(IdStackBuf[:0], ModID, 10)
			LogOutLn(S2B("[core] mod update triggered: ["), ID, S2B("] "), Payload)

		}
	}

	if UpdateAvailable {
		return State_OK
	}

	return State_NoUpdate
}

func ExecGameUpdate() uint8 {
	if !GlobalConf.AtomicGate.GameUpdatingGate.CompareAndSwap(false, true) {
		LogOutLn(S2B("[core] game update lock denied. already in progress."))
		return State_Locked
	}
	defer GlobalConf.AtomicGate.GameUpdatingGate.Store(false)

	LogOutLn(S2B("[core] dispatching steamcmd for game update..."))

	SteamCmdProc := exec.CommandContext(GlobalCtx, SteamCmdBinPath, "+login", "anonymous", "+app_update", "343050", "validate", "+quit")
	BindProcLifetime(SteamCmdProc)

	SteamCmdProc.Stdout = os.Stdout
	SteamCmdProc.Stderr = os.Stderr

	if err := SteamCmdProc.Start(); err != nil {
		LogOutLn(S2B("[fatal] steamcmd spawn failed: "), E2B(err))
		return State_Fail
	}

	SetProcExitSig(SteamCmdProc)

	if err := SteamCmdProc.Wait(); err != nil {
		LogOutLn(S2B("[fatal] steamcmd tcp stream broken: "), E2B(err))
		return State_Fail
	}

	CloneFile(GlobalConf.Section1.ModLuaBackup, GlobalConf.Section1.ModLuaTarget)

	LogOutLn(S2B("[core] game binary overwritten."))
	return State_OK
}

func ExecModUpdate() uint8 {
	if !GlobalConf.AtomicGate.ModBusyGate.CompareAndSwap(false, true) {
		LogOutLn(S2B("[core] mod update lock denied. already in progress."))
		return State_Locked
	}
	defer GlobalConf.AtomicGate.ModBusyGate.Store(false)
	GlobalConf.AtomicGate.ModUpdatingGate.Store(true)
	defer GlobalConf.AtomicGate.ModUpdatingGate.Store(false)

	ModList, err := ReadLines()
	if err != 0 {
		LogOutLn(S2B("[fatal] mod update aborted. io error: "), S2B(GlobalConf.Section1.ModLuaTarget))
		return State_Fail
	}

	var TargetMod []uint64
	for _, m := range ModList {
		if m != 0 {
			TargetMod = append(TargetMod, m)
		}
	}

	if len(TargetMod) == 0 {
		LogOutLn(S2B("[core] mod.txt empty. bypassing mod update."))
		return State_NoUpdate
	}

	FinalUpdateList := TargetMod

	LocalEpochMap, err1 := FetchLocalModEpochs(TargetMod)
	RemoteEpochMap, err2 := FetchRemoteModEpochs(TargetMod)

	if err1 == 0 && err2 == 0 {
		var DeltaMods []uint64
		for _, modID := range TargetMod {
			LocalTime := LocalEpochMap[modID]
			RemoteEpoch, Exists := RemoteEpochMap[modID]

			if Exists && RemoteEpoch > LocalTime {
				DeltaMods = append(DeltaMods, modID)
			}
		}

		if len(DeltaMods) > 0 {
			FinalUpdateList = DeltaMods
			LogOutLn(S2B("[core] diff logic matched "), strconv.AppendInt(make([]byte, 0, 8), int64(len(FinalUpdateList)), 10), S2B(" outdated mods, performing incremental update..."))
		} else {
			LogOutLn(S2B("[warn] diff logic failed. forcing full fallback update..."))
		}
	} else {
		LogOutLn(S2B("[warn] steam api rejected query. downgrading to full blind update..."))
	}

	vArgs := []string{"+login", "anonymous"}
	for _, m := range FinalUpdateList {
		vArgs = append(vArgs, "+workshop_download_item", "322330", strconv.FormatUint(m, 10))
	}
	vArgs = append(vArgs, "+quit")

	var StackBuf [8]byte
	CountCache := strconv.AppendInt(StackBuf[:0], int64(len(FinalUpdateList)), 10)
	LogOutLn(S2B("[core] injecting "), CountCache, S2B(" mod instructions to steamcmd..."))

	SteamCmdProc := exec.CommandContext(GlobalCtx, SteamCmdBinPath, vArgs...)
	BindProcLifetime(SteamCmdProc)

	SteamCmdProc.Stdout = os.Stdout
	SteamCmdProc.Stderr = os.Stderr

	if err := SteamCmdProc.Start(); err != nil {
		LogOutLn(S2B("[fatal] steamcmd spawn failed: "), E2B(err))
		return State_Fail
	}

	SetProcExitSig(SteamCmdProc)

	if err := SteamCmdProc.Wait(); err != nil {
		LogOutLn(S2B("[fatal] steamcmd process killed mid-flight: "), E2B(err))
		return State_Fail
	}

	LogOutLn(S2B("[core] mod queue processed."))
	return State_OK
}

func ReadLocalVer() string {
	const MaxRetries = 3
	target := S2B(`"TargetBuildID"`)

	for i := 0; i < MaxRetries; i++ {
		PreReadStat, err := os.Stat(GameVerAcfPath)
		if err != nil {
			return ""
		}

		f, err := os.Open(GameVerAcfPath)
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
					match = ParseVerNum(data[absIdx+len(target):])
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

		PostReadStat, err := os.Stat(GameVerAcfPath)
		if err != nil {
			return ""
		}

		if PreReadStat.ModTime().Equal(PostReadStat.ModTime()) && PreReadStat.Size() == PostReadStat.Size() {
			return match
		}

		time.Sleep(50 * time.Millisecond)
	}

	return ""
}

func ParseVerNum(data []byte) string {
	p := 0
	vLen := len(data)

	for p < vLen && (data[p] == ' ' || data[p] == '\t') {
		p++
	}

	if p < vLen && data[p] == '"' {
		p++
		start := p
		for p < vLen && data[p] >= '0' && data[p] <= '9' {
			p++
		}
		if p > start && p < vLen && data[p] == '"' {
			return B2S(data[start:p])
		}
	}
	return ""
}

func ReadSnapshot(FilePath string) ([]byte, uint8) {
	const MaxRetries = 3
	const 防内存爆炸大小 = 1024 * 1024

	for i := 0; i < MaxRetries; i++ {
		PreReadStat, err := os.Stat(FilePath)
		if err != nil {
			return nil, 128
		}

		if PreReadStat.Size() > 防内存爆炸大小 {
			LogOutLn(S2B("[warn] refusing to buffer massive payload (>1MB): "), S2B(FilePath))
			return nil, 132
		}

		RawContent, err := os.ReadFile(FilePath)
		if err != nil {
			return nil, 129
		}

		PostReadStat, err := os.Stat(FilePath)
		if err != nil {
			return nil, 130
		}

		if PreReadStat.ModTime().Equal(PostReadStat.ModTime()) && PreReadStat.Size() == PostReadStat.Size() {
			return RawContent, 0
		}

		time.Sleep(50 * time.Millisecond)
	}

	LogOutLn(S2B("[fatal] concurrent io tear detected during snapshot."))
	return nil, 131
}

func FetchRemoteVer() string {
	VerNum, err := FetchRemoteVerHTTP()
	if err == 0 && VerNum != "" {
		return VerNum
	}
	LogOutLn(S2B("[warn] http probe failed/timeout. fallback to steamcmd local query."))

	SteamCmdProc := exec.CommandContext(GlobalCtx, SteamCmdBinPath, "+login", "anonymous", "+app_info_update", "1", "+app_info_print", "343050", "+quit")
	BindProcLifetime(SteamCmdProc)

	stdoutPipe, err1 := SteamCmdProc.StdoutPipe()
	if err1 != nil {
		LogOutLn(S2B("[sys] steamcmd pipe creation failed."))
		return ""
	}

	if err := SteamCmdProc.Start(); err != nil {
		LogOutLn(S2B("[sys] steamcmd start failed."))
		return ""
	}

	SetProcExitSig(SteamCmdProc)

	vStdout, err2 := io.ReadAll(stdoutPipe)

	if err := SteamCmdProc.Wait(); err != nil || err2 != nil {
		LogOutLn(S2B("[sys] steamcmd query failed."))
		return ""
	}

	NodeAnchor := bytes.Index(vStdout, []byte(`"public"`))
	if NodeAnchor == -1 {
		return ""
	}

	TargetOffset := bytes.Index(vStdout[NodeAnchor:], []byte(`"buildid"`))
	if TargetOffset == -1 {
		return ""
	}

	vCursor := NodeAnchor + TargetOffset + len(`"buildid"`)

	for vCursor < len(vStdout) && (vStdout[vCursor] < '0' || vStdout[vCursor] > '9') {
		vCursor++
	}
	NumOrigin := vCursor

	for vCursor < len(vStdout) && vStdout[vCursor] >= '0' && vStdout[vCursor] <= '9' {
		vCursor++
	}

	if vCursor > NumOrigin {
		return B2S(vStdout[NumOrigin:vCursor])
	}

	return ""
}

func FetchRemoteVerHTTP() (string, uint8) {
	apiURL := "https://api.steamcmd.net/v1/info/343050"

	body := FireRacingRequest(apiURL, "")
	if body == nil {
		return "", 128
	}

	BranchAnchor := bytes.Index(body, []byte(`"branches"`))
	if BranchAnchor == -1 {
		LogOutLn(S2B("[sys] ast parser: missing 'branches' node"))
		return "", 130
	}

	NodeAnchor := bytes.Index(body[BranchAnchor:], []byte(`"public"`))
	if NodeAnchor == -1 {
		LogOutLn(S2B("[sys] ast parser: missing 'public' node"))
		return "", 130
	}
	AbsStart := BranchAnchor + NodeAnchor

	TargetOffset := bytes.Index(body[AbsStart:], []byte(`"buildid"`))
	if TargetOffset == -1 {
		LogOutLn(S2B("[sys] ast parser: missing 'buildid' node"))
		return "", 130
	}

	vCursor := AbsStart + TargetOffset + len(`"buildid"`)

	for vCursor < len(body) && (body[vCursor] < '0' || body[vCursor] > '9') {
		vCursor++
	}
	NumOrigin := vCursor

	for vCursor < len(body) && body[vCursor] >= '0' && body[vCursor] <= '9' {
		vCursor++
	}

	if vCursor > NumOrigin {
		return B2S(body[NumOrigin:vCursor]), 0
	}

	LogOutLn(S2B("[sys] ast parser: buildid overflow"))
	return "", 130
}

var (
	ProxyClient = &http.Client{
		Timeout: 15 * time.Second,
	}
	DirectClient = &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
)

// 由于国内网络环境极差，为了确保环境变量配置了代理，但有时代理可能失效的情况，同时发起代理和直连请求，哪个快看哪个
func FireRacingRequest(TargetURL string, RawPayload string) []byte {
	RacingPipe := make(chan []byte, 2)

	FireRequest := func(Probe *http.Client) {
		var req *http.Request
		var err error

		if len(RawPayload) > 0 {
			req, err = http.NewRequest("POST", TargetURL, strings.NewReader(RawPayload))
			if err == nil {
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
		} else {
			req, err = http.NewRequest("GET", TargetURL, nil)
		}

		if err != nil {
			RacingPipe <- nil
			return
		}

		resp, err := Probe.Do(req)
		if err != nil {
			RacingPipe <- nil
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			RacingPipe <- nil
			return
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			RacingPipe <- nil
			return
		}

		RacingPipe <- body
	}

	go FireRequest(ProxyClient)
	go FireRequest(DirectClient)

	FailCount := 0
	for {
		select {
		case Resp := <-RacingPipe:
			if Resp != nil {
				return Resp
			}
			FailCount++
			if FailCount == 2 {
				return nil
			}
		case <-time.After(5 * time.Second):
			return nil
		}
	}
}

var CharacterValidityTable = [256]byte{
	'0': 1, '1': 1, '2': 1, '3': 1, '4': 1, '5': 1, '6': 1, '7': 1, '8': 1, '9': 1,
	'A': 1, 'B': 1, 'C': 1, 'D': 1, 'E': 1, 'F': 1, 'G': 1, 'H': 1, 'I': 1, 'J': 1,
	'K': 1, 'L': 1, 'M': 1, 'N': 1, 'O': 1, 'P': 1, 'Q': 1, 'R': 1, 'S': 1, 'T': 1,
	'U': 1, 'V': 1, 'W': 1, 'X': 1, 'Y': 1, 'Z': 1,
	'a': 1, 'b': 1, 'c': 1, 'd': 1, 'e': 1, 'f': 1, 'g': 1, 'h': 1, 'i': 1, 'j': 1,
	'k': 1, 'l': 1, 'm': 1, 'n': 1, 'o': 1, 'p': 1, 'q': 1, 'r': 1, 's': 1, 't': 1,
	'u': 1, 'v': 1, 'w': 1, 'x': 1, 'y': 1, 'z': 1,
	'_': 1,
}

func IsVarChar(b byte) bool {
	//经过实际压力测试，查找表法比位图法快1倍以上
	return CharacterValidityTable[b] == 1
}

// dedicated_server_mods_setup.lua
func ParseSetup(RawContent []byte, Result *[]uint64, DedupSet map[uint64]struct{}, SetupExclusive map[uint64]struct{}) {
	vLen := len(RawContent)
	vCursor := 0

	for vCursor < vLen {
		c := RawContent[vCursor]

		if c == '-' && vCursor+1 < vLen && RawContent[vCursor+1] == '-' {
			vCursor += 2
			for vCursor < vLen && RawContent[vCursor] != '\n' {
				vCursor++
			}
			continue
		}

		if c == '"' || c == '\'' {
			Quote := c
			vCursor++
			for vCursor < vLen {
				if RawContent[vCursor] == '\\' {
					vCursor += 2
					continue
				}
				if RawContent[vCursor] == Quote {
					vCursor++
					break
				}
				vCursor++
			}
			continue
		}

		IsHit := false
		if vCursor+16 <= vLen && (string(RawContent[vCursor:vCursor+16]) == "ServerModSetup(\"" || string(RawContent[vCursor:vCursor+16]) == "ServerModSetup('") {
			vCursor += 16
			IsHit = true
		} else if vCursor+9 <= vLen && string(RawContent[vCursor:vCursor+9]) == "workshop-" {
			vCursor += 9
			IsHit = true
		}

		if IsHit {
			NumOrigin := vCursor
			for vCursor < vLen && RawContent[vCursor] >= '0' && RawContent[vCursor] <= '9' {
				vCursor++
			}
			if vCursor > NumOrigin {
				ID := 字节转Uint64(RawContent[NumOrigin:vCursor])
				if _, Exists := DedupSet[ID]; !Exists {
					DedupSet[ID] = struct{}{}
					*Result = append(*Result, ID)
				}
				SetupExclusive[ID] = struct{}{}
			}

			for vCursor < vLen && (RawContent[vCursor] == '"' || RawContent[vCursor] == '\'' || RawContent[vCursor] == ')') {
				vCursor++
			}

			continue
		}
		vCursor++
	}
}

// modoverrides.lua
func ParseModOverrides(RawContent []byte, Result *[]uint64, DedupSet map[uint64]struct{}) {
	vLen := len(RawContent)
	vCursor := 0
	Depth := 0

	var CurrID uint64
	IsIDDisabled := false

	for vCursor < vLen {
		c := RawContent[vCursor]

		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			vCursor++
			continue
		}

		if c == '-' && vCursor+1 < vLen && RawContent[vCursor+1] == '-' {
			vCursor += 2
			for vCursor < vLen && RawContent[vCursor] != '\n' {
				vCursor++
			}
			continue
		}

		if c == '"' || c == '\'' {
			Quote := c
			vCursor++
			for vCursor < vLen {
				if RawContent[vCursor] == '\\' {
					vCursor += 2
					continue
				}
				if RawContent[vCursor] == Quote {
					vCursor++
					break
				}
				vCursor++
			}
			continue
		}

		if c == '{' {
			Depth++
			vCursor++
			continue
		}

		if c == '}' {
			if Depth == 2 && CurrID != 0 {
				if !IsIDDisabled {
					if _, Exists := DedupSet[CurrID]; !Exists {
						DedupSet[CurrID] = struct{}{}
						*Result = append(*Result, CurrID)
					}
				}
				CurrID = 0
			}
			Depth--
			vCursor++
			continue
		}

		if Depth == 1 && c == '[' {
			TmpCursor := vCursor + 1
			for TmpCursor < vLen && (RawContent[TmpCursor] == ' ' || RawContent[TmpCursor] == '\t' || RawContent[TmpCursor] == '"' || RawContent[TmpCursor] == '\'') {
				TmpCursor++
			}

			if TmpCursor+9 <= vLen && string(RawContent[TmpCursor:TmpCursor+9]) == "workshop-" {
				TmpCursor += 9
				NumOrigin := TmpCursor
				for TmpCursor < vLen && RawContent[TmpCursor] >= '0' && RawContent[TmpCursor] <= '9' {
					TmpCursor++
				}
				if TmpCursor > NumOrigin {
					ProbeID := 字节转Uint64(RawContent[NumOrigin:TmpCursor])
					for TmpCursor < vLen && (RawContent[TmpCursor] == ' ' || RawContent[TmpCursor] == '\t' || RawContent[TmpCursor] == '"' || RawContent[TmpCursor] == '\'') {
						TmpCursor++
					}
					if TmpCursor < vLen && RawContent[TmpCursor] == ']' {
						CurrID = ProbeID
						IsIDDisabled = false
						vCursor = TmpCursor + 1
						continue
					}
				}
			}
		}

		if Depth == 2 && CurrID != 0 && c == 'e' {
			if vCursor+7 <= vLen && string(RawContent[vCursor:vCursor+7]) == "enabled" {
				CleanPrefix := vCursor == 0 || !IsVarChar(RawContent[vCursor-1])
				CleanSuffix := vCursor+7 == vLen || !IsVarChar(RawContent[vCursor+7])

				if CleanPrefix && CleanSuffix {
					ProbeCursor := vCursor + 7
					EqFound := false
					for ProbeCursor < vLen {
						k := RawContent[ProbeCursor]
						if k == ' ' || k == '\t' || k == '\r' || k == '\n' {
							ProbeCursor++
							continue
						}
						if k == '=' {
							EqFound = true
							ProbeCursor++
							break
						}
						break
					}

					if EqFound {
						for ProbeCursor < vLen {
							k := RawContent[ProbeCursor]
							if k == ' ' || k == '\t' || k == '\r' || k == '\n' {
								ProbeCursor++
								continue
							}
							if k == 'f' || k == 'F' {
								IsIDDisabled = true
							}
							break
						}
						vCursor = ProbeCursor
						continue
					}
				}
			}
		}

		vCursor++
	}
}

var ModMapPool = sync.Pool{
	New: func() any {
		return make(map[uint64]struct{}, 64)
	},
}

func ReleaseModMap(vPool *sync.Pool, m map[uint64]struct{}) {
	clear(m)
	vPool.Put(m)
}

func ReadLines() ([]uint64, uint8) {
	DedupSet := ModMapPool.Get().(map[uint64]struct{})
	SetupOnlyDict := ModMapPool.Get().(map[uint64]struct{})

	defer ReleaseModMap(&ModMapPool, DedupSet)
	defer ReleaseModMap(&ModMapPool, SetupOnlyDict)

	var Result []uint64

	for FileIdx, 路径 := range ModUpdateConfPaths {
		if 路径 == "" {
			continue
		}

		RawContent, err := ReadSnapshot(路径)
		if err != 0 || len(RawContent) == 0 {
			continue
		}

		if FileIdx == 0 {
			ParseSetup(RawContent, &Result, DedupSet, SetupOnlyDict)
		} else {
			ParseModOverrides(RawContent, &Result, DedupSet)
		}
	}

	var MissingConf []byte

	for _, ID := range Result {
		if _, Exists := SetupOnlyDict[ID]; !Exists {
			MissingConf = append(MissingConf, "ServerModSetup(\""...)
			MissingConf = strconv.AppendUint(MissingConf, ID, 10)
			MissingConf = append(MissingConf, "\")\n"...)
		}
	}

	if len(MissingConf) > 0 {
		f, err := os.OpenFile(ModUpdateConfPaths[0], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			f.Write(MissingConf)
			f.Sync()
			f.Close()
			LogOutLn(S2B("[sys] unhandled mod detected. hot-patching setup.lua with "), strconv.AppendInt(make([]byte, 0, 4), int64(bytes.Count(MissingConf, []byte("\n"))), 10), S2B(" payloads."))
		}
	}

	return Result, 0
}

var (
	LocalModEpochs  = make(map[uint64]int64, 128)
	RemoteModEpochs = make(map[uint64]int64, 128)
)

func FetchLocalModEpochs(TargetModList []uint64) (map[uint64]int64, uint8) {
	for k := range LocalModEpochs {
		delete(LocalModEpochs, k)
	}
	AcfDir := filepath.Join(GlobalConf.Section1.SteamCmdPath, "steamapps", "workshop", "appworkshop_322330.acf")

	RawContent, err := ReadSnapshot(AcfDir)
	if err != 0 {
		return make(map[uint64]int64), err
	}

	Scanner := bufio.NewScanner(bytes.NewReader(RawContent))

	TargetSet := make(map[uint64]bool)
	for _, id := range TargetModList {
		TargetSet[id] = true
	}

	var currentModID uint64
	SigTimePrefix := []byte(`"timeupdated"`)

	for Scanner.Scan() {
		Line := bytes.TrimSpace(Scanner.Bytes())

		if len(Line) > 2 && Line[0] == '"' && Line[len(Line)-1] == '"' {
			ProbeID := Line[1 : len(Line)-1]

			IsNumeric := true
			for i := 0; i < len(ProbeID); i++ {
				if ProbeID[i] < '0' || ProbeID[i] > '9' {
					IsNumeric = false
					break
				}
			}

			if IsNumeric {
				ProbeIDNum := 字节转Uint64(ProbeID)
				if TargetSet[ProbeIDNum] {
					currentModID = ProbeIDNum
				} else {
					currentModID = 0
				}
				continue
			}
		}

		if currentModID != 0 && bytes.HasPrefix(Line, SigTimePrefix) {
			RightQuotePos := bytes.LastIndexByte(Line, '"')
			if RightQuotePos > 0 {
				LeftQuotePos := bytes.LastIndexByte(Line[:RightQuotePos], '"')
				if LeftQuotePos > 0 {
					ts, _ := strconv.ParseInt(B2S(Line[LeftQuotePos+1:RightQuotePos]), 10, 64)
					LocalModEpochs[currentModID] = ts

					currentModID = 0
				}
			}
		}
	}
	return LocalModEpochs, 0
}

var (
	GlobalApiBuffer [65536]byte
)

func FetchRemoteModEpochs(ModList []uint64) (map[uint64]int64, uint8) {
	for k := range RemoteModEpochs {
		delete(RemoteModEpochs, k)
	}
	if len(ModList) == 0 {
		return nil, 0
	}

	const apiURL = "https://api.steampowered.com/ISteamRemoteStorage/GetPublishedFileDetails/v1/"

	vCursor := 0

	vCursor += copy(GlobalApiBuffer[vCursor:], "itemcount=")
	NumStr := strconv.AppendInt(GlobalApiBuffer[vCursor:vCursor], int64(len(ModList)), 10)
	vCursor += len(NumStr)

	for Idx, ModID := range ModList {
		GlobalApiBuffer[vCursor] = '&'
		vCursor++
		vCursor += copy(GlobalApiBuffer[vCursor:], "publishedfileids[")
		NumStr = strconv.AppendInt(GlobalApiBuffer[vCursor:vCursor], int64(Idx), 10)
		vCursor += len(NumStr)
		vCursor += copy(GlobalApiBuffer[vCursor:], "]=")
		ID := strconv.AppendUint(GlobalApiBuffer[vCursor:vCursor], ModID, 10)
		vCursor += len(ID)
	}

	ReqPayload := string(GlobalApiBuffer[:vCursor])

	body := FireRacingRequest(apiURL, ReqPayload)
	if body == nil {
		return nil, 128
	}

	Offset := 0
	PayloadLen := len(body)

	SigID := []byte(`"publishedfileid":"`)
	SigTime := []byte(`"time_updated":`)

	for Offset < PayloadLen {
		IdAnchor := bytes.Index(body[Offset:], SigID)
		if IdAnchor == -1 {
			break
		}
		Offset += IdAnchor + len(SigID)

		RightQuote := bytes.IndexByte(body[Offset:], '"')
		if RightQuote == -1 {
			break
		}
		modID := 字节转Uint64(body[Offset : Offset+RightQuote])
		Offset += RightQuote + 1

		TimeAnchor := bytes.Index(body[Offset:], SigTime)
		if TimeAnchor == -1 {
			break
		}
		Offset += TimeAnchor + len(SigTime)

		NumStart := Offset
		for NumStart < PayloadLen && body[NumStart] == ' ' {
			NumStart++
		}
		NumEnd := NumStart
		for NumEnd < PayloadLen && body[NumEnd] >= '0' && body[NumEnd] <= '9' {
			NumEnd++
		}

		TimeStr := string(body[NumStart:NumEnd])
		ts, _ := strconv.ParseInt(TimeStr, 10, 64)
		RemoteModEpochs[modID] = ts

		Offset = NumEnd
	}

	return RemoteModEpochs, 0
}

func CloneFile(SrcPath, TargetPath string) (int64, uint8) {
	const MaxRetries = 3

	for range MaxRetries {
		PreReadStat, err := os.Stat(SrcPath)
		if err != nil {
			return 0, 128
		}

		SrcFile, err := os.Open(SrcPath)
		if err != nil {
			return 0, 129
		}

		TargetDir := filepath.Dir(TargetPath)
		os.MkdirAll(TargetDir, 0755)

		TempFile, err := os.CreateTemp(TargetDir, "tmp_clone_*")
		if err != nil {
			SrcFile.Close()
			return 0, 130
		}
		TmpPath := TempFile.Name()

		CopiedBytes, err := io.Copy(TempFile, SrcFile)
		SrcFile.Close()

		if err != nil {
			TempFile.Close()
			os.Remove(TmpPath)
			LogOutLn(S2B("[sys] kernel copy interrupted: "), E2B(err))
			return 0, 128
		}

		TempFile.Sync()
		TempFile.Close()

		PostReadStat, err := os.Stat(SrcPath)
		if err != nil {
			os.Remove(TmpPath)
			LogOutLn(E2B(err))
			return 0, 129
		}

		if !PreReadStat.ModTime().Equal(PostReadStat.ModTime()) || PreReadStat.Size() != PostReadStat.Size() {
			os.Remove(TmpPath)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		var renameErr error
		const MaxRetries = 5
		for range MaxRetries {
			renameErr = os.Rename(TmpPath, TargetPath)
			if renameErr == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		if renameErr != nil {
			os.Remove(TmpPath)
			LogOutLn(S2B("[sys] atomic rename access denied: "), E2B(renameErr))
			return 0, 130
		}

		return CopiedBytes, 0
	}

	LogOutLn(S2B("[fatal] continuous physical concurrency tear during clone."))
	return 0, 131
}

func 字节转Uint64(b []byte) uint64 {
	var n uint64
	for _, v := range b {
		n = n*10 + uint64(v-'0')
	}
	return n
}
