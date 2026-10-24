// Package cortex — executor.go: ordinaryExecutor and sdkExecutor implementations.
//
// pclntab entries:
//   - ordinaryExecutor.Close                           0x7fff860
//   - ordinaryExecutor.Run                             0x7fffd80
//   - ordinaryExecutor.Send                            0x7fffe20
//   - ordinaryExecutor.Listen                          0x7fffc60
//   - ordinaryExecutor.MaybeRevive                     0x7fffce0
//   - ordinaryExecutor.GetTrajectory                   0x7fffaa0
//   - ordinaryExecutor.GetAgentState                   0x7fff8c0
//   - ordinaryExecutor.GetExecutorLoopStatus           0x7fff920
//   - ordinaryExecutor.GetExecutorLoopStatusAndLastIdleTime 0x7fff980
//   - ordinaryExecutor.GetRunStateInternal             0x7fff9a0
//   - ordinaryExecutor.HandleExecutionError            0x7fffbc0
//   - ordinaryExecutor.SendBookmark                    0x7fffea0
//   - ordinaryExecutor.WaitAndFlushSnapshot            0x7ffff20
//   - sdkExecutor.Close                                0x7ffedc0
//   - sdkExecutor.Run                                  0x7fff640
//   - sdkExecutor.Send                                 0x7fec700
//   - sdkExecutor.Listen                               0x7fff480
//   - sdkExecutor.MaybeRevive                          0x7fff560
//   - sdkExecutor.GetTrajectory                        0x7fff240
//   - sdkExecutor.GetAgentState                        0x7ffee80
//   - sdkExecutor.GetExecutorLoopStatus                0x7ffef40
//   - sdkExecutor.GetExecutorLoopStatusAndLastIdleTime 0x7fff000
//   - sdkExecutor.GetRunStateInternal                  0x7fff020
//   - sdkExecutor.HandleExecutionError                 0x7fff3a0
//   - sdkExecutor.SendBookmark                         0x7fff720
//   - sdkExecutor.WaitAndFlushSnapshot                 0x7fff800
//   - sdkExecutor.WarmUpAgentProcess                   0x7fd9400
//   - sdkExecutor.executeScript                        0x7fd8600
//   - sdkExecutor.getDefaultWorkspaceCwd               0x7fd6ea0
//   - sdkExecutor.getWorkspaceCwd                      0x7fd7040
//   - sdkExecutor.surfaceAgentProcessError             0x7fd9900
//   - sdkExecutor.updateSdkExecutableStatus            0x7fda160
//   - sdkExecutor.updateSdkExecutableStatusLocked      0x7fda2e0
package cortex

import (
	"sync"
	"time"
)

// ordinaryExecutor is the recovered `(*ordinaryExecutor)` from
// google3/third_party/jetski/cortex/cortex.
type ordinaryExecutor struct {
	mu           sync.Mutex
	closed       bool
	trajectoryID string
}

// Close closes the ordinary executor. v1.3.5 binary addr 0x7fff860,
// name=Close, signature=(e *ordinaryExecutor) error.
func (e *ordinaryExecutor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

// Run runs the ordinary executor. v1.3.5 binary addr 0x7fffd80,
// name=Run, signature=(e *ordinaryExecutor, ctx ExecContext, prompt
// string) error.
func (e *ordinaryExecutor) Run(_ ExecContext, _ string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	return nil
}

// Send sends a message. v1.3.5 binary addr 0x7fffe20, name=Send,
// signature=(e *ordinaryExecutor, msg AgentMessage) error.
func (e *ordinaryExecutor) Send(_ AgentMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	return nil
}

// Listen returns the agent state update channel. v1.3.5 binary addr
// 0x7fffc60, name=Listen, signature=(e *ordinaryExecutor) <-chan
// AgentStateUpdate.
func (e *ordinaryExecutor) Listen() <-chan AgentStateUpdate {
	return make(chan AgentStateUpdate)
}

// MaybeRevive revives the executor. v1.3.5 binary addr 0x7fffce0,
// name=MaybeRevive, signature=(e *ordinaryExecutor) error.
func (e *ordinaryExecutor) MaybeRevive() error {
	return nil
}

// GetTrajectory returns the trajectory. v1.3.5 binary addr 0x7fffaa0,
// name=GetTrajectory, signature=(e *ordinaryExecutor) (Trajectory, error).
func (e *ordinaryExecutor) GetTrajectory() (Trajectory, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return Trajectory{ID: e.trajectoryID}, nil
}

// GetAgentState returns the agent state. v1.3.5 binary addr 0x7fff8c0,
// name=GetAgentState, signature=(e *ordinaryExecutor) AgentState.
func (e *ordinaryExecutor) GetAgentState() AgentState {
	return AgentState{Status: SubagentRunning, TrajectoryID: e.trajectoryID}
}

// GetExecutorLoopStatus returns the loop status. v1.3.5 binary addr
// 0x7fff920, name=GetExecutorLoopStatus, signature=(e
// *ordinaryExecutor) ExecutorLoopStatus.
func (e *ordinaryExecutor) GetExecutorLoopStatus() ExecutorLoopStatus {
	return LoopStatusIdle
}

// GetExecutorLoopStatusAndLastIdleTime returns the loop status and last
// idle time. v1.3.5 binary addr 0x7fff980,
// name=GetExecutorLoopStatusAndLastIdleTime, signature=(e
// *ordinaryExecutor) (ExecutorLoopStatus, time.Time).
func (e *ordinaryExecutor) GetExecutorLoopStatusAndLastIdleTime() (ExecutorLoopStatus, time.Time) {
	return LoopStatusIdle, time.Now()
}

// GetRunStateInternal returns the run state. v1.3.5 binary addr
// 0x7fff9a0, name=GetRunStateInternal, signature=(e
// *ordinaryExecutor) interface{}.
func (e *ordinaryExecutor) GetRunStateInternal() interface{} {
	return nil
}

// HandleExecutionError handles an execution error. v1.3.5 binary addr
// 0x7fffbc0, name=HandleExecutionError, signature=(e
// *ordinaryExecutor, err error) error.
func (e *ordinaryExecutor) HandleExecutionError(err error) error {
	return err
}

// SendBookmark sends a bookmark. v1.3.5 binary addr 0x7fffea0,
// name=SendBookmark, signature=(e *ordinaryExecutor, b Bookmark) error.
func (e *ordinaryExecutor) SendBookmark(_ Bookmark) error {
	return nil
}

// WaitAndFlushSnapshot waits for the snapshot to flush. v1.3.5 binary
// addr 0x7ffff20, name=WaitAndFlushSnapshot, signature=(e
// *ordinaryExecutor) error.
func (e *ordinaryExecutor) WaitAndFlushSnapshot() error {
	return nil
}

// sdkExecutor is the recovered `(*sdkExecutor)` from
// google3/third_party/jetski/cortex/cortex.
type sdkExecutor struct {
	mu           sync.Mutex
	cmd          *sdkProcess // child process handle
	closed       bool
	trajectoryID string
}

// sdkProcess is the recovered child-process handle. The 1.3.5 binary
// uses os/exec.Cmd; the stub here captures the same surface.
type sdkProcess struct {
	Path string
	Args []string
	Env  []string
}

// Close closes the sdk executor. v1.3.5 binary addr 0x7ffedc0,
// name=Close, signature=(e *sdkExecutor) error.
func (e *sdkExecutor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	return nil
}

// Run runs the sdk executor. v1.3.5 binary addr 0x7fff640, name=Run,
// signature=(e *sdkExecutor, ctx ExecContext, prompt string) error.
func (e *sdkExecutor) Run(_ ExecContext, _ string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	return nil
}

// Send sends a message. v1.3.5 binary addr 0x7fec700, name=Send,
// signature=(e *sdkExecutor, msg AgentMessage) error.
func (e *sdkExecutor) Send(_ AgentMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return ErrClosed
	}
	return nil
}

// Listen returns the agent state update channel. v1.3.5 binary addr
// 0x7fff480, name=Listen, signature=(e *sdkExecutor) <-chan
// AgentStateUpdate.
func (e *sdkExecutor) Listen() <-chan AgentStateUpdate {
	return make(chan AgentStateUpdate)
}

// MaybeRevive revives the sdk executor. v1.3.5 binary addr 0x7fff560,
// name=MaybeRevive, signature=(e *sdkExecutor) error.
func (e *sdkExecutor) MaybeRevive() error {
	return nil
}

// GetTrajectory returns the trajectory. v1.3.5 binary addr 0x7fff240,
// name=GetTrajectory, signature=(e *sdkExecutor) (Trajectory, error).
func (e *sdkExecutor) GetTrajectory() (Trajectory, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return Trajectory{ID: e.trajectoryID}, nil
}

// GetAgentState returns the agent state. v1.3.5 binary addr 0x7ffee80,
// name=GetAgentState, signature=(e *sdkExecutor) AgentState.
func (e *sdkExecutor) GetAgentState() AgentState {
	return AgentState{Status: SubagentRunning, TrajectoryID: e.trajectoryID}
}

// GetExecutorLoopStatus returns the loop status. v1.3.5 binary addr
// 0x7ffef40, name=GetExecutorLoopStatus, signature=(e *sdkExecutor)
// ExecutorLoopStatus.
func (e *sdkExecutor) GetExecutorLoopStatus() ExecutorLoopStatus {
	return LoopStatusIdle
}

// GetExecutorLoopStatusAndLastIdleTime returns the loop status and last
// idle time. v1.3.5 binary addr 0x7fff000,
// name=GetExecutorLoopStatusAndLastIdleTime, signature=(e
// *sdkExecutor) (ExecutorLoopStatus, time.Time).
func (e *sdkExecutor) GetExecutorLoopStatusAndLastIdleTime() (ExecutorLoopStatus, time.Time) {
	return LoopStatusIdle, time.Now()
}

// GetRunStateInternal returns the run state. v1.3.5 binary addr
// 0x7fff020, name=GetRunStateInternal, signature=(e *sdkExecutor)
// interface{}.
func (e *sdkExecutor) GetRunStateInternal() interface{} {
	return nil
}

// HandleExecutionError handles an execution error. v1.3.5 binary addr
// 0x7fff3a0, name=HandleExecutionError, signature=(e *sdkExecutor,
// err error) error.
func (e *sdkExecutor) HandleExecutionError(err error) error {
	return err
}

// SendBookmark sends a bookmark. v1.3.5 binary addr 0x7fff720,
// name=SendBookmark, signature=(e *sdkExecutor, b Bookmark) error.
func (e *sdkExecutor) SendBookmark(_ Bookmark) error {
	return nil
}

// WaitAndFlushSnapshot waits for the snapshot to flush. v1.3.5 binary
// addr 0x7fff800, name=WaitAndFlushSnapshot, signature=(e
// *sdkExecutor) error.
func (e *sdkExecutor) WaitAndFlushSnapshot() error {
	return nil
}

// WarmUpAgentProcess warms up the agent process. v1.3.5 binary addr
// 0x7fd9400, name=WarmUpAgentProcess, signature=(e *sdkExecutor) error.
func (e *sdkExecutor) WarmUpAgentProcess() error {
	return nil
}

// executeScript executes a script. v1.3.5 binary addr 0x7fd8600,
// name=executeScript, signature=(e *sdkExecutor, script string) error.
func (e *sdkExecutor) executeScript(_ string) error {
	return nil
}

// getDefaultWorkspaceCwd returns the default workspace cwd. v1.3.5
// binary addr 0x7fd6ea0, name=getDefaultWorkspaceCwd, signature=(e
// *sdkExecutor) string.
func (e *sdkExecutor) getDefaultWorkspaceCwd() string {
	return ""
}

// getWorkspaceCwd returns the workspace cwd. v1.3.5 binary addr
// 0x7fd7040, name=getWorkspaceCwd, signature=(e *sdkExecutor, ws
// WorkspaceProject) string.
func (e *sdkExecutor) getWorkspaceCwd(_ WorkspaceProject) string {
	return ""
}

// surfaceAgentProcessError surfaces an agent process error. v1.3.5
// binary addr 0x7fd9900, name=surfaceAgentProcessError, signature=(e
// *sdkExecutor, err error) error.
func (e *sdkExecutor) surfaceAgentProcessError(err error) error {
	return err
}

// updateSdkExecutableStatus updates the sdk executable status. v1.3.5
// binary addr 0x7fda160, name=updateSdkExecutableStatus,
// signature=(e *sdkExecutor, status string) error.
func (e *sdkExecutor) updateSdkExecutableStatus(_ string) error {
	return nil
}

// updateSdkExecutableStatusLocked updates the sdk executable status
// (caller holds lock). v1.3.5 binary addr 0x7fda2e0,
// name=updateSdkExecutableStatusLocked, signature=(e *sdkExecutor,
// status string) error.
func (e *sdkExecutor) updateSdkExecutableStatusLocked(_ string) error {
	return nil
}
