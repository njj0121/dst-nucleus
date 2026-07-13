package main

import (
	"os"
	"path/filepath"
	"time"
)

func AtomicWriteFile(TargetPath string, RawContent []byte) uint8 {
	TargetDir := filepath.Dir(TargetPath)
	if err := os.MkdirAll(TargetDir, 0755); err != nil {
		return 128
	}

	TempFile, err := os.CreateTemp(TargetDir, "dstn_tmp_*")
	if err != nil {
		return 129
	}
	TmpPath := TempFile.Name()

	defer os.Remove(TmpPath)

	if _, err = TempFile.Write(RawContent); err != nil {
		TempFile.Close()
		return 130
	}

	if err = TempFile.Sync(); err != nil {
		TempFile.Close()
		return 131
	}

	if err = TempFile.Close(); err != nil {
		return 132
	}
	for range 5 {
		if err = os.Rename(TmpPath, TargetPath); err == nil {
			return 0
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 133
}
