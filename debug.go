package main

import (
	"log"
	"os"
	"sync"
)

var (
	debugMode   bool
	initLogOnce sync.Once
	logger      *log.Logger
)

func debugLog(format string, args ...any) {
	if !debugMode {
		return
	}
	initLogOnce.Do(func() {
		logger = log.New(os.Stdout, "[PERSEUS] ", log.LstdFlags|log.LUTC)
	})
	logger.Printf(format, args...)
}
