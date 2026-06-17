//go:build !windows && !darwin

package main

import "fmt"

func isServiceInstalled() bool            { return false }
func queryServiceState() (string, string) { return "not_found", "not a Windows system" }
func startService() error                 { return fmt.Errorf("service management is only supported on Windows") }
func stopService() error                  { return nil }
func repairAgentService() error           { return fmt.Errorf("service management is only supported on Windows") }
