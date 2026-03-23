package main

import "log/slog"

type debugAdapter struct {
	*slog.Logger
}

func (log *debugAdapter) Printf(msg string, args ...any) {
	log.Logger.Debug(msg, args...)
}
