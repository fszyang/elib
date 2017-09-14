// Copyright 2016 Platina Systems, Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package loop

import (
	"github.com/platinasystems/go/elib"
	"github.com/platinasystems/go/elib/cpu"
	"github.com/platinasystems/go/elib/elog"

	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

type fromToNode struct {
	toNode   chan struct{}
	fromNode chan bool
}

func (x *fromToNode) init() {
	x.toNode = make(chan struct{}, 1)
	x.fromNode = make(chan bool, 1)
}

func (x *fromToNode) signalNode()       { x.toNode <- struct{}{} }
func (x *fromToNode) waitNode() bool    { return <-x.fromNode }
func (x *fromToNode) signalLoop(v bool) { x.fromNode <- v }
func (x *fromToNode) waitLoop()         { <-x.toNode }

type nodeState struct {
	is_pending bool
	is_polling bool
	nodePollerState
}

var poller_state_strings = [...]string{
	poller_inactive:  "inactive",
	poller_active:    "active",
	poller_suspended: "suspended",
	poller_resumed:   "resumed",
}

func (ns *nodePollerState) String() (s string) {
	_, active, suspend, state := ns.get()
	s = poller_state_strings[state]
	s += fmt.Sprintf(" %d/%d", active, suspend)
	return
}

type nodeAllocPending struct {
	nodeIndex uint
}

type nodeStateMain struct {
	// Protects pending vectors.
	mu sync.Mutex

	// Signals allocations pending to polling loop.
	activePending [2][]nodeAllocPending

	// Low bit of sequence number selects one of 2 activePending vectors.
	// Index sequence&1 is used for new pending adds.
	// Index 1^(sequence&1) is used by getAllocPending to remove pending nodes.
	sequence uint
}

type SuspendLimits struct {
	Suspend, Resume int
}

const poller_panics = true

func (n *Node) addActivity(da, ds int32,
	is_activate, activate_is_active bool,
	lim *SuspendLimits) (was_active, did_suspend, did_resume bool) {
	s := &n.s
	for {
		old_state, active, suspend, state := s.get()
		was_active = active > 0
		if is_activate && was_active != activate_is_active {
			da = int32(1)
			if !activate_is_active {
				da = -1
			}
		}
		active += da
		suspend += ds

		if poller_panics {
			if _, ok := isDataPoller(n.noder); !ok {
				panic("not data poller")
			}
			if active < 0 {
				panic(fmt.Errorf("active < 0 was %d added %d", active-da, da))
			}
			if suspend < 0 {
				panic(fmt.Errorf("suspend < 0 was %d added %d", suspend-ds, ds))
			}
		}

		is_active := active > 0
		is_suspended := false
		if lim != nil {
			slimit := int32(lim.Suspend)
			limit := slimit
			if state == poller_suspended {
				limit = int32(lim.Resume)
			}
			// Be active when suspend count is below resume limit.
			is_active = is_active || suspend <= limit

			// Suspend first time we are over suspend limit.
			did_suspend = state != poller_suspended && ds > 0 && suspend > slimit
			// Back-up so suspend count is never above limit.
			if suspend > slimit {
				suspend -= ds
			}
			did_resume = state == poller_suspended && suspend <= limit
			is_suspended = did_suspend && !did_resume
		}

		need_alloc := is_active && !was_active && state == poller_inactive && !is_suspended

		switch {
		case need_alloc:
			state = poller_active
		case did_suspend:
			state = poller_suspended
		case did_resume:
			state = poller_resumed
		}
		new_state := makeNodePollerState(active, suspend, state)
		if !s.compare_and_swap(old_state, new_state) {
			continue
		}
		if elog.Enabled() {
			kind := poller_elog_kind(poller_elog_data_activity)
			if lim != nil {
				kind = poller_elog_suspend_activity
			}
			n.poller_elog_state(kind, old_state, new_state)
		}

		if was_active != is_active && is_active {
			n.changeActivePollerState(is_active)
		}
		if !need_alloc {
			return
		}
		m := &n.l.nodeStateMain
		// Only take lock if we need to change pending vector.
		m.mu.Lock()
		if !s.is_pending {
			s.is_pending = true
			i := m.sequence & 1
			ap := nodeAllocPending{
				nodeIndex: n.index,
			}
			n.poller_elog(poller_elog_alloc_pending)
			m.activePending[i] = append(m.activePending[i], ap)
		}
		m.mu.Unlock()
		return
	}
}

func (n *Node) changeActivePollerState(is_active bool) {
	if _, eventWait := n.l.activePollerState.changeActive(is_active); eventWait {
		n.poller_elog(poller_elog_event_wake)
		n.l.Interrupt()
	}
}

func (n *Node) AddDataActivity(i int) { n.addActivity(int32(i), 0, false, false, nil) }
func (n *Node) Activate(enable bool) (was bool) {
	was, _, _ = n.addActivity(0, 0, true, enable, nil)
	return
}
func (n *Node) IsActive() bool {
	_, active, _, _ := n.s.get()
	return active > 0
}

func (m *nodeStateMain) getAllocPending(l *Loop) (pending []nodeAllocPending) {
	m.mu.Lock()
	i0 := m.sequence & 1
	i1 := i0 ^ 1

	// Reset pending for next sequence.
	if m.activePending[i1] != nil {
		m.activePending[i1] = m.activePending[i1][:0]
	}
	pending = m.activePending[i0]
	// Clear pending state while we still have lock.
	for _, p := range pending {
		n := l.nodes[p.nodeIndex]
		n.s.is_pending = false
	}
	m.sequence++
	m.mu.Unlock()
	return
}

type activateEvent struct{ n *Node }

func (e *activateEvent) EventAction()   { e.n.Activate(true) }
func (e *activateEvent) String() string { return fmt.Sprintf("activate %s", e.n.name) }

func (n *Node) ActivateAfter(secs float64) {
	if was := n.Activate(false); was {
		n.e.activateEvent.n = n
		le := n.l.getLoopEvent(&n.e.activateEvent, nil, elog.PointerToFirstArg(&n))
		n.l.signalEventAfter(le, secs)
	}
}

func (in *In) getPoller(l *Loop) (a *activePoller, n *Node) {
	p := &l.activePollerPool
	a = p.entries[in.activeIndex]
	if poller_panics {
		if p.IsFree(uint(in.activeIndex)) {
			panic(fmt.Errorf("reference of freed active poller: %v", in))
		}
		if i0, i1 := uint(in.pollerNodeIndex), a.pollerNode.index; i0 != i1 {
			n0, n1 := l.GetNode(i0), l.GetNode(i1)
			panic(fmt.Errorf("active poller node mismatch %s %d != %s %d", n0.Name(), i0, n1.Name(), i1))
		}
	}
	n = a.pollerNode
	return
}

func (l *Loop) AddSuspendActivity(in *In, i int, lim *SuspendLimits) {
	var did_suspend, did_resume bool
	a, n := in.getPoller(l)
	// Loop until add activity succeeds.
	for {
		_, did_suspend, did_resume = n.addActivity(0, int32(i), false, false, lim)
		if !did_suspend {
			break
		}

		// Signal polling done to main loop.
		n.inputStats.current.suspends++
		n.poller_elog(poller_elog_suspended)
		if poll_active {
			a.toLoop <- struct{}{}
		} else {
			n.ft.signalLoop(false)
		}
		// Wait for continue (resume) signal from main loop.
		t0 := cpu.TimeNow()
		if poll_active {
			<-a.fromLoop
		} else {
			n.ft.waitLoop()
		}
		// Don't charge node for time suspended.
		// Reduce from output side since its tx that suspends not rx.
		dt := cpu.TimeNow() - t0
		n.outputStats.current.clocks -= uint64(dt)
		n.poller_elog(poller_elog_resumed)
	}
	if did_resume {
		n.poller_elog(poller_elog_resume_wait)
	}
	return
}

func (l *Loop) Suspend(in *In, lim *SuspendLimits) { l.AddSuspendActivity(in, 1, lim) }
func (l *Loop) Resume(in *In, lim *SuspendLimits)  { l.AddSuspendActivity(in, -1, lim) }

type nodePollerState uint64

const (
	poller_inactive = iota
	poller_active
	poller_suspended
	poller_resumed
)

const countMask = 1<<31 - 1

func (ns *nodePollerState) get() (x nodePollerState, active, suspend int32, state int) {
	x = nodePollerState(atomic.LoadUint64((*uint64)(ns)))
	state = int(x) & 3
	active = int32(x>>(2+31*0)) & countMask
	suspend = int32(x>>(2+31*1)) & countMask
	return
}

func makeNodePollerState(active, suspend int32, state int) (s nodePollerState) {
	if active < 0 {
		panic("active count < 0")
	}
	if suspend < 0 {
		panic("suspend count < 0")
	}
	s = nodePollerState(state)
	s |= nodePollerState(active&countMask) << (2 + 31*0)
	s |= nodePollerState(suspend&countMask) << (2 + 31*1)
	return
}

func (s *nodePollerState) compare_and_swap(old, new nodePollerState) (swapped bool) {
	return atomic.CompareAndSwapUint64((*uint64)(s), uint64(old), uint64(new))
}

type activePollerState uint32

func (s *activePollerState) compare_and_swap(old, new activePollerState) (swapped bool) {
	return atomic.CompareAndSwapUint32((*uint32)(s), uint32(old), uint32(new))
}
func (s *activePollerState) get() (x activePollerState, nActive uint, eventWait bool) {
	x = activePollerState(atomic.LoadUint32((*uint32)(s)))
	eventWait = x&1 != 0
	nActive = uint(x >> 1)
	return
}
func makeActivePollerState(nActive uint, eventWait bool) (s activePollerState) {
	s = activePollerState(nActive << 1)
	if eventWait {
		s |= 1
	}
	return
}
func (s *activePollerState) setEventWait() (nActive uint, wait bool) {
	var old activePollerState
	if old, nActive, wait = s.get(); nActive == 0 {
		wantWait := true
		new := makeActivePollerState(nActive, wantWait)
		if !s.compare_and_swap(old, new) {
			return
		}
		wait = wantWait
	}
	return
}
func (s *activePollerState) clearEventWait() {
	old, nActive, wait := s.get()
	for wait {
		new := makeActivePollerState(nActive, false)
		if s.compare_and_swap(old, new) {
			break
		}
		old, nActive, wait = s.get()
	}
}

func (s *activePollerState) changeActive(isActive bool) (uint, bool) {
	for {
		old, n, w := s.get()
		if isActive {
			n += 1
		} else {
			if n == 0 {
				panic("negative active count")
			}
			n -= 1
		}
		new := makeActivePollerState(n, w && n == 0)
		if s.compare_and_swap(old, new) {
			return n, w
		}
	}
}

func (n *Node) getActivePoller() *activePoller {
	return n.l.activePollerPool.entries[n.activePollerIndex]
}

func (n *Node) allocActivePoller() {
	p := &n.l.activePollerPool
	if !p.IsFree(n.activePollerIndex) {
		panic("already allocated")
	}
	i := p.GetIndex()
	a := p.entries[i]
	create := a == nil
	if create {
		a = &activePoller{}
		p.entries[i] = a
	}
	a.index = uint16(i)
	n.activePollerIndex = i
	a.pollerNode = n
	n.poller_elog_i(poller_elog_alloc_poller, i, p.Elts())
	if create {
		a.initActiveNodes(n.l)
	}
	if poll_active {
		a.fromLoop = make(chan inLooper, 1)
		a.toLoop = make(chan struct{}, 1)
		go a.dataPoll(n.l)
	}
}

func (n *Node) freeActivePoller() {
	a := n.getActivePoller()
	a.flushActivePollerStats(n.l)
	a.pollerNode = nil
	i := n.activePollerIndex
	p := &n.l.activePollerPool
	p.PutIndex(i)
	n.activePollerIndex = ^uint(0)
	n.poller_elog_i(poller_elog_free_poller, i, p.Elts())
	if poll_active {
		// Shut down active poller.
		close(a.fromLoop)
		close(a.toLoop)
		a.fromLoop = nil
		a.toLoop = nil
	}
}

func (n *Node) maybeFreeActive() {
	for {
		old_state, active, suspend, state := n.s.get()
		switch state {
		case poller_inactive:
		case poller_suspended:
			return
		}
		if active != 0 || suspend != 0 {
			return
		}
		new_state := makeNodePollerState(0, 0, poller_inactive)
		if !n.s.compare_and_swap(old_state, new_state) {
			continue
		}
		n.freeActivePoller()
		return
	}
}

func (a *activePoller) flushActivePollerStats(l *Loop) {
	for i := range a.activeNodes {
		an := &a.activeNodes[i]
		n := l.nodes[an.index]

		n.inputStats.current.add_raw(&an.inputStats)
		an.inputStats.zero()

		n.outputStats.current.add_raw(&an.outputStats)
		an.outputStats.zero()
	}
}

func (l *Loop) flushAllActivePollerStats() {
	m := &l.nodeStateMain
	m.mu.Lock()
	defer m.mu.Unlock()
	p := &l.activePollerPool
	for i := uint(0); i < p.Len(); i++ {
		if !p.IsFree(i) {
			p.entries[i].flushActivePollerStats(l)
		}
	}
}

// FIXME make poll_active = true will be default.
const poll_active = false

func (a *activePoller) dataPoll(l *Loop) {
	if false {
		runtime.LockOSThread()
	}

	// Save elog if thread panics.
	defer func() {
		if elog.Enabled() {
			if err := recover(); err != nil {
				elog.Panic(fmt.Errorf("poller%d: %v", a.index, err))
				panic(err)
			}
		}
	}()
	for p := range a.fromLoop {
		n := p.GetNode()
		an := &a.activeNodes[n.index]
		a.currentNode = an
		t0 := cpu.TimeNow()
		a.timeNow = t0
		p.LoopInput(l, an.looperOut)
		nVec := an.out.call(l, a)
		a.pollerStats.update(nVec, t0)
		l.pollerStats.update(nVec)
		a.toLoop <- struct{}{}
	}
}

func (l *Loop) dataPoll(p inLooper) {
	n := p.GetNode()
	// Save elog if thread panics.
	defer func() {
		if elog.Enabled() {
			if err := recover(); err != nil {
				elog.Panic(fmt.Errorf("%s: %v", n.name, err))
				panic(err)
			}
		}
	}()
	for {
		n.poller_elog(poller_elog_node_wait)
		n.ft.waitLoop()
		n.poller_elog(poller_elog_node_wake)
		ap := n.getActivePoller()
		an := &ap.activeNodes[n.index]
		ap.currentNode = an
		t0 := cpu.TimeNow()
		ap.timeNow = t0
		p.LoopInput(l, an.looperOut)
		nVec := an.out.call(l, ap)
		ap.pollerStats.update(nVec, t0)
		l.pollerStats.update(nVec)
		n.poller_elog(poller_elog_node_signal)
		n.ft.signalLoop(true)
	}
}

func (l *Loop) doPollers() {
	pending := l.nodeStateMain.getAllocPending(l)
	for _, p := range pending {
		n := l.nodes[p.nodeIndex]
		n.allocActivePoller()
	}

	p := &l.activePollerPool
	for i := uint(0); i < p.Len(); i++ {
		if p.IsFree(i) {
			continue
		}
		a := p.entries[i]
		n := a.pollerNode

		_, active, _, state := n.s.get()
		n.s.is_polling = (active > 0 || state == poller_resumed) && state != poller_suspended
		if !n.s.is_polling {
			continue
		}
		n.poller_elog(poller_elog_poll)

		// Start poller who will be blocked waiting on fromLoop.
		if poll_active {
			a.fromLoop <- n.noder.(inLooper)
		} else {
			n.ft.signalNode()
		}
	}

	// Wait for pollers to finish.
	for i := uint(0); i < p.Len(); i++ {
		if p.IsFree(i) {
			continue
		}
		a := p.entries[i]
		n := a.pollerNode
		done := false
		if n.s.is_polling {
			n.poller_elog(poller_elog_wait)
			if poll_active {
				<-a.toLoop
			} else {
				done = n.ft.waitNode()
			}
			n.poller_elog(poller_elog_wait_done)
		}
		if done {
			n.maybeFreeActive()
		}
	}

	if l.activePollerPool.Elts() == 0 {
		l.resetPollerStats()
	} else {
		l.doPollerStats()
	}
}

const (
	poller_elog_alloc_poller = iota
	poller_elog_free_poller
	poller_elog_alloc_pending
	poller_elog_event_wake
	poller_elog_poll
	poller_elog_wait
	poller_elog_wait_done
	poller_elog_suspended
	poller_elog_resume_wait
	poller_elog_resumed
	poller_elog_data_activity
	poller_elog_suspend_activity
	poller_elog_node_wait
	poller_elog_node_wake
	poller_elog_node_signal
)

type poller_elog_kind uint32

func (k poller_elog_kind) String() string {
	t := [...]string{
		poller_elog_alloc_poller:     "alloc-poller",
		poller_elog_free_poller:      "free-poller",
		poller_elog_alloc_pending:    "alloc-pending",
		poller_elog_event_wake:       "event-wake",
		poller_elog_poll:             "wake-node",
		poller_elog_wait:             "wait",
		poller_elog_wait_done:        "wait-done",
		poller_elog_suspended:        "suspended",
		poller_elog_resume_wait:      "resume-wait",
		poller_elog_resumed:          "resumed",
		poller_elog_data_activity:    "add-data",
		poller_elog_suspend_activity: "add-suspend",
		poller_elog_node_wait:        "node-wait",
		poller_elog_node_wake:        "node-awake",
		poller_elog_node_signal:      "node-signal",
	}
	return elib.StringerHex(t[:], int(k))
}

type poller_elog struct {
	name elog.StringRef
	kind poller_elog_kind
	// Old and new state.
	old, new nodePollerState
	da, a    int32
}

func (n *Node) poller_elog_i(kind poller_elog_kind, i, elts uint) {
	e := poller_elog{
		name: n.elogNodeName,
		kind: kind,
		a:    int32(i),
		da:   int32(elts),
	}
	elog.Add(&e)
}

func (n *Node) poller_elog_state(kind poller_elog_kind, old, new nodePollerState) {
	e := poller_elog{
		name: n.elogNodeName,
		kind: kind,
		old:  old,
		new:  new,
	}
	elog.Add(&e)
}

func (n *Node) poller_elog(kind poller_elog_kind) {
	e := poller_elog{
		name: n.elogNodeName,
		kind: kind,
	}
	elog.Add(&e)
}

func (e *poller_elog) Elog(l *elog.Log) {
	switch e.kind {
	case poller_elog_alloc_poller, poller_elog_free_poller:
		l.Logf("loop %s %v %d/%d", e.kind, e.name, e.a, e.da)
	case poller_elog_data_activity, poller_elog_suspend_activity:
		l.Logf("loop %s %v %v -> %v", e.kind, e.name, &e.old, &e.new)
	default:
		l.Logf("loop %s %v", e.kind, e.name)
	}
}
