package supervisor

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"strings"
)

// forwardLines reads r line-by-line and emits each line as a slog record.
// promoteErrors=true elevates lines containing "[Error]" or "[Fatal]"
// (xray-core's own log markers) to slog.LevelError. Closes when r EOFs.
func forwardLines(r io.Reader, baseLevel slog.Level, promoteErrors bool, source string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 1<<16)
	for sc.Scan() {
		line := sc.Text()
		level := baseLevel
		if promoteErrors && (strings.Contains(line, "[Error]") || strings.Contains(line, "[Fatal]")) {
			level = slog.LevelError
		}
		slog.Default().Log(context.Background(), level, line, "source", source)
	}
}
