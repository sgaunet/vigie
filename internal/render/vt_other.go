//go:build !windows

package render

// EnableVT reports that the terminal behind fd understands ANSI escapes, which is always
// the case outside Windows.
func EnableVT(_ uintptr) bool { return true }
