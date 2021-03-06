// autogenerated: do not edit!
// generated from gentemplate [gentemplate -d Package=elog -id Event -d VecType=bufferEventVec -d Type=bufferEvent github.com/platinasystems/elib/vec.tmpl]

// Copyright 2016 Platina Systems, Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package elog

import (
	"github.com/platinasystems/elib"
)

type bufferEventVec []bufferEvent

func (p *bufferEventVec) Resize(n uint) {
	old_cap := uint(cap(*p))
	new_len := uint(len(*p)) + n
	if new_len > old_cap {
		new_cap := elib.NextResizeCap(new_len)
		q := make([]bufferEvent, new_len, new_cap)
		copy(q, *p)
		*p = q
	}
	*p = (*p)[:new_len]
}

func (p *bufferEventVec) validate(new_len uint, zero bufferEvent) *bufferEvent {
	old_cap := uint(cap(*p))
	old_len := uint(len(*p))
	if new_len <= old_cap {
		// Need to reslice to larger length?
		if new_len > old_len {
			*p = (*p)[:new_len]
			for i := old_len; i < new_len; i++ {
				(*p)[i] = zero
			}
		}
		return &(*p)[new_len-1]
	}
	return p.validateSlowPath(zero, old_cap, new_len, old_len)
}

func (p *bufferEventVec) validateSlowPath(zero bufferEvent, old_cap, new_len, old_len uint) *bufferEvent {
	if new_len > old_cap {
		new_cap := elib.NextResizeCap(new_len)
		q := make([]bufferEvent, new_cap, new_cap)
		copy(q, *p)
		for i := old_len; i < new_cap; i++ {
			q[i] = zero
		}
		*p = q[:new_len]
	}
	if new_len > old_len {
		*p = (*p)[:new_len]
	}
	return &(*p)[new_len-1]
}

func (p *bufferEventVec) Validate(i uint) *bufferEvent {
	var zero bufferEvent
	return p.validate(i+1, zero)
}

func (p *bufferEventVec) ValidateInit(i uint, zero bufferEvent) *bufferEvent {
	return p.validate(i+1, zero)
}

func (p *bufferEventVec) ValidateLen(l uint) (v *bufferEvent) {
	if l > 0 {
		var zero bufferEvent
		v = p.validate(l, zero)
	}
	return
}

func (p *bufferEventVec) ValidateLenInit(l uint, zero bufferEvent) (v *bufferEvent) {
	if l > 0 {
		v = p.validate(l, zero)
	}
	return
}

func (p *bufferEventVec) ResetLen() {
	if *p != nil {
		*p = (*p)[:0]
	}
}

func (p bufferEventVec) Len() uint { return uint(len(p)) }
