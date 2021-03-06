// autogenerated: do not edit!
// generated from gentemplate [gentemplate -d Package=mctree -id level -d VecType=level_vec -d Type=level github.com/platinasystems/elib/vec.tmpl]

// Copyright 2016 Platina Systems, Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mctree

import (
	"github.com/platinasystems/elib"
)

type level_vec []level

func (p *level_vec) Resize(n uint) {
	old_cap := uint(cap(*p))
	new_len := uint(len(*p)) + n
	if new_len > old_cap {
		new_cap := elib.NextResizeCap(new_len)
		q := make([]level, new_len, new_cap)
		copy(q, *p)
		*p = q
	}
	*p = (*p)[:new_len]
}

func (p *level_vec) validate(new_len uint, zero level) *level {
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

func (p *level_vec) validateSlowPath(zero level, old_cap, new_len, old_len uint) *level {
	if new_len > old_cap {
		new_cap := elib.NextResizeCap(new_len)
		q := make([]level, new_cap, new_cap)
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

func (p *level_vec) Validate(i uint) *level {
	var zero level
	return p.validate(i+1, zero)
}

func (p *level_vec) ValidateInit(i uint, zero level) *level {
	return p.validate(i+1, zero)
}

func (p *level_vec) ValidateLen(l uint) (v *level) {
	if l > 0 {
		var zero level
		v = p.validate(l, zero)
	}
	return
}

func (p *level_vec) ValidateLenInit(l uint, zero level) (v *level) {
	if l > 0 {
		v = p.validate(l, zero)
	}
	return
}

func (p *level_vec) ResetLen() {
	if *p != nil {
		*p = (*p)[:0]
	}
}

func (p level_vec) Len() uint { return uint(len(p)) }
