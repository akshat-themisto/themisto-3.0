//go:build darwin

package main

import (
	"github.com/themisto/agent/adapter/darwin"
	"github.com/themisto/agent/core/adapter/iface"
	"github.com/themisto/agent/pkg/log"
)

func newPlatformAdapter(logger log.Logger) iface.OSAdapter {
	return darwin.New(logger)
}
