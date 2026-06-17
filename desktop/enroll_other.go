//go:build !darwin

package main

import "fmt"

func directEnrollAndStartService(token, backendURL, gatewayURL, orgName, deviceID string) error {
	return fmt.Errorf("direct desktop enrollment fallback is only implemented on macOS")
}
