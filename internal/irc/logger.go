package irc

import "log"

// Logger is the interface used by Client for logging. Compatible with *log.Logger.
type Logger interface {
	Printf(format string, v ...interface{})
}

// defaultLogger returns a standard library logger with the "[xdcc] " prefix.
func defaultLogger() Logger {
	return log.New(log.Writer(), "[xdcc] ", log.Flags())
}
