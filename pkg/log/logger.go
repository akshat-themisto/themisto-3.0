// Package log defines the logging contract used by core. Implementations live in cmd or here.
package log

// Logger is the minimal interface required by core. No OS-specific methods.
type Logger interface {
	Debug(msg string, keyvals ...interface{})
	Info(msg string, keyvals ...interface{})
	Warn(msg string, keyvals ...interface{})
	Error(msg string, keyvals ...interface{})
	With(keyvals ...interface{}) Logger
}
