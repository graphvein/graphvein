package storeloc

import (
	"errors"
	"path/filepath"
)

// UserDataDir is the platform's per-user data folder, the parent of
// Graphvein's stores folder (ADR 0093). It is the data folder, not the
// cache folder: a store holds run history, change baselines and pins that
// a re-index cannot rebuild, and cache folders get emptied.
//
//   - Windows: %LocalAppData%, not the roaming %AppData%, which can be
//     redirected to a network share where SQLite's WAL does not work.
//   - macOS: ~/Library/Application Support.
//   - Linux and others: $XDG_DATA_HOME when it is an absolute path, else
//     ~/.local/share (XDG Base Directory specification).
//
// goos and getenv are passed in so every platform's rule is tested on
// every platform. Go has no os.UserDataDir.
func UserDataDir(goos string, getenv func(string) string) (string, error) {
	switch goos {
	case "windows":
		if dir := getenv("LocalAppData"); dir != "" {
			return dir, nil
		}
		return "", errors.New("%LocalAppData% is not set")
	case "darwin", "ios":
		home := getenv("HOME")
		if home == "" {
			return "", errors.New("$HOME is not set")
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	case "plan9":
		home := getenv("home")
		if home == "" {
			return "", errors.New("$home is not set")
		}
		return filepath.Join(home, "lib"), nil
	}
	if dir := getenv("XDG_DATA_HOME"); dir != "" && filepath.IsAbs(dir) {
		return dir, nil
	}
	home := getenv("HOME")
	if home == "" {
		return "", errors.New("neither $XDG_DATA_HOME nor $HOME is set")
	}
	return filepath.Join(home, ".local", "share"), nil
}
