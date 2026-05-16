package ipc

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"elepn/daemon/internal/platform"
)

// TestRegisterConnRejectsAfterClose pins down the orphan-connection race
// fix: a connection accepted by the kernel after Close has started must be
// rejected by registerConn so its serve goroutine bails immediately instead
// of blocking forever in bufio.Scanner.
func TestRegisterConnRejectsAfterClose(t *testing.T) {
	srv := NewServer(
		filepath.Join(t.TempDir(), "ignored.sock"),
		platform.XrayInfo{},
		nil, /* store */
		nil, /* machine */
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
	)
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	h := &connHandle{} // conn=nil is fine; registerConn never touches it
	if srv.registerConn(h) {
		t.Fatal("registerConn returned true after Close — late connections would leak goroutines")
	}
}
