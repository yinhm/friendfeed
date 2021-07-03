// Log the panic to the log file - for oses which can't do this

// +build !windows,!darwin,!dragonfly,!freebsd,!linux,!nacl,!netbsd,!openbsd

package util

import "os"

// redirectStderr to the file passed in
func RedirectStderr(f *os.File) {
	Errorf(nil, "Can't redirect stderr to file")
}
