//go:build !windows

package main

func checkCursorRegistry() (bool, string) { return false, "" }

func checkCursorCommonPaths() (bool, string) { return false, "" }
