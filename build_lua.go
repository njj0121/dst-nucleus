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

	SrcCode := rawLua

	SplitByLine := bytes.Split(SrcCode, []byte("\n"))
	var CleanCodeChunk [][]byte

	for _, Line := range SplitByLine {
		if Idx := bytes.Index(Line, []byte("--")); Idx != -1 {
			Line = Line[:Idx]
		}

		Line = bytes.TrimSpace(Line)

		if len(Line) > 0 {
			CleanCodeChunk = append(CleanCodeChunk, Line)
		}
	}

	Flatten := bytes.Join(CleanCodeChunk, []byte(" "))

	DynamicParasiteCmd := append(Flatten, '\n')

	err = os.WriteFile("parasite_min.lua", DynamicParasiteCmd, 0644)
	if err != nil {
		panic(err)
	}
}
