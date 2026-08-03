//go:build !windows

package mount

func hideDir(_ string) {}

func HideMetaDir(_ string) {}
