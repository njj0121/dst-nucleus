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
var 默认配置文件 []byte

const 物理上限 = 64 << 10

var 配置文件目录 = "config.yaml"

func 加载配置(目标 any, 文件路径 string) uint8 {
	信息, err := os.Stat(文件路径)
	if err != nil {
		return 128
	}

	if 信息.Size() > 物理上限 {
		return 129
	}
	文件, err := os.Open(文件路径)
	if err != nil {
		return 130
	}
	defer 文件.Close()

	配置图 := make(map[string]string)
	扫描器 := bufio.NewScanner(文件)
	for 扫描器.Scan() {
		行 := strings.TrimSpace(扫描器.Text())
		if 行 == "" || strings.HasPrefix(行, "#") {
			continue
		}
		部分 := strings.SplitN(行, ":", 2)
		if len(部分) == 2 {
			键 := strings.TrimSpace(部分[0])
			值 := strings.TrimSpace(部分[1])
			值 = strings.Trim(值, "\"'`")
			if 值 != "" {
				配置图[键] = 值
			}
		}
	}

	return 递归填充(reflect.ValueOf(目标), 配置图)
}

func 递归填充(对象 reflect.Value, 数据 map[string]string) uint8 {
	v := 对象
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		字段 := v.Field(i)
		字段类型 := t.Field(i)

		if 字段类型.Name == "_" {
			continue
		}

		if 字段.Kind() == reflect.Struct {
			if !strings.HasPrefix(字段.Type().PkgPath(), "sync/atomic") {
				递归填充(字段.Addr(), 数据)
				continue
			}
		}

		标签 := 字段类型.Tag.Get("yaml")
		if 标签 == "" {
			//标签 = 字段类型.Name
			continue
		}

		值, 存在 := 数据[标签]
		if !存在 {
			continue
		}

		指针 := reflect.NewAt(字段.Type(), unsafe.Pointer(字段.UnsafeAddr())).Interface()
		switch 目标 := 指针.(type) {
		case *atomic.Bool:
			目标.Store(len(值) > 0 && (值[0]|32 == 't'))
		case *atomic.Uint32:
			if n, err := strconv.ParseUint(值, 10, 32); err == nil {
				目标.Store(uint32(n))
			}
		case *atomic.Uint64:
			if n, err := strconv.ParseUint(值, 10, 64); err == nil {
				目标.Store(n)
			}
		case *atomic.Int64:
			if n, err := strconv.ParseInt(值, 10, 64); err == nil {
				目标.Store(n)
			}
		case *string:
			*目标 = 值
		case *bool:
			*目标 = len(值) > 0 && (值[0]|32 == 't')
		case *atomic.Value:
			目标.Store(值)
		}
	}
	return 0
}

func 校验配置是否为原始默认版本(当前文件, 默认模板 []byte) bool {
	i, j := 0, 0
	总长文件 := len(当前文件)
	总长模板 := len(默认模板)

	for i < 总长文件 || j < 总长模板 {
		for i < 总长文件 && (当前文件[i] == '\r' || 当前文件[i] == '\n') {
			i++
		}
		for j < 总长模板 && (默认模板[j] == '\r' || 默认模板[j] == '\n') {
			j++
		}

		if i == 总长文件 && j == 总长模板 {
			return true
		}

		if i == 总长文件 || j == 总长模板 {
			return false
		}

		if 当前文件[i] != 默认模板[j] {
			return false
		}

		i++
		j++
	}
	return true
}
