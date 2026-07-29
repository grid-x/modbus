package main

import "log/slog"

type debugAdapter struct {
	*slog.Logger
}

func (log *debugAdapter) Printf(msg string, args ...any) {
	log.Debug(msg, args...)
}
