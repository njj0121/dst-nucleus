package main

import (
	"os"
	"path/filepath"
	"time"
)

func 原子写文件(目标路径 string, 内容 []byte) uint8 {
	目标目录 := filepath.Dir(目标路径)
	if err := os.MkdirAll(目标目录, 0755); err != nil {
		return 128
	}

	临时文件, err := os.CreateTemp(目标目录, "dstn_tmp_*")
	if err != nil {
		return 129
	}
	临时路径 := 临时文件.Name()

	defer os.Remove(临时路径)

	if _, err = 临时文件.Write(内容); err != nil {
		临时文件.Close()
		return 130
	}

	if err = 临时文件.Sync(); err != nil {
		临时文件.Close()
		return 131
	}

	if err = 临时文件.Close(); err != nil {
		return 132
	}
	for range 5 {
		if err = os.Rename(临时路径, 目标路径); err == nil {
			return 0
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 133
}
