//go:build !windows

package main

import "context"

func initTray(_ context.Context) {}
func destroyTray()               {}
