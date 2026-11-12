package model

import (
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/bjarneo/cliamp/ipc"
)

// SystemVolume controls the output volume of the operating system, which
// the phone remote shows beside cliamp's own volume. package main passes
// internal/osvolume. Its calls run a command or a system API and can take
// tens of milliseconds, so the Model makes them only inside commands.
type SystemVolume interface {
	Get() (level float64, muted bool, err error) // level is 0 to 1
	Set(level float64) error
	SetMuted(muted bool) error
}

// systemVolumePollInterval is how often the Model reads the system volume
// while an IPC client subscribes, so a change made on the computer reaches
// the phone.
const systemVolumePollInterval = 3 * time.Second

// systemVolumeState is the last system volume the Model read.
type systemVolumeState struct {
	backend SystemVolume
	known   bool
	percent int
	muted   bool
	// seq counts the changes the Model started. A read that started before
	// the latest change is stale and does not apply.
	seq uint64
}

// systemVolumeMsg carries the result of a system volume read, or of a
// change and the read after it. jobID is set for a change.
type systemVolumeMsg struct {
	seq     uint64
	poll    bool
	percent int
	muted   bool
	err     error
	jobs    *ipc.JobStore
	jobID   string
}

// systemVolumePollMsg starts the next poll read.
type systemVolumePollMsg struct{}

// SetSystemVolume enables the system.volume and system.mute operations and
// the system volume of the runtime snapshot.
func (m *Model) SetSystemVolume(backend SystemVolume) {
	m.sysVolume = systemVolumeState{backend: backend}
}

func toPercent(level float64) int { return int(math.Round(level * 100)) }

// readSystemVolumeCmd reads the system volume. A poll read schedules the
// next one when its message arrives.
func readSystemVolumeCmd(backend SystemVolume, seq uint64, poll bool) tea.Cmd {
	return func() tea.Msg {
		level, muted, err := backend.Get()
		return systemVolumeMsg{seq: seq, poll: poll, percent: toPercent(level), muted: muted, err: err}
	}
}

func systemVolumePollCmd() tea.Cmd {
	return teaTick(systemVolumePollInterval, func(time.Time) tea.Msg { return systemVolumePollMsg{} })
}

// pollSystemVolume reads the system volume when an IPC client listens for
// changes, and otherwise waits for the next poll.
func (m *Model) pollSystemVolume() tea.Cmd {
	if m.sysVolume.backend == nil {
		return nil
	}
	if m.ipcRuntime == nil || m.ipcRuntime.broker == nil || m.ipcRuntime.broker.Subscribers() == 0 {
		return systemVolumePollCmd()
	}
	return readSystemVolumeCmd(m.sysVolume.backend, m.sysVolume.seq, true)
}

// handleSystemVolume records a system volume result and finishes its job.
func (m *Model) handleSystemVolume(msg systemVolumeMsg) tea.Cmd {
	if msg.err == nil && msg.seq == m.sysVolume.seq {
		m.sysVolume.known = true
		m.sysVolume.percent = msg.percent
		m.sysVolume.muted = msg.muted
	}
	if msg.jobID != "" {
		if msg.err != nil {
			err := v2InternalError()
			err.Detail = msg.err.Error()
			m.failV2Job(msg.jobs, msg.jobID, err)
		} else {
			percent, muted := msg.percent, msg.muted
			m.completeV2Job(msg.jobs, msg.jobID, ipc.Response{OK: true, SystemVolume: &percent, SystemMuted: &muted})
		}
	}
	if msg.poll {
		return systemVolumePollCmd()
	}
	return nil
}

// handleV2SystemVolume runs system.volume, with value from 0 to 100, or
// system.mute, with name on, off or toggle, off the event loop.
func (m *Model) handleV2SystemVolume(jobs *ipc.JobStore, jobID string, request ipc.Request) tea.Cmd {
	backend := m.sysVolume.backend
	if backend == nil {
		m.failV2Job(jobs, jobID, v2UnavailableError())
		return nil
	}
	var apply func() error
	switch request.Cmd {
	case "system.volume":
		level := min(max(request.Value, 0), 100) / 100
		apply = func() error { return backend.Set(level) }
	case "system.mute":
		muted, ok := switchValue(strings.ToLower(request.Name), m.sysVolume.muted)
		if !ok {
			m.failV2Job(jobs, jobID, v2InvalidParamsError())
			return nil
		}
		apply = func() error { return backend.SetMuted(muted) }
	}
	m.sysVolume.seq++
	seq := m.sysVolume.seq
	return func() tea.Msg {
		msg := systemVolumeMsg{seq: seq, jobs: jobs, jobID: jobID}
		if msg.err = apply(); msg.err != nil {
			return msg
		}
		var level float64
		level, msg.muted, msg.err = backend.Get()
		msg.percent = toPercent(level)
		return msg
	}
}
