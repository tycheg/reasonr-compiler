package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
)

// JSON-RPC and LSP error codes.
const (
	codeParseError           = -32700
	codeInvalidRequest       = -32600
	codeMethodNotFound       = -32601
	codeInvalidParams        = -32602
	codeInternalError        = -32603
	codeServerNotInitialized = -32002
	codeRequestFailed        = -32803
)

// message is a JSON-RPC 2.0 message: a request (id and method), a
// notification (method only) or a response (id and result or error).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (m *message) isRequest() bool { return m.Method != "" && len(m.ID) > 0 }

// rpcError is a response error.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

func errorf(code int, format string, args ...any) *rpcError {
	return &rpcError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// conn reads and writes messages with a Content-Length header (the LSP base
// protocol). Writing is safe for several goroutines.
type conn struct {
	r  *bufio.Reader
	mu sync.Mutex
	w  io.Writer
}

func newConn(r io.Reader, w io.Writer) *conn {
	return &conn{r: bufio.NewReader(r), w: w}
}

// read reads the next message. io.EOF means the stream is closed.
func (c *conn) read() (*message, error) {
	tp := textproto.NewReader(c.r)
	h, err := tp.ReadMIMEHeader()
	if err != nil {
		if errors.Is(err, io.EOF) && len(h) == 0 {
			return nil, io.EOF
		}
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(h.Get("Content-Length")))
	if err != nil || n < 0 {
		return nil, fmt.Errorf("lsp: invalid Content-Length header %q", h.Get("Content-Length"))
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return nil, err
	}
	m := &message{}
	if err := json.Unmarshal(body, m); err != nil {
		// The body was read: the stream is in sync, we can go on.
		return &message{Error: errorf(codeParseError, "%v", err)}, nil
	}
	return m, nil
}

// write sends a message.
func (c *conn) write(m *message) error {
	m.JSONRPC = "2.0"
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

// reply answers request id with a result or an error.
func (c *conn) reply(id json.RawMessage, result any, rerr *rpcError) error {
	m := &message{ID: id}
	if rerr != nil {
		m.Error = rerr
	} else {
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		m.Result = b
	}
	return c.write(m)
}

// notify sends a notification.
func (c *conn) notify(method string, params any) error {
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(&message{Method: method, Params: b})
}
