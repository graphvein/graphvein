//go:build !graphveinprofile

package ingest

func profileRead() func() { return nil }
