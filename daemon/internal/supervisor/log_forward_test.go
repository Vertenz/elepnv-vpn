package supervisor

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func recordLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), buf
}

func TestForwardLinesEmitsBaseLevelByDefault(t *testing.T) {
	logger, buf := recordLogger()
	slog.SetDefault(logger)
	r := strings.NewReader("hello\nworld\n")
	forwardLines(r, slog.LevelInfo, false, "xray.stdout")
	out := buf.String()
	if strings.Count(out, `"level":"INFO"`) != 2 {
		t.Fatalf("expected 2 INFO lines, got %s", out)
	}
	if strings.Contains(out, `"level":"ERROR"`) {
		t.Fatalf("unexpected ERROR promotion: %s", out)
	}
	if !strings.Contains(out, `"source":"xray.stdout"`) {
		t.Fatalf("missing source attribute: %s", out)
	}
}

func TestForwardLinesPromotesErrorLinesWhenAsked(t *testing.T) {
	logger, buf := recordLogger()
	slog.SetDefault(logger)
	r := strings.NewReader("[Info] starting\n[Error] config invalid\n[Fatal] bye\nplain\n")
	forwardLines(r, slog.LevelWarn, true, "xray.stderr")
	out := buf.String()
	if strings.Count(out, `"level":"ERROR"`) != 2 {
		t.Fatalf("expected 2 ERROR-promoted lines, got %s", out)
	}
	if strings.Count(out, `"level":"WARN"`) != 2 {
		t.Fatalf("expected 2 WARN lines (non-error), got %s", out)
	}
}

func TestForwardLinesDoesNotPromoteWhenFlagFalse(t *testing.T) {
	logger, buf := recordLogger()
	slog.SetDefault(logger)
	r := strings.NewReader("[Error] something\n")
	forwardLines(r, slog.LevelInfo, false, "xray.stdout")
	out := buf.String()
	if strings.Contains(out, `"level":"ERROR"`) {
		t.Fatalf("promotion happened despite flag=false: %s", out)
	}
}
