//go:build windows

package main

import (
	"github.com/themisto/agent/adapter/windows"
	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/pkg/log"
)

func newPlatformAdapter(logger log.Logger) iface.OSAdapter {
	return windows.New(logger)
}
