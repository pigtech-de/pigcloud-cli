package daemon

import (
	"encoding/json"
	"errors"
	"net"
	"time"

	"pigcloud/internal/mount"
	"pigcloud/internal/mount/mlog"
	"pigcloud/internal/netutil"
)

type ipcHost interface {
	ipcName() string
	ipcToken() string
	ipcShutdownCh() <-chan struct{}
	beginMutation() bool
	endMutation()
	handleStatus(enc *json.Encoder)
	setPinned(remotePath string, pinned bool)
	flushWriteback(budget time.Duration) (int, error)
	cleanRejected() (int, error)
	retryFailed(remotePath string) (int, error)
	shutdown()
	handleExtra(req mount.DaemonRequest, enc *json.Encoder) bool
}

func acceptIPC(h ipcHost, listener net.Listener) {
	var backoff netutil.AcceptBackoff
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-h.ipcShutdownCh():
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				mlog.Infof("%s: IPC listener closed, stopping accept loop", h.ipcName())
				return
			}
			delay := backoff.Next()
			mlog.Warnf("%s: accept failed, retrying in %v: %v", h.ipcName(), delay, err)
			select {
			case <-h.ipcShutdownCh():
				return
			case <-time.After(delay):
			}
			continue
		}
		backoff.Reset()
		go handleIPCConn(h, conn)
	}
}

func handleIPCConn(h ipcHost, conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var req mount.DaemonRequest
	if err := dec.Decode(&req); err != nil {
		enc.Encode(mount.DaemonResponse{Error: "invalid request"})
		return
	}

	if req.Token != h.ipcToken() {
		enc.Encode(mount.DaemonResponse{Error: "unauthorized"})
		return
	}

	if isMutatingIPC(req.Action) {
		if !h.beginMutation() {
			enc.Encode(mount.DaemonResponse{Error: "shutting down"})
			return
		}
		defer h.endMutation()
	}

	if req.Action == "flush" {
		conn.SetDeadline(time.Now().Add(mount.FlushDeadline))
	}

	switch req.Action {
	case "ping":
		enc.Encode(mount.DaemonResponse{OK: true})

	case "status":
		h.handleStatus(enc)

	case "shutdown":
		enc.Encode(mount.DaemonResponse{OK: true})
		go func() {
			time.Sleep(100 * time.Millisecond)
			h.shutdown()
		}()

	case "pin", "unpin":
		if req.Path != "" {
			h.setPinned(req.Path, req.Action == "pin")
		}
		enc.Encode(mount.DaemonResponse{OK: true})

	case "flush":
		flushed, err := h.flushWriteback(mount.FlushBudget)
		if err != nil {
			enc.Encode(mount.DaemonResponse{OK: false, Error: err.Error()})
		} else {
			enc.Encode(mount.DaemonResponse{OK: true, PendingCount: flushed})
		}

	case "clean":
		count, err := h.cleanRejected()
		if err != nil {
			enc.Encode(mount.DaemonResponse{OK: false, Error: err.Error()})
		} else {
			enc.Encode(mount.DaemonResponse{OK: true, Cleaned: count})
		}

	case "retry":
		count, err := h.retryFailed(req.Path)
		if err != nil {
			enc.Encode(mount.DaemonResponse{OK: false, Error: err.Error()})
		} else {
			enc.Encode(mount.DaemonResponse{OK: true, Retried: count})
		}

	default:
		if !h.handleExtra(req, enc) {
			enc.Encode(mount.DaemonResponse{Error: "unknown action"})
		}
	}
}
