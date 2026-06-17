//go:build !windows

package main

func checkWindsurfRegistry() (bool, string) { return false, "" }

func checkWindsurfCommonPaths() (bool, string) { return false, "" }
