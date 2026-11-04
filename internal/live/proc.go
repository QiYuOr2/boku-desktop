package live

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// readyQuiet is how long CrispASR's stderr has to stay quiet before the model
// counts as loaded. The stream has no ready event; the log stops once the
// model is in memory.
const readyQuiet = 400 * time.Millisecond

type processSpec struct {
	name     string
	label    string
	path     string
	args     []string
	dir      string
	stdin    bool
	extraEnv []string
}

type running struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	exited chan struct{}
	ready  chan struct{}
	err    error
	once   sync.Once
}

func (r *running) markReady() {
	r.once.Do(func() { close(r.ready) })
}

func (r *running) Write(p []byte) error {
	if r == nil || r.stdin == nil {
		return io.ErrClosedPipe
	}
	_, err := r.stdin.Write(p)
	return err
}

func startProcess(ctx context.Context, spec processSpec) (*running, error) {
	cmd := exec.CommandContext(ctx, spec.path, spec.args...)
	cmd.Dir = spec.dir
	cmd.WaitDelay = 3 * time.Second
	if len(spec.extraEnv) > 0 {
		cmd.Env = append(os.Environ(), spec.extraEnv...)
	}
	hideConsole(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	var stdin io.WriteCloser
	if spec.stdin {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	proc := &running{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		exited: make(chan struct{}),
		ready:  make(chan struct{}),
	}
	go proc.wait()
	go proc.watchStderr(stderr)
	return proc, nil
}

func (r *running) wait() {
	r.err = r.cmd.Wait()
	if r.stdin != nil {
		_ = r.stdin.Close()
	}
	close(r.exited)
}

func (r *running) watchStderr(stderr io.Reader) {
	var last atomic.Int64
	saw := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 512)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				last.Store(time.Now().UnixNano())
				select {
				case saw <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	seen := false
	for {
		select {
		case <-r.exited:
			return
		case <-saw:
			seen = true
		case <-ticker.C:
			stamp := last.Load()
			if seen && stamp > 0 && time.Since(time.Unix(0, stamp)) >= readyQuiet {
				r.markReady()
				return
			}
		}
	}
}

type procSlot struct {
	spec     processSpec
	mu       sync.Mutex
	proc     *running
	restarts int
}

func (s *procSlot) current() *running {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proc
}

func (s *procSlot) alive() bool {
	proc := s.current()
	if proc == nil {
		return false
	}
	select {
	case <-proc.exited:
		return false
	default:
		return true
	}
}

// run starts the process, and starts it once more if it exits. The second
// exit is returned. A cancelled context is a stop, not a crash.
func (s *procSlot) run(ctx context.Context, onLine func(string), onReady func(*running)) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		proc, err := startProcess(ctx, s.spec)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.proc = proc
		s.mu.Unlock()
		if onLine != nil {
			go scanLines(proc.stdout, onLine)
		}
		if onReady != nil {
			go func(proc *running) {
				select {
				case <-proc.ready:
					onReady(proc)
				case <-proc.exited:
				case <-ctx.Done():
				}
			}(proc)
		}
		select {
		case <-ctx.Done():
			<-proc.exited
			return nil
		case <-proc.exited:
			if ctx.Err() != nil {
				return nil
			}
			slog.Warn("live process exited", "event", "live_process_exited", "process", s.spec.name, "error", proc.err)
			s.mu.Lock()
			s.restarts++
			restarts := s.restarts
			s.mu.Unlock()
			if restarts > 1 {
				return errString(s.spec.label + "进程退出了，模型没有载入")
			}
		}
	}
}

func scanLines(r io.Reader, fn func(string)) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fn(scanner.Text())
	}
}
