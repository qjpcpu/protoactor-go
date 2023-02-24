package etcd

import "github.com/qjpcpu/protoactor-go/log"

var (
	plog = log.New(log.DefaultLevel, "[CLU/ETCD]")
)

// SetLogLevel sets the log level for the logger
// SetLogLevel is safe to be called concurrently
func SetLogLevel(level log.Level) {
	plog.SetLevel(level)
}
