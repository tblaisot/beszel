package systems

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/henrygd/beszel/internal/common"
	"golang.org/x/crypto/ssh"
)

// execOpenTimeout bounds opening the SSH session and the agent's "ready" answer.
const execOpenTimeout = 20 * time.Second

// ExecStream is an open console on an agent: ExecFrames go in and out over an SSH session.
type ExecStream struct {
	session *ssh.Session
	stdin   io.WriteCloser
	decoder *cbor.Decoder
	encoder *cbor.Encoder
	sendMu  sync.Mutex
	once    sync.Once
}

// OpenExec opens a shell in a container over a new SSH session. It only works when the hub reaches
// the agent over SSH; agents that connect to the hub through a WebSocket only are not supported.
func (sys *System) OpenExec(ctx context.Context, req common.ExecRequest) (*ExecStream, error) {
	transport, err := sys.getSSHTransport()
	if err != nil {
		return nil, fmt.Errorf("the console needs the hub to reach the agent over SSH: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, execOpenTimeout)
	defer cancel()
	client, err := transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	session, err := transport.NewSession(ctx, client)
	if err != nil {
		return nil, err
	}
	// closing the session releases a blocked read if the agent never answers
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stop()

	stdout, err := session.StdoutPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		session.Close()
		return nil, err
	}
	if err := session.Shell(); err != nil {
		session.Close()
		return nil, err
	}
	stream := &ExecStream{session: session, stdin: stdin, decoder: cbor.NewDecoder(stdout), encoder: cbor.NewEncoder(stdin)}

	if err := stream.encoder.Encode(common.HubRequest[any]{Action: common.Exec, Data: req}); err != nil {
		stream.Close()
		return nil, fmt.Errorf("failed to send the console request: %w", err)
	}
	var resp common.AgentResponse
	if err := stream.decoder.Decode(&resp); err != nil {
		stream.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("failed to read the console answer: %w", err)
	}
	if resp.Error != "" {
		stream.Close()
		return nil, errors.New(resp.Error)
	}
	return stream, nil
}

// Send writes a frame to the agent. It is safe to call from several goroutines.
func (s *ExecStream) Send(frame common.ExecFrame) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.encoder.Encode(frame)
}

// Recv reads the next frame from the agent. Only one goroutine may call it.
func (s *ExecStream) Recv() (common.ExecFrame, error) {
	var frame common.ExecFrame
	err := s.decoder.Decode(&frame)
	return frame, err
}

// Close ends the session, which ends the shell on the agent.
func (s *ExecStream) Close() {
	s.once.Do(func() {
		_ = s.stdin.Close()
		_ = s.session.Close()
	})
}
