//go:build !windows

package doctor

// These are only used on Windows.
func netstatListening() (string, error) { return "", nil }

func processInfo(int) (name, path string) { return "", "" }
