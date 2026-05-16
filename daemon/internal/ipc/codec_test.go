package ipc_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/ipc"
)

func TestDecodeRequestParsesValidLine(t *testing.T) {
	line := []byte(`{"jsonrpc":"2.0","id":"7","method":"Daemon.Ping","params":{}}`)
	req, err := ipc.DecodeRequest(line)
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if req.Method != "Daemon.Ping" {
		t.Fatalf("method = %q, want Daemon.Ping", req.Method)
	}
	if string(req.ID) != `"7"` {
		t.Fatalf("id = %s, want \"7\"", string(req.ID))
	}
}

func TestDecodeRequestRejectsNonJSON(t *testing.T) {
	_, err := ipc.DecodeRequest([]byte("not json"))
	if !errors.Is(err, derr.ErrParseError) {
		t.Fatalf("err = %v, want ErrParseError", err)
	}
}

func TestDecodeRequestRejectsWrongVersion(t *testing.T) {
	_, err := ipc.DecodeRequest([]byte(`{"jsonrpc":"1.0","id":1,"method":"X"}`))
	if !errors.Is(err, derr.ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestDecodeRequestRejectsEmptyMethod(t *testing.T) {
	_, err := ipc.DecodeRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":""}`))
	if !errors.Is(err, derr.ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
}

func TestEncodeResponseRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	id := json.RawMessage(`"42"`)
	type result struct {
		OK bool `json:"ok"`
	}
	if err := ipc.EncodeResponse(&buf, id, result{OK: true}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatal("response not terminated with newline")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimRight(out, "\n")), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v (%s)", err, out)
	}
	if got["id"] != "42" {
		t.Fatalf("id = %v, want 42", got["id"])
	}
	res := got["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("result.ok = %v, want true", res["ok"])
	}
}

func TestEncodeErrorIncludesSymbol(t *testing.T) {
	var buf bytes.Buffer
	if err := ipc.EncodeError(&buf, json.RawMessage(`1`), derr.ErrMethodNotFound.WithMessage("X.Y")); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &got); err != nil {
		t.Fatal(err)
	}
	errObj := got["error"].(map[string]any)
	data := errObj["data"].(map[string]any)
	if data["symbol"] != "method_not_found" {
		t.Fatalf("symbol = %v, want method_not_found", data["symbol"])
	}
}

func TestEncodeNotificationOmitsID(t *testing.T) {
	var buf bytes.Buffer
	if err := ipc.EncodeNotification(&buf, "State.Changed", map[string]string{"state": "Disconnected"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &got); err != nil {
		t.Fatal(err)
	}
	if _, hasID := got["id"]; hasID {
		t.Fatalf("notification should have no id, got: %v", got)
	}
	if got["method"] != "State.Changed" {
		t.Fatalf("method = %v, want State.Changed", got["method"])
	}
}

func TestScannerRejectsOversizeLine(t *testing.T) {
	// Build a line larger than MaxRequestBytes.
	big := bytes.Repeat([]byte("x"), ipc.MaxRequestBytes+1)
	sc := ipc.NewScanner(bytes.NewReader(big))
	if sc.Scan() {
		t.Fatal("expected Scan to return false on oversize line")
	}
	classified := ipc.ScanErr(sc.Err())
	if !errors.Is(classified, derr.ErrRequestTooLarge) {
		t.Fatalf("classified = %v, want ErrRequestTooLarge", classified)
	}
}

func TestScannerEOFOnEmpty(t *testing.T) {
	sc := ipc.NewScanner(bytes.NewReader(nil))
	if sc.Scan() {
		t.Fatal("expected Scan to return false on empty input")
	}
	if got := ipc.ScanErr(sc.Err()); !errors.Is(got, io.EOF) {
		t.Fatalf("ScanErr = %v, want io.EOF", got)
	}
}
