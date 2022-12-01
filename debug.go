package main

import (
	"fmt"
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
		logger = log.New(os.Stdout, "[PERSEUS] ", log.LstdFlags|log.LUTC|log.Llongfile)
	})
	// skip 2 stack frames so that the logs report the code that called into this function
	logger.Output(2, fmt.Sprintf(format, args...))
}
