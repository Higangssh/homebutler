//go:build !windows

package system

import "errors"

// readWindows exists so Status compiles everywhere; the Win32 calls behind
// the real one are only linked into a Windows build.
func readWindows() (readings, error) {
	return readings{}, errors.New("this build cannot read Windows")
}
