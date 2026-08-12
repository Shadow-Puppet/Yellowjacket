package autotagservice

import (
	"context"
	"strings"

	"yellowjacket/backend/jobs"
)

// applyJobPrefix namespaces autotag apply jobs in the shared registry.
const applyJobPrefix = "autotag:"

// SetJobRegistry wires the background job registry so an apply reports
// progress and offers a cancel like every other long-running operation.
//
// Before this, apply was a bare goroutine whose progress lived in a
// component field that navigation discarded, with no cancel and no
// record of where it stopped (errors.C3). Everything routed through the
// registry gets progress, cancel and the global indicator for free; the
// three subsystems that lacked them were the three that were not
// registered.
func (s *Service) SetJobRegistry(reg *jobs.Registry) {
	s.mu.Lock()
	s.jobsReg = reg
	s.mu.Unlock()
}

// applyJobID is the registry ID for one folder's apply.
func applyJobID(groupKey string) string {
	return applyJobPrefix + groupKey
}

// WritesInFlight reports whether an apply is currently rewriting tags
// on disk. Quitting mid-apply leaves a folder half-retagged, so the app
// asks before closing (errors.p4).
func (s *Service) WritesInFlight() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.runningApplies) > 0
}

// startApplyJob registers the job and returns the handle plus a context
// the user's Cancel button can stop. A nil registry (tests, and the
// window before wiring) degrades to the plain context.
func (s *Service) startApplyJob(
	groupKey string,
	total int,
) (*jobs.Handle, context.Context, context.CancelFunc) {
	s.mu.Lock()
	parent := s.ctx
	reg := s.jobsReg
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(parent)

	if reg == nil {
		return nil, ctx, cancel
	}

	handle := reg.Start(jobs.Spec{
		ID:       applyJobID(groupKey),
		Kind:     jobs.KindAutotagApply,
		Title:    "Writing tags",
		Subtitle: folderLabel(groupKey),
		Total:    int64(total),
		Caps:     jobs.Caps{Cancellable: true},
		Controls: jobs.Controls{Cancel: cancel},
	})

	return handle, ctx, cancel
}

// folderLabel is the part of a group key worth showing: the folder,
// not the whole path, which is usually wider than the row.
func folderLabel(groupKey string) string {
	trimmed := strings.TrimRight(groupKey, "/")

	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		return trimmed[idx+1:]
	}

	return trimmed
}
