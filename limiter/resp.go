package limiter

// A deliberately tiny Redis client (RESP2 over TCP/TLS) so the service has no
// third-party dependencies. It supports exactly what the limiter needs:
// AUTH, SELECT, PING, EVALSHA/EVAL, DEL. No pipelining, no pub/sub, no cluster
// redirects (use a single endpoint or a proxy that handles cluster routing).

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// redisError is an error reply from the server ("-ERR ...").
type redisError string

func (e redisError) Error() string { return string(e) }

type respConn struct {
	net.Conn
	r        *bufio.Reader
	w        *bufio.Writer
	lastUsed time.Time
}

type respClient struct {
	addr     string
	tlsConf  *tls.Config
	username string
	password string
	db       int

	dialTimeout time.Duration
	ioTimeout   time.Duration
	idleTimeout time.Duration

	mu     sync.Mutex
	idle   []*respConn
	maxIdl int
	closed bool
}

func newRespClient(rawURL string) (*respClient, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis url: %w", err)
	}
	c := &respClient{
		dialTimeout: 2 * time.Second,
		ioTimeout:   2 * time.Second,
		idleTimeout: 30 * time.Second,
		maxIdl:      64,
	}
	switch u.Scheme {
	case "redis":
	case "rediss":
		c.tlsConf = &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}
	default:
		return nil, fmt.Errorf("redis url scheme must be redis:// or rediss://, got %q", u.Scheme)
	}
	host, port := u.Hostname(), u.Port()
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "6379"
	}
	c.addr = net.JoinHostPort(host, port)
	if u.User != nil {
		c.username = u.User.Username()
		c.password, _ = u.User.Password()
		// redis://secret@host is a common shorthand for a password-only login.
		if c.password == "" && c.username != "" {
			c.password, c.username = c.username, ""
		}
	}
	if p := strings.Trim(u.Path, "/"); p != "" {
		db, err := strconv.Atoi(p)
		if err != nil || db < 0 {
			return nil, fmt.Errorf("invalid redis db %q", p)
		}
		c.db = db
	}
	return c, nil
}

func (c *respClient) dial(ctx context.Context) (*respConn, error) {
	d := net.Dialer{Timeout: c.dialTimeout}
	var nc net.Conn
	var err error
	if c.tlsConf != nil {
		td := tls.Dialer{NetDialer: &d, Config: c.tlsConf}
		nc, err = td.DialContext(ctx, "tcp", c.addr)
	} else {
		nc, err = d.DialContext(ctx, "tcp", c.addr)
	}
	if err != nil {
		return nil, err
	}
	rc := &respConn{Conn: nc, r: bufio.NewReaderSize(nc, 4096), w: bufio.NewWriterSize(nc, 4096)}
	if c.password != "" {
		args := []any{"AUTH"}
		if c.username != "" {
			args = append(args, c.username)
		}
		args = append(args, c.password)
		if _, err := c.roundTrip(ctx, rc, args); err != nil {
			nc.Close()
			return nil, fmt.Errorf("redis auth: %w", err)
		}
	}
	if c.db != 0 {
		if _, err := c.roundTrip(ctx, rc, []any{"SELECT", c.db}); err != nil {
			nc.Close()
			return nil, fmt.Errorf("redis select: %w", err)
		}
	}
	return rc, nil
}

func (c *respClient) get(ctx context.Context) (conn *respConn, reused bool, err error) {
	now := time.Now()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, false, errors.New("redis client closed")
	}
	for len(c.idle) > 0 {
		n := len(c.idle) - 1
		cn := c.idle[n]
		c.idle = c.idle[:n]
		if now.Sub(cn.lastUsed) > c.idleTimeout {
			cn.Close()
			continue
		}
		c.mu.Unlock()
		return cn, true, nil
	}
	c.mu.Unlock()
	cn, err := c.dial(ctx)
	return cn, false, err
}

func (c *respClient) put(cn *respConn) {
	cn.lastUsed = time.Now()
	c.mu.Lock()
	if c.closed || len(c.idle) >= c.maxIdl {
		c.mu.Unlock()
		cn.Close()
		return
	}
	c.idle = append(c.idle, cn)
	c.mu.Unlock()
}

func (c *respClient) close() {
	c.mu.Lock()
	c.closed = true
	idle := c.idle
	c.idle = nil
	c.mu.Unlock()
	for _, cn := range idle {
		cn.Close()
	}
}

// do sends one command and returns its reply. Error replies are returned as
// redisError. A command that failed on a *reused* connection before any reply
// byte arrived is retried once on a fresh connection: that is the signature of
// the server having closed an idle connection, not of a half-applied command.
func (c *respClient) do(ctx context.Context, args ...any) (any, error) {
	for attempt := 0; ; attempt++ {
		cn, reused, err := c.get(ctx)
		if err != nil {
			return nil, err
		}
		reply, err := c.roundTrip(ctx, cn, args)
		var rerr redisError
		if err == nil || errors.As(err, &rerr) {
			c.put(cn)
			return reply, err
		}
		cn.Close()
		if reused && attempt == 0 && isStaleConn(err) {
			continue
		}
		return nil, err
	}
}

func isStaleConn(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var ne *net.OpError
	if errors.As(err, &ne) {
		msg := ne.Error()
		return strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset")
	}
	return false
}

func (c *respClient) roundTrip(ctx context.Context, cn *respConn, args []any) (any, error) {
	deadline := time.Now().Add(c.ioTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = cn.SetDeadline(deadline)
	if err := writeCommand(cn.w, args); err != nil {
		return nil, err
	}
	if err := cn.w.Flush(); err != nil {
		return nil, err
	}
	reply, err := readReply(cn.r)
	if err != nil {
		return nil, err
	}
	if e, ok := reply.(redisError); ok {
		return nil, e
	}
	return reply, nil
}

func writeCommand(w *bufio.Writer, args []any) error {
	fmt.Fprintf(w, "*%d\r\n", len(args))
	for _, a := range args {
		var s string
		switch v := a.(type) {
		case string:
			s = v
		case []byte:
			s = string(v)
		case int:
			s = strconv.Itoa(v)
		case int64:
			s = strconv.FormatInt(v, 10)
		case bool:
			if v {
				s = "1"
			} else {
				s = "0"
			}
		default:
			s = fmt.Sprint(v)
		}
		if _, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(s), s); err != nil {
			return err
		}
	}
	return nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) < 2 || line[len(line)-2] != '\r' {
		return "", errors.New("malformed RESP line")
	}
	return line[:len(line)-2], nil
}

// readReply returns string, int64, []any, nil or redisError.
func readReply(r *bufio.Reader) (any, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	if line == "" {
		return nil, errors.New("empty RESP line")
	}
	body := line[1:]
	switch line[0] {
	case '+':
		return body, nil
	case '-':
		return redisError(body), nil
	case ':':
		n, err := strconv.ParseInt(body, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("bad RESP integer %q", body)
		}
		return n, nil
	case '$':
		n, err := strconv.Atoi(body)
		if err != nil {
			return nil, fmt.Errorf("bad RESP bulk length %q", body)
		}
		if n < 0 {
			return nil, nil
		}
		buf := make([]byte, n+2)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(body)
		if err != nil {
			return nil, fmt.Errorf("bad RESP array length %q", body)
		}
		if n < 0 {
			return nil, nil
		}
		out := make([]any, n)
		for i := range out {
			if out[i], err = readReply(r); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("unexpected RESP type %q", line[0])
}
