// Package supervisor starts and stops registered services and captures their output.
// Process services execute one program with arguments. They do not run through a shell.
package supervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cng1985/ncode/internal/model"
	"go.uber.org/fx"
)

const (
	maxRunningServices = 20
	serviceIDEnv       = "NCODE_SERVICE_ID"
	maxLogRunes        = 4000
)

var (
	ErrAlreadyRunning = errors.New("service is already running")
	ErrNotRunning     = errors.New("service is not running")
	ErrTooMany        = errors.New("too many services are running")
)

var Module = fx.Module("supervisor", fx.Provide(NewGormStore, NewManager))

type CommandError struct{ Reason string }

func (e *CommandError) Error() string { return e.Reason }

type Spec struct {
	ID      int64
	Kind    string
	Command string
	Args    []string
	WorkDir string
}

type Launch struct {
	ID      int64
	Kind    string
	Command string
	Args    []string
	WorkDir string
}

type LogEntry struct {
	ServiceID int64
	Stream    string
	Message   string
}

type RuntimeState struct {
	State        int
	PID          int
	ExitCode     *int
	ClearExit    bool
	StartedAt    *time.Time
	StoppedAt    *time.Time
	ClearStopped bool
}

type RunningRef struct {
	ID  int64
	PID int
}

type Store interface {
	AppendLog(ctx context.Context, entry LogEntry) error
	UpdateRuntime(ctx context.Context, serviceID int64, state RuntimeState) error
	ListRunning(ctx context.Context) ([]RunningRef, error)
}

type Manager struct {
	store     Store
	logger    *slog.Logger
	heartbeat time.Duration
	mu        sync.Mutex
	running   map[int64]*running
}

type running struct {
	cancel        context.CancelFunc
	cmd           *exec.Cmd
	stdout        *lineWriter
	stderr        *lineWriter
	done          chan struct{}
	stopRequested atomic.Bool
}

func NewManager(store Store, logger *slog.Logger, lc fx.Lifecycle) *Manager {
	manager := &Manager{
		store:     store,
		logger:    logger,
		heartbeat: time.Second,
		running:   map[int64]*running{},
	}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := manager.Reconcile(ctx); err != nil {
				return fmt.Errorf("reconcile services: %w", err)
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			done := make(chan struct{})
			go func() {
				manager.StopAll()
				close(done)
			}()
			select {
			case <-done:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	})
	return manager
}

func (m *Manager) Reconcile(ctx context.Context) error {
	refs, err := m.store.ListRunning(ctx)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		message := "服务管理器已重启，该服务已标记为停止。"
		if processOwned(ref.PID, ref.ID) {
			signalPID(ref.PID, syscall.SIGTERM)
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) && processOwned(ref.PID, ref.ID) {
				time.Sleep(50 * time.Millisecond)
			}
			if processOwned(ref.PID, ref.ID) {
				signalPID(ref.PID, syscall.SIGKILL)
			}
			message = "服务管理器已重启，已结束上次未跟踪的进程并标记为停止。"
		}
		now := time.Now()
		if err := m.store.UpdateRuntime(ctx, ref.ID, RuntimeState{State: model.ServiceStopped, StoppedAt: &now}); err != nil {
			return err
		}
		if err := m.store.AppendLog(ctx, LogEntry{ServiceID: ref.ID, Stream: "system", Message: message}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Start(spec Spec) error {
	launch, err := Prepare(spec)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &running{cancel: cancel, done: make(chan struct{})}

	m.mu.Lock()
	if _, ok := m.running[spec.ID]; ok {
		m.mu.Unlock()
		cancel()
		return ErrAlreadyRunning
	}
	if len(m.running) >= maxRunningServices {
		m.mu.Unlock()
		cancel()
		return ErrTooMany
	}
	m.running[spec.ID] = run
	m.mu.Unlock()

	release := func(cause error) error {
		m.mu.Lock()
		delete(m.running, spec.ID)
		m.mu.Unlock()
		cancel()
		if run.cmd != nil {
			signalGroup(run.cmd, syscall.SIGKILL)
			_ = run.cmd.Wait()
			run.stdout.Flush()
			run.stderr.Flush()
		}
		close(run.done)
		finished := time.Now()
		code := 1
		_ = m.store.UpdateRuntime(context.Background(), spec.ID, RuntimeState{
			State:     model.ServiceFailed,
			ExitCode:  &code,
			StoppedAt: &finished,
		})
		m.emit(spec.ID, "system", "启动失败："+cause.Error())
		var commandErr *CommandError
		var execErr *exec.Error
		var pathErr *os.PathError
		if errors.As(cause, &commandErr) {
			return cause
		}
		if errors.As(cause, &execErr) || errors.As(cause, &pathErr) {
			return &CommandError{Reason: "command could not be started"}
		}
		return cause
	}

	if launch.Kind == model.ServiceKindProcess {
		cmd, stdout, stderr, err := startProcess(launch, func(line string) { m.emit(spec.ID, "stdout", line) }, func(line string) { m.emit(spec.ID, "stderr", line) })
		if err != nil {
			return release(err)
		}
		run.cmd = cmd
		run.stdout = stdout
		run.stderr = stderr
		now := time.Now()
		if err := m.store.UpdateRuntime(context.Background(), spec.ID, RuntimeState{
			State:        model.ServiceRunning,
			PID:          cmd.Process.Pid,
			ClearExit:    true,
			StartedAt:    &now,
			ClearStopped: true,
		}); err != nil {
			return release(err)
		}
		m.emit(spec.ID, "system", "服务已启动")
		go m.waitProcess(spec.ID, run, stdout, stderr)
		return nil
	}

	now := time.Now()
	if err := m.store.UpdateRuntime(context.Background(), spec.ID, RuntimeState{
		State:        model.ServiceRunning,
		ClearExit:    true,
		StartedAt:    &now,
		ClearStopped: true,
	}); err != nil {
		return release(err)
	}
	m.emit(spec.ID, "system", "服务已启动")
	go m.runBuiltin(ctx, spec.ID, run, launch.Command)
	return nil
}

func (m *Manager) Stop(id int64) error {
	m.mu.Lock()
	run, ok := m.running[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotRunning
	}
	run.stopRequested.Store(true)
	cmd := run.cmd
	cancel := run.cancel
	done := run.done
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if cmd != nil {
		signalGroup(cmd, syscall.SIGTERM)
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		if cmd != nil {
			signalGroup(cmd, syscall.SIGKILL)
		}
		<-done
		return nil
	}
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]int64, 0, len(m.running))
	for id := range m.running {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			_ = m.Stop(id)
		}(id)
	}
	wg.Wait()
}

func (m *Manager) runBuiltin(ctx context.Context, id int64, run *running, command string) {
	var waitErr error
	switch command {
	case "heartbeat":
		m.emit(id, "stdout", "心跳服务已开始")
		interval := m.heartbeat
		if interval <= 0 {
			interval = time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		count := 0
		for {
			select {
			case <-ctx.Done():
				m.emit(id, "stdout", "心跳服务已收到停止信号")
				goto finish
			case tick := <-ticker.C:
				count++
				m.emit(id, "stdout", fmt.Sprintf("heartbeat %d %s", count, tick.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05")))
			}
		}
	case "once":
		m.emit(id, "stdout", "短任务开始")
		m.emit(id, "stdout", "短任务完成")
	default:
		waitErr = &CommandError{Reason: "builtin command must be heartbeat or once"}
	}
finish:
	m.finish(id, run, waitErr)
}

func (m *Manager) waitProcess(id int64, run *running, stdout, stderr *lineWriter) {
	waitErr := run.cmd.Wait()
	stdout.Flush()
	stderr.Flush()
	m.finish(id, run, waitErr)
}

func (m *Manager) finish(id int64, run *running, waitErr error) {
	m.mu.Lock()
	delete(m.running, id)
	stopRequested := run.stopRequested.Load()
	m.mu.Unlock()

	now := time.Now()
	state := model.ServiceStopped
	var code *int
	message := "服务已停止"
	if !stopRequested && waitErr != nil {
		state = model.ServiceFailed
		exit := exitCode(waitErr)
		code = &exit
		message = fmt.Sprintf("服务异常退出，退出码 %d", exit)
	} else if waitErr != nil {
		exit := exitCode(waitErr)
		code = &exit
		message = fmt.Sprintf("服务已停止，退出码 %d", exit)
	} else if !stopRequested {
		zero := 0
		code = &zero
		message = "服务已退出，退出码 0"
	}
	if err := m.store.UpdateRuntime(context.Background(), id, RuntimeState{State: state, ExitCode: code, StoppedAt: &now}); err != nil && m.logger != nil {
		m.logger.Error("update service runtime", "service", id, "error", err)
	}
	m.emit(id, "system", message)
	close(run.done)
}

func (m *Manager) emit(serviceID int64, stream, message string) {
	message = trimMessage(message)
	if message == "" {
		return
	}
	if err := m.store.AppendLog(context.Background(), LogEntry{ServiceID: serviceID, Stream: stream, Message: message}); err != nil && m.logger != nil {
		m.logger.Error("append service log", "service", serviceID, "error", err)
	}
}

func startProcess(launch Launch, emitStdout, emitStderr func(string)) (*exec.Cmd, *lineWriter, *lineWriter, error) {
	cmd := exec.Command(launch.Command, launch.Args...)
	cmd.Env = withServiceEnv(launch.ID)
	if launch.WorkDir != "" {
		cmd.Dir = launch.WorkDir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout := &lineWriter{emit: emitStdout}
	stderr := &lineWriter{emit: emitStderr}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}
	return cmd, stdout, stderr, nil
}

type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	var lines []string
	for {
		index := bytes.IndexByte(w.buf, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:index]), "\r")
		w.buf = append([]byte(nil), w.buf[index+1:]...)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(w.buf) > 256*1024 {
		lines = append(lines, string(w.buf))
		w.buf = nil
	}
	w.mu.Unlock()
	for _, line := range lines {
		w.emit(line)
	}
	return len(p), nil
}

func (w *lineWriter) Flush() {
	if w == nil {
		return
	}
	w.mu.Lock()
	line := strings.TrimRight(string(w.buf), "\r\n")
	w.buf = nil
	w.mu.Unlock()
	if line != "" {
		w.emit(line)
	}
}

func withServiceEnv(id int64) []string {
	prefix := serviceIDEnv + "="
	value := prefix + strconv.FormatInt(id, 10)
	filtered := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		filtered = append(filtered, item)
	}
	return append(filtered, value)
}

func signalGroup(cmd *exec.Cmd, signal syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, signal); err != nil {
		_ = cmd.Process.Signal(signal)
	}
}

func signalPID(pid int, signal syscall.Signal) {
	if pid <= 1 {
		return
	}
	if err := syscall.Kill(-pid, signal); err != nil {
		_ = syscall.Kill(pid, signal)
	}
}

func processOwned(pid int, serviceID int64) bool {
	if pid <= 1 || serviceID <= 0 {
		return false
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return false
	}
	needle := serviceIDEnv + "=" + strconv.FormatInt(serviceID, 10)
	for _, part := range strings.Split(string(data), "\x00") {
		if part == needle {
			return true
		}
	}
	return false
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func trimMessage(message string) string {
	runes := []rune(message)
	if len(runes) <= maxLogRunes {
		return message
	}
	return string(runes[:maxLogRunes])
}

var bareName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

var blockedCommands = map[string]struct{}{
	"sh": {}, "bash": {}, "dash": {}, "zsh": {}, "fish": {}, "csh": {}, "tcsh": {}, "ksh": {}, "rbash": {},
	"sudo": {}, "su": {}, "doas": {}, "pkexec": {},
	"systemctl": {}, "service": {}, "launchctl": {},
	"kill": {}, "killall": {}, "pkill": {},
	"shutdown": {}, "reboot": {}, "poweroff": {}, "halt": {}, "init": {}, "telinit": {},
	"mkfs": {}, "dd": {}, "nohup": {}, "setsid": {},
}

func Prepare(spec Spec) (Launch, error) {
	spec.Kind = strings.TrimSpace(spec.Kind)
	spec.Command = strings.TrimSpace(spec.Command)
	spec.WorkDir = strings.TrimSpace(spec.WorkDir)
	if spec.Kind != model.ServiceKindBuiltin && spec.Kind != model.ServiceKindProcess {
		return Launch{}, &CommandError{Reason: "kind must be builtin or process"}
	}
	if len(spec.Args) > 32 {
		return Launch{}, &CommandError{Reason: "too many arguments"}
	}
	for _, arg := range spec.Args {
		if strings.ContainsRune(arg, 0) || len(arg) > 500 {
			return Launch{}, &CommandError{Reason: "invalid argument"}
		}
	}
	if spec.Kind == model.ServiceKindBuiltin {
		if spec.Command != "heartbeat" && spec.Command != "once" {
			return Launch{}, &CommandError{Reason: "builtin command must be heartbeat or once"}
		}
		if len(spec.Args) > 0 || spec.WorkDir != "" {
			return Launch{}, &CommandError{Reason: "builtin services do not accept arguments or a working directory"}
		}
		return Launch{ID: spec.ID, Kind: spec.Kind, Command: spec.Command}, nil
	}
	if err := validateExecutable(spec.Command); err != nil {
		return Launch{}, err
	}
	workDir := ""
	if spec.WorkDir != "" {
		if strings.Contains(spec.WorkDir, "..") || !filepath.IsAbs(spec.WorkDir) {
			return Launch{}, &CommandError{Reason: "working directory must be an existing absolute path"}
		}
		workDir = filepath.Clean(spec.WorkDir)
		info, err := os.Stat(workDir)
		if err != nil || !info.IsDir() {
			return Launch{}, &CommandError{Reason: "working directory must be an existing absolute path"}
		}
	}
	resolved, err := resolveExecutable(spec.Command)
	if err != nil {
		return Launch{}, err
	}
	return Launch{ID: spec.ID, Kind: spec.Kind, Command: resolved, Args: append([]string(nil), spec.Args...), WorkDir: workDir}, nil
}

func validateExecutable(command string) error {
	if command == "" || len(command) > 200 {
		return &CommandError{Reason: "command is required"}
	}
	if strings.ContainsAny(command, "\x00\n\r;&|$`<>") || strings.Contains(command, "..") {
		return &CommandError{Reason: "command must be a bare executable name or an absolute path"}
	}
	if strings.Contains(command, "/") {
		if !filepath.IsAbs(command) || filepath.Clean(command) != command {
			return &CommandError{Reason: "command must be a bare executable name or an absolute path"}
		}
	} else if !bareName.MatchString(command) {
		return &CommandError{Reason: "command must be a bare executable name or an absolute path"}
	}
	if isBlocked(filepath.Base(command)) {
		return &CommandError{Reason: "command is not allowed"}
	}
	return nil
}

func resolveExecutable(command string) (string, error) {
	path := command
	if !filepath.IsAbs(command) {
		resolved, err := exec.LookPath(command)
		if err != nil {
			return "", &CommandError{Reason: "command was not found"}
		}
		path = resolved
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", &CommandError{Reason: "command was not found"}
	}
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", &CommandError{Reason: "command was not found"}
	}
	if isBlocked(filepath.Base(command)) || isBlocked(filepath.Base(path)) || isBlocked(filepath.Base(evaluated)) {
		return "", &CommandError{Reason: "command is not allowed"}
	}
	return path, nil
}

func isBlocked(name string) bool {
	_, ok := blockedCommands[strings.ToLower(name)]
	return ok
}
