package desktop

import "context"

// Adapter captures prompt text in desktop AI clients before send.
// Platform-specific implementations use macOS Accessibility APIs or
// Windows UI Automation APIs.
type Adapter interface {
	Name() string
	Start(context.Context) error
	Stop(context.Context) error
}
