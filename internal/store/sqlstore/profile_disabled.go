//go:build !graphveinprofile

package sqlstore

func profilePhase(string) func()       { return nil }
func profileMaps(string, *transaction) {}
