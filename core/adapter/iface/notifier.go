package iface

import "context"

// UserNotifier sends user-visible local desktop notifications.
// Implementations should be best-effort and must not return errors for transient UI failures.
type UserNotifier interface {
	Notify(ctx context.Context, title, message string) error
}
