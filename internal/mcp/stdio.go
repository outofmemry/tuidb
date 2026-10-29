package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stdioTransport is the SDK's newline-delimited transport with a line filter in front. The SDK closes the session
// on the first line that is not a JSON-RPC message; here such a line gets a JSON-RPC error with id null and the
// session goes on. At end of input the session ends only once every request read so far has been answered.
type stdioTransport struct {
	in  io.Reader
	out io.Writer
}

func (t stdioTransport) Connect(ctx context.Context) (sdk.Connection, error) {
	pr, pw := io.Pipe()
	w := &answerWriter{w: t.out, pending: map[jsonrpc.ID]bool{}}
	w.done = sync.NewCond(&w.mu)
	go w.pump(t.in, pw)
	return (&sdk.IOTransport{Reader: pr, Writer: w}).Connect(ctx)
}

// answerWriter serializes everything written to out and tracks the requests still waiting for an answer.
type answerWriter struct {
	w       io.Writer
	mu      sync.Mutex
	done    *sync.Cond
	pending map[jsonrpc.ID]bool
}

func (a *answerWriter) Write(p []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	n, err := a.w.Write(p)
	if m, derr := jsonrpc.DecodeMessage(bytes.TrimSpace(p)); derr == nil {
		if r, ok := m.(*jsonrpc.Response); ok {
			delete(a.pending, r.ID)
			a.done.Broadcast()
		}
	}
	return n, err
}

func (a *answerWriter) Close() error { return nil } // stdout stays open

// pump copies JSON-RPC lines from in to the SDK, answering the ones it would choke on.
func (a *answerWriter) pump(in io.Reader, pw *io.PipeWriter) {
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		if s := bytes.TrimSpace(line); len(s) > 0 {
			if code, msg := a.check(s); code != 0 {
				b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": code, "message": msg}})
				a.Write(append(b, '\n'))
			} else if _, werr := pw.Write(append(s, '\n')); werr != nil {
				return // the session is closed
			}
		}
		if err != nil {
			a.mu.Lock()
			for len(a.pending) > 0 {
				a.done.Wait()
			}
			a.mu.Unlock()
			pw.CloseWithError(err)
			return
		}
	}
}

// check returns a JSON-RPC error code for a line the SDK cannot take (0: pass it on) and notes requests and cancels.
func (a *answerWriter) check(s []byte) (int, string) {
	if !json.Valid(s) {
		return jsonrpc.CodeParseError, "Parse error"
	}
	if s[0] == '[' { // a batch: the SDK answers it as a whole
		return 0, ""
	}
	m, err := jsonrpc.DecodeMessage(s)
	if err != nil {
		return jsonrpc.CodeInvalidRequest, "Invalid Request: " + err.Error()
	}
	if req, ok := m.(*jsonrpc.Request); ok {
		a.mu.Lock()
		defer a.mu.Unlock()
		if req.ID.IsValid() {
			a.pending[req.ID] = true
		} else if req.Method == "notifications/cancelled" { // a cancelled request may never be answered
			var p struct {
				RequestID any `json:"requestId"`
			}
			if json.Unmarshal(req.Params, &p) == nil {
				if id, err := jsonrpc.MakeID(p.RequestID); err == nil {
					delete(a.pending, id)
					a.done.Broadcast()
				}
			}
		}
	}
	return 0, ""
}
