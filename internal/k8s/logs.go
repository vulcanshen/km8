package k8s

import (
	"bufio"
	"context"
	"sync"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// LogLine represents a single log line from a container.
//
// StreamID tags the line with the LogStreamer epoch that emitted it.
// Consumers compare against LogStreamer.CurrentStreamID() to drop
// stale lines from a closed prior stream — buffered residue in the
// channel can still be received with ok=true after Stop closed it
// (Go closed-buffered-channel semantics), and without the tag those
// lines would bleed into the next stream's detail panel.
type LogLine struct {
	StreamID  int64
	Pod       string
	Container string
	Text      string
}

// PodTarget identifies a pod whose containers should be streamed. Used by
// LogStreamer.StartMulti to aggregate logs across multiple pods (e.g. all
// pods of a Deployment's current ReplicaSet).
type PodTarget struct {
	Name       string
	Namespace  string
	Containers []string
}

// streamIDCounter is the process-global monotonic source for every
// LogStreamer's streamID epoch. Per-instance counters (the previous
// design) collide across LogStreamer replacement — ContextChangedMsg
// swaps m.logStreamer for a fresh NewLogStreamer whose streamID
// starts at 0, so the new instance's first Start lands on the same
// streamID value some prior instance already emitted. Buffered LogLine
// residue from the old instance then matches the new instance's
// CurrentStreamID() and bleeds into the new context's logs tab.
// Sourcing every Stop/Start bump from this global makes collisions
// across instances impossible — every epoch in the process is unique.
var streamIDCounter atomic.Int64

// LogStreamer streams logs from one or more containers in a pod.
// It integrates with Bubble Tea through a channel-based message pattern.
//
// aggregate distinguishes single-pod streams (Start: Pod identity is
// implicit — the user is on the Pod's detail panel) from multi-pod
// aggregate streams (StartMulti: pods come from a workload's selector and
// every line needs a Pod tag so the consumer can colour-code by source).
// When aggregate=false, emitted LogLine.Pod is left empty so the renderer
// skips the `<pod-hash>@` prefix segment.
//
// Channel lifecycle: each Start allocates a fresh lines channel + wg;
// Stop synchronously waits for producers (they exit promptly after ctx
// cancel propagates to k8s.Stream) and CLOSES the channel. Parked
// readers (the consumer-side waitForLogLine Cmd) unblock with !ok and
// return nil, ending the chain cleanly — without close, each Stop +
// Start leaked one parked reader per row change because the consumer
// was still blocked on the OLD shared channel.
type LogStreamer struct {
	clientset kubernetes.Interface
	mu        sync.Mutex
	cancel    context.CancelFunc
	wg        *sync.WaitGroup
	lines     chan LogLine
	aggregate bool
	// streamID is the epoch this LogStreamer's current stream was
	// started with — sourced from the package-level streamIDCounter so
	// the value is unique across every LogStreamer instance in the
	// process (cross-instance collisions are otherwise possible when
	// ContextChangedMsg replaces the streamer). Producers capture it
	// at goroutine spawn and tag each emitted LogLine; the consumer-
	// side handler reads CurrentStreamID() and drops lines whose tag
	// doesn't match. This closes the "buffered residue from chOLD
	// bleeding into the new context's logs tab" window that the
	// nil-channel guard fix could only partially address (it stopped
	// the goroutine leak but not the user-visible stale-line append).
	streamID atomic.Int64
}

// NewLogStreamer creates a new LogStreamer for the given clientset.
// The lines channel is left nil — start() allocates it on first
// Start, Stop closes and nils it. Keeps the lifecycle invariant
// uniform: Lines() returns non-nil only while a stream is active.
// The previous eager `make(chan LogLine, 64)` here was dead (start
// always overwrote it) and contradicted Lines()'s nil-after-Stop
// docstring; waitForLogLine has a nil-channel guard so the pre-
// first-Start state is handled cleanly.
func NewLogStreamer(clientset kubernetes.Interface) *LogStreamer {
	return &LogStreamer{
		clientset: clientset,
	}
}

// Start streams the named containers from a single pod. LogLine.Pod is left
// empty on emitted lines so the renderer drops the `<pod-hash>@` prefix
// segment (pod identity is implicit when the user is on Pod detail).
func (ls *LogStreamer) Start(podName, namespace string, containers []string) {
	ls.start(false, []PodTarget{{Name: podName, Namespace: namespace, Containers: containers}})
}

// StartMulti cancels any existing stream and starts streaming logs from
// every (pod, container) pair in `targets`. All lines flow through the
// shared Lines() channel; each LogLine carries Pod / Container / Text so
// the consumer can multiplex by either dimension.
func (ls *LogStreamer) StartMulti(targets []PodTarget) {
	ls.start(true, targets)
}

func (ls *LogStreamer) start(aggregate bool, targets []PodTarget) {
	// Hold the lock across the entire Stop+restart sequence so two
	// concurrent Starts can't interleave (A allocates chA + spawns
	// producers, B's allocation overwrites ls.lines/ls.wg/ls.cancel,
	// A's producers + ctx end up orphaned with no field references —
	// goroutine leak + ctx leak). The public API claims concurrency
	// safety via the mutex; this is what makes that promise true.
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.stopLocked()

	ls.aggregate = aggregate
	ls.lines = make(chan LogLine, 64)
	ls.wg = &sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())
	ls.cancel = cancel
	// Bump streamID for the new stream from the process-global source
	// so producers tag their lines with a fresh epoch unique across
	// every LogStreamer instance. Bump count varies by entry path:
	//   - First Start of a fresh streamer: stopLocked early-returns
	//     (cancel==nil), so this Add is the ONLY bump — one slot
	//     consumed.
	//   - Restart of an active stream: stopLocked bumped first to
	//     invalidate the prior stream's residue, then this Add gives
	//     the new producers their own distinct epoch — two slots
	//     consumed.
	// The stopLocked bump matters most for the stop-without-restart
	// case (RowSelectedMsg default branch, ContextChangedMsg) where
	// only stopLocked fires — without it, any stale LogLine residue
	// would still match CurrentStreamID() since no new Start would
	// have bumped past it.
	streamID := streamIDCounter.Add(1)
	ls.streamID.Store(streamID)

	// Snapshot the channel pointer for producers — they MUST capture
	// it at goroutine spawn rather than reading ls.lines later, so
	// that a subsequent Stop+Start (which swaps ls.lines to a new
	// channel) can't accidentally cross-contaminate streams.
	lines := ls.lines
	wg := ls.wg
	for _, t := range targets {
		for _, c := range t.Containers {
			wg.Add(1)
			go func(podName, namespace, container string) {
				defer wg.Done()
				ls.streamContainer(ctx, lines, streamID, podName, namespace, container)
			}(t.Name, t.Namespace, c)
		}
	}
}

// Stop cancels the current log stream, waits for in-flight producer
// goroutines to exit (they exit promptly after ctx cancel propagates
// to the k8s Stream → scanner.Scan returns false), then closes the
// lines channel so any parked consumer (waitForLogLine) unblocks
// with !ok and returns nil.
//
// Blocks the caller for the duration of producer drain — typically
// milliseconds (scanner.Scan returns immediately when the underlying
// HTTP body is closed by ctx cancel). Synchronous wait is the cost
// of correctness: without it, closing the channel concurrently with
// producers' `case lines <- ...:` would panic.
func (ls *LogStreamer) Stop() {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ls.stopLocked()
}

// stopLocked is Stop's body without the lock acquisition — used both
// by the public Stop (which takes the lock) and by start() (which
// holds it across the full Stop+restart so concurrent Starts can't
// orphan producers). MUST be called with ls.mu held.
//
// Producer goroutines do NOT take ls.mu (they capture `lines` as a
// parameter at spawn time), so wg.Wait while holding mu cannot
// deadlock. Lines() takes mu briefly to read ls.lines; callers that
// race with stopLocked will block until it returns, then see the
// nil-out state and either get a closed channel they had captured
// earlier or nil — both safe.
//
// streamID is bumped at the start of an active-stream stop so any
// LogLine still buffered in the old channel arrives at the consumer
// with a stale tag (against CurrentStreamID's post-bump value) and
// gets dropped before AppendLogLine. Without this, stop-without-
// restart (e.g. RowSelectedMsg default branch, ContextChangedMsg)
// would leave the old streamID as "current" and the consumer
// couldn't tell stale from fresh.
func (ls *LogStreamer) stopLocked() {
	cancel := ls.cancel
	wg := ls.wg
	lines := ls.lines
	ls.cancel = nil
	ls.wg = nil
	ls.lines = nil

	if cancel == nil {
		return
	}
	// Bump from the process-global source so this Stop's epoch is
	// distinct from every other LogStreamer instance's past or future
	// streamIDs. Any LogLine still buffered in `lines` carries the
	// PRE-bump streamID; the consumer's CurrentStreamID() vs msg.StreamID
	// comparison will mismatch and drop it.
	ls.streamID.Store(streamIDCounter.Add(1))
	cancel()
	if wg != nil {
		wg.Wait()
	}
	if lines != nil {
		close(lines)
	}
}

// CurrentStreamID returns the active stream's epoch — the value
// producers tagged their LogLine emissions with. The Update-side
// LogLineMsg handler compares msg.StreamID against this to drop
// stale buffered residue from a prior stream.
func (ls *LogStreamer) CurrentStreamID() int64 {
	return ls.streamID.Load()
}

// Lines returns the channel for receiving log lines. Returns nil
// before the first Start and after every Stop (the field is nil-ed
// during stopLocked alongside the close). In practice the consumer
// captures the channel at waitForLogLine dispatch time: a stopped
// stream's channel was closed before nil-ing here, so the consumer
// unblocks with !ok cleanly; a not-yet-started streamer returns nil
// and waitForLogLine's nil-channel guard returns a nil Cmd without
// spawning a goroutine.
func (ls *LogStreamer) Lines() <-chan LogLine {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	return ls.lines
}

// podTag returns the Pod field value to emit on each LogLine — empty in
// single-pod mode (implicit identity), populated in aggregate mode.
func (ls *LogStreamer) podTag(podName string) string {
	if ls.aggregate {
		return podName
	}
	return ""
}

// streamContainer sends lines into the supplied channel (captured at
// goroutine spawn — NOT read from ls.lines on each send, so a later
// Stop+Start swapping ls.lines can't cross-contaminate this stream).
// streamID is also captured at spawn so every emitted LogLine carries
// the epoch this goroutine belongs to; the consumer-side handler
// compares against LogStreamer.CurrentStreamID() to drop stale
// residue from a closed channel's buffer.
// Exit pattern: every send is guarded by `<-ctx.Done()` so after
// Stop's cancel() fires the next select picks Done and the goroutine
// returns. Stop() then waits on the wg + closes `lines` — safe
// because all producers have exited by that point.
func (ls *LogStreamer) streamContainer(ctx context.Context, lines chan<- LogLine, streamID int64, podName, namespace, container string) {
	tailLines := int64(100)
	opts := &corev1.PodLogOptions{
		Container: container,
		Follow:    true,
		TailLines: &tailLines,
	}

	req := ls.clientset.CoreV1().Pods(namespace).GetLogs(podName, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		// Send an error line so the user sees something.
		select {
		case lines <- LogLine{StreamID: streamID, Pod: ls.podTag(podName), Container: container, Text: "[error: " + err.Error() + "]"}:
		case <-ctx.Done():
		}
		return
	}
	defer func() {
		_ = stream.Close()
	}()

	scanner := bufio.NewScanner(stream)
	// Increase the scanner buffer for potentially long log lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		case lines <- LogLine{StreamID: streamID, Pod: ls.podTag(podName), Container: container, Text: scanner.Text()}:
		}
	}

	// If the scanner ended due to an error (not EOF / context cancel), report it.
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		select {
		case lines <- LogLine{StreamID: streamID, Pod: ls.podTag(podName), Container: container, Text: "[stream error: " + err.Error() + "]"}:
		case <-ctx.Done():
		}
	}

	// If the stream ended (container terminated) and context is still active,
	// send a marker so the user knows the stream ended.
	if ctx.Err() == nil {
		select {
		case lines <- LogLine{StreamID: streamID, Pod: ls.podTag(podName), Container: container, Text: "[stream ended]"}:
		case <-ctx.Done():
		}
	}
}

// ContainerNames extracts container names (init + regular) from a Pod's Raw field.
// Returns nil if the Raw field is not a *corev1.Pod.
func ContainerNames(raw interface{}) []string {
	pod, ok := raw.(*corev1.Pod)
	if !ok || pod == nil {
		return nil
	}

	var names []string
	for _, c := range pod.Spec.InitContainers {
		names = append(names, c.Name)
	}
	for _, c := range pod.Spec.Containers {
		names = append(names, c.Name)
	}
	return names
}
