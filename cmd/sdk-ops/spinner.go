package main

import (
	"fmt"
	"os"
	"time"
)

var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var spinnerStop chan struct{}

func startSpinner(msg string) {
	if !isTerminal() {
		fmt.Fprintf(os.Stderr, "  %s...\n", msg)
		return
	}
	spinnerStop = make(chan struct{})
	go func() {
		i := 0
		for {
			select {
			case <-spinnerStop:
				return
			default:
				fmt.Fprintf(os.Stderr, "\r  %s %s ", spinnerChars[i%len(spinnerChars)], msg)
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

func stopSpinner(suffix string) {
	if spinnerStop != nil {
		close(spinnerStop)
		spinnerStop = nil
	}
	if suffix != "" {
		fmt.Fprintf(os.Stderr, "\r  %s✓%s %s\n", colorGreen, colorReset, suffix)
	} else {
		fmt.Fprintf(os.Stderr, "\r  \r")
	}
}

func isTerminal() bool {
	if fi, err := os.Stderr.Stat(); err == nil {
		return fi.Mode()&os.ModeCharDevice != 0
	}
	return false
}
