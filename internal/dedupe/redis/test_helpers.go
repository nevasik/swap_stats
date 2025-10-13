package redis

import "gitlab.com/nevasik7/alerting/logger"

// NoopLogger is a logger that does nothing (for testing)
type NoopLogger struct{}

func (n *NoopLogger) Debug(msg string)                          {}
func (n *NoopLogger) Debugf(format string, args ...interface{}) {}
func (n *NoopLogger) Info(msg string)                           {}
func (n *NoopLogger) Infof(format string, args ...interface{})  {}
func (n *NoopLogger) Warn(msg string)                           {}
func (n *NoopLogger) Warnf(format string, args ...interface{})  {}
func (n *NoopLogger) Error(msg string)                          {}
func (n *NoopLogger) Errorf(format string, args ...interface{}) {}
func (n *NoopLogger) Fatal(msg string)                          {}
func (n *NoopLogger) Fatalf(format string, args ...interface{}) {}
func (n *NoopLogger) Panic(msg string)                          {}
func (n *NoopLogger) Panicf(format string, args ...interface{}) {}
func (n *NoopLogger) WithField(key string, value interface{}) logger.Logger {
	return n
}
func (n *NoopLogger) WithFields(fields map[string]interface{}) logger.Logger {
	return n
}

func createTestLogger() logger.Logger {
	return &NoopLogger{}
}
