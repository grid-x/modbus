package main

import (
	"fmt"
	"log/slog"
)

type debugAdapter struct {
	*slog.Logger
}

func (log *debugAdapter) Printf(format string, args ...any) {
	log.Debug(fmt.Sprintf(format, args...))
}
