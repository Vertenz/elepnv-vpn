// codec_test_helpers_test.go — convenience wrappers used only in tests.
//
// EncodeResponse / EncodeError / EncodeNotification combine a Marshal* call
// with a single Write. They are intentionally NOT in production code because
// production writes go through connHandle.write under its per-connection mutex;
// a bare Write here is not goroutine-safe for concurrent callers.
package ipc_test

import (
	"encoding/json"
	"io"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/ipc"
)

func EncodeResponse(w io.Writer, id json.RawMessage, result any) error {
	b, err := ipc.MarshalResponse(id, result)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func EncodeError(w io.Writer, id json.RawMessage, de *derr.Error) error {
	b, err := ipc.MarshalError(id, de)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func EncodeNotification(w io.Writer, method string, params any) error {
	b, err := ipc.MarshalNotification(method, params)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
