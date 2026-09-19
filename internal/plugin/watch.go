package plugin

import (
	"context"
	"reflect"
	"time"

	"github.com/patriceckhart/hrdx/internal/state"
)

// WatchApprovals revokes changed approvals, but never grants additional rights
// to a live instance. New approvals are loaded only on a fresh TUI startup.
// Revocation is observed within one polling interval plus local filesystem I/O.
func (r *Runtime) WatchApprovals(base string) {
	r.mu.Lock()
	if r.closing || r.watchCancel != nil {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(r.ctx)
	r.approvalBase = base
	r.watchCancel = cancel
	r.wg.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			for _, entry := range r.Registrations() {
				if !entry.Approval.Enabled {
					continue
				}
				approval, err := state.LoadPluginApproval(ApprovalPath(base, entry.Package.ID))
				if err == nil && reflect.DeepEqual(approval, entry.Approval) {
					continue
				}
				r.revoke(entry.Package.ID)
			}
		}
	}()
}

func (r *Runtime) revoke(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[id]
	if !ok {
		return
	}
	entry.Approval.Enabled = false
	r.entries[id] = entry
	status := r.statuses[id]
	status.State, status.Error = "blocked", "approval changed or revoked, restart hrdx to load current approvals"
	r.statuses[id] = status
	if session := r.sessions[id]; session != nil {
		session.stopOnce.Do(func() { close(session.stop) })
	}
	select {
	case r.events <- Event{Status: &status}:
	default:
	}
}
