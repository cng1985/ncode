package supervisor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cng1985/ncode/internal/model"
)

type memoryStore struct {
	mu      sync.Mutex
	logs    []LogEntry
	states  map[int64]RuntimeState
	running []RunningRef
}

func newMemoryStore() *memoryStore {
	return &memoryStore{states: map[int64]RuntimeState{}}
}

func (s *memoryStore) AppendLog(_ context.Context, entry LogEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, entry)
	return nil
}

func (s *memoryStore) UpdateRuntime(_ context.Context, serviceID int64, state RuntimeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[serviceID] = state
	return nil
}

func (s *memoryStore) ListRunning(context.Context) ([]RunningRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RunningRef(nil), s.running...), nil
}

func (s *memoryStore) messages(serviceID int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var messages []string
	for _, entry := range s.logs {
		if entry.ServiceID == serviceID {
			messages = append(messages, entry.Stream+":"+entry.Message)
		}
	}
	return messages
}

func (s *memoryStore) state(serviceID int64) (RuntimeState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[serviceID]
	return state, ok
}

func newTestManager(store Store) *Manager {
	return &Manager{store: store, heartbeat: 20 * time.Millisecond, running: map[int64]*running{}}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func TestPrepareRejectsUnsafeCommands(t *testing.T) {
	cases := []Spec{
		{Kind: "process", Command: "bash"},
		{Kind: "process", Command: "sh"},
		{Kind: "process", Command: "systemctl"},
		{Kind: "process", Command: "/bin/bash"},
		{Kind: "process", Command: "echo;rm"},
		{Kind: "process", Command: "../bin/echo"},
		{Kind: "process", Command: "bin/echo"},
		{Kind: "process", Command: "echo", Args: []string{"ok\x00"}},
		{Kind: "builtin", Command: "rm"},
		{Kind: "builtin", Command: "heartbeat", Args: []string{"fast"}},
		{Kind: "process", Command: "echo", WorkDir: "relative"},
		{Kind: "process", Command: "echo", WorkDir: "/tmp/does-not-exist-ncode"},
		{Kind: "other", Command: "echo"},
	}
	for _, spec := range cases {
		if _, err := Prepare(spec); err == nil {
			t.Fatalf("expected error for %+v", spec)
		}
	}
}

func TestPrepareResolvesEcho(t *testing.T) {
	launch, err := Prepare(Spec{Kind: "process", Command: "echo", Args: []string{"hi"}, WorkDir: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(launch.Command) {
		t.Fatalf("resolved command = %q", launch.Command)
	}
	if filepath.Base(launch.Command) == "bash" || filepath.Base(launch.Command) == "sh" {
		t.Fatalf("echo resolved to a shell: %s", launch.Command)
	}
	if launch.WorkDir != "/tmp" || len(launch.Args) != 1 || launch.Args[0] != "hi" {
		t.Fatalf("launch = %+v", launch)
	}
}

func TestBuiltinOnceWritesLogsAndExits(t *testing.T) {
	store := newMemoryStore()
	manager := newTestManager(store)
	if err := manager.Start(Spec{ID: 1, Kind: model.ServiceKindBuiltin, Command: "once"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		state, ok := store.state(1)
		return ok && state.State == model.ServiceStopped
	})
	joined := strings.Join(store.messages(1), "\n")
	for _, expected := range []string{"服务已启动", "短任务开始", "短任务完成", "服务已退出，退出码 0"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("logs %q do not contain %q", joined, expected)
		}
	}
}

func TestHeartbeatCanBeStopped(t *testing.T) {
	store := newMemoryStore()
	manager := newTestManager(store)
	t.Cleanup(manager.StopAll)
	if err := manager.Start(Spec{ID: 2, Kind: model.ServiceKindBuiltin, Command: "heartbeat"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		return strings.Contains(strings.Join(store.messages(2), "\n"), "heartbeat ")
	})
	if err := manager.Stop(2); err != nil {
		t.Fatal(err)
	}
	state, ok := store.state(2)
	if !ok || state.State != model.ServiceStopped {
		t.Fatalf("state = %+v ok=%v", state, ok)
	}
	if err := manager.Stop(2); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("second stop = %v", err)
	}
}

func TestProcessEchoAndWorkDir(t *testing.T) {
	store := newMemoryStore()
	manager := newTestManager(store)
	dir := t.TempDir()
	if err := manager.Start(Spec{ID: 3, Kind: model.ServiceKindProcess, Command: "pwd", WorkDir: dir}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		state, ok := store.state(3)
		return ok && state.State == model.ServiceStopped && strings.Contains(strings.Join(store.messages(3), "\n"), dir)
	})
}

func TestProcessFailureIsRecorded(t *testing.T) {
	if _, err := exec.LookPath("false"); err != nil {
		t.Skip("false is not installed")
	}
	store := newMemoryStore()
	manager := newTestManager(store)
	if err := manager.Start(Spec{ID: 4, Kind: model.ServiceKindProcess, Command: "false"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		state, ok := store.state(4)
		return ok && state.State == model.ServiceFailed && state.ExitCode != nil && *state.ExitCode != 0
	})
}

func TestStopRunningProcess(t *testing.T) {
	store := newMemoryStore()
	manager := newTestManager(store)
	t.Cleanup(manager.StopAll)
	if err := manager.Start(Spec{ID: 5, Kind: model.ServiceKindProcess, Command: "sleep", Args: []string{"30"}}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		state, ok := store.state(5)
		return ok && state.State == model.ServiceRunning && state.PID > 1
	})
	if err := manager.Start(Spec{ID: 5, Kind: model.ServiceKindProcess, Command: "sleep", Args: []string{"30"}}); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second start = %v", err)
	}
	if err := manager.Stop(5); err != nil {
		t.Fatal(err)
	}
	state, _ := store.state(5)
	if state.State != model.ServiceStopped {
		t.Fatalf("state after stop = %+v", state)
	}
}

func TestReconcileMarksUnownedServiceStopped(t *testing.T) {
	store := newMemoryStore()
	store.running = []RunningRef{{ID: 8, PID: 999999}}
	manager := newTestManager(store)
	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, ok := store.state(8)
	if !ok || state.State != model.ServiceStopped {
		t.Fatalf("state = %+v ok=%v", state, ok)
	}
	if !strings.Contains(strings.Join(store.messages(8), "\n"), "已标记为停止") {
		t.Fatalf("logs = %#v", store.messages(8))
	}
}

func TestProcessOwnedRecognizesChild(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	cmd.Env = withServiceEnv(42)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	if !processOwned(cmd.Process.Pid, 42) {
		t.Fatal("expected child to be recognized")
	}
	if processOwned(cmd.Process.Pid, 43) {
		t.Fatal("different service id matched")
	}
	if processOwned(1, 42) || processOwned(os.Getpid(), 42) {
		t.Fatal("unrelated pid matched")
	}
}
