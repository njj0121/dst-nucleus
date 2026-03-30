//go:build ignore

package main

import (
	"bytes"
	"os"
)

func main() {
	rawLua, err := os.ReadFile("parasite.lua")
	if err != nil {
		panic(err)
	}

	源码 := rawLua

	按行分割 := bytes.Split(源码, []byte("\n"))
	var 纯净代码块 [][]byte

	for _, 行 := range 按行分割 {
		if 索引 := bytes.Index(行, []byte("--")); 索引 != -1 {
			行 = 行[:索引]
		}

		行 = bytes.TrimSpace(行)

		if len(行) > 0 {
			纯净代码块 = append(纯净代码块, 行)
		}
	}

	压扁 := bytes.Join(纯净代码块, []byte(" "))

	动态寄生虫指令 := append(压扁, '\n')

	err = os.WriteFile("parasite_min.lua", 动态寄生虫指令, 0644)
	if err != nil {
		panic(err)
	}
}
