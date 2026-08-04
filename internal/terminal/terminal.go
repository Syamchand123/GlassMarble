package terminal

import "os"

// IsTTY detects if standard output is a terminal
func IsTTY() bool {
	if fileInfo, _ := os.Stderr.Stat(); (fileInfo.Mode() & os.ModeCharDevice) != 0 {
		return true
	}
	return false
}
