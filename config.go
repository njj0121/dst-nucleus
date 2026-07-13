package main

import (
	"bufio"
	_ "embed"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"unsafe"
)

//go:embed default.yaml
var DefConfFile []byte

const HardLimit = 64 << 10

var ConfDir = "config.yaml"

func LoadConfig(Target any, FilePath string) uint8 {
	Info, err := os.Stat(FilePath)
	if err != nil {
		return 128
	}

	if Info.Size() > HardLimit {
		return 129
	}
	vFile, err := os.Open(FilePath)
	if err != nil {
		return 130
	}
	defer vFile.Close()

	ConfigMap := make(map[string]string)
	Scanner := bufio.NewScanner(vFile)
	for Scanner.Scan() {
		Line := strings.TrimSpace(Scanner.Text())
		if Line == "" || strings.HasPrefix(Line, "#") {
			continue
		}
		Chunk := strings.SplitN(Line, ":", 2)
		if len(Chunk) == 2 {
			Key := strings.TrimSpace(Chunk[0])
			Val := strings.TrimSpace(Chunk[1])
			Val = strings.Trim(Val, "\"'`")
			if Val != "" {
				ConfigMap[Key] = Val
			}
		}
	}

	return RecurseFill(reflect.ValueOf(Target), ConfigMap)
}

func RecurseFill(Entity reflect.Value, vData map[string]string) uint8 {
	v := Entity
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		vField := v.Field(i)
		FieldType := t.Field(i)

		if FieldType.Name == "_" {
			continue
		}

		if vField.Kind() == reflect.Struct {
			if !strings.HasPrefix(vField.Type().PkgPath(), "sync/atomic") {
				RecurseFill(vField.Addr(), vData)
				continue
			}
		}

		vTag := FieldType.Tag.Get("yaml")
		if vTag == "" {
			//标签 = 字段类型.Name
			continue
		}

		Val, Exists := vData[vTag]
		if !Exists {
			continue
		}

		vPtr := reflect.NewAt(vField.Type(), unsafe.Pointer(vField.UnsafeAddr())).Interface()
		switch Target := vPtr.(type) {
		case *atomic.Bool:
			Target.Store(len(Val) > 0 && (Val[0]|32 == 't'))
		case *atomic.Uint32:
			if n, err := strconv.ParseUint(Val, 10, 32); err == nil {
				Target.Store(uint32(n))
			}
		case *atomic.Uint64:
			if n, err := strconv.ParseUint(Val, 10, 64); err == nil {
				Target.Store(n)
			}
		case *atomic.Int64:
			if n, err := strconv.ParseInt(Val, 10, 64); err == nil {
				Target.Store(n)
			}
		case *string:
			*Target = Val
		case *bool:
			*Target = len(Val) > 0 && (Val[0]|32 == 't')
		case *atomic.Value:
			Target.Store(Val)
		}
	}
	return 0
}

func IsFactoryDefault(CurrFile, DefTpl []byte) bool {
	i, j := 0, 0
	TotalLenFile := len(CurrFile)
	TotalLenTpl := len(DefTpl)

	for i < TotalLenFile || j < TotalLenTpl {
		for i < TotalLenFile && (CurrFile[i] == '\r' || CurrFile[i] == '\n') {
			i++
		}
		for j < TotalLenTpl && (DefTpl[j] == '\r' || DefTpl[j] == '\n') {
			j++
		}

		if i == TotalLenFile && j == TotalLenTpl {
			return true
		}

		if i == TotalLenFile || j == TotalLenTpl {
			return false
		}

		if CurrFile[i] != DefTpl[j] {
			return false
		}

		i++
		j++
	}
	return true
}
