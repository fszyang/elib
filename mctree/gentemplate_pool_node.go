// autogenerated: do not edit!
// generated from gentemplate [gentemplate -d Package=mctree -id node -d PoolType=node_pool -d Type=node -d Data=nodes github.com/platinasystems/elib/pool.tmpl]

// Copyright 2016 Platina Systems, Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mctree

import (
	"github.com/platinasystems/elib"
)

type node_pool struct {
	elib.Pool
	nodes []node
}

func (p *node_pool) GetIndex() (i uint) {
	l := uint(len(p.nodes))
	i = p.Pool.GetIndex(l)
	if i >= l {
		p.Validate(i)
	}
	return i
}

func (p *node_pool) PutIndex(i uint) (ok bool) {
	return p.Pool.PutIndex(i)
}

func (p *node_pool) IsFree(i uint) (v bool) {
	v = i >= uint(len(p.nodes))
	if !v {
		v = p.Pool.IsFree(i)
	}
	return
}

func (p *node_pool) Resize(n uint) {
	c := uint(cap(p.nodes))
	l := uint(len(p.nodes) + int(n))
	if l > c {
		c = elib.NextResizeCap(l)
		q := make([]node, l, c)
		copy(q, p.nodes)
		p.nodes = q
	}
	p.nodes = p.nodes[:l]
}

func (p *node_pool) Validate(i uint) {
	c := uint(cap(p.nodes))
	l := uint(i) + 1
	if l > c {
		c = elib.NextResizeCap(l)
		q := make([]node, l, c)
		copy(q, p.nodes)
		p.nodes = q
	}
	if l > uint(len(p.nodes)) {
		p.nodes = p.nodes[:l]
	}
}

func (p *node_pool) Elts() uint {
	return uint(len(p.nodes)) - p.FreeLen()
}

func (p *node_pool) Len() uint {
	return uint(len(p.nodes))
}

func (p *node_pool) Foreach(f func(x node)) {
	for i := range p.nodes {
		if !p.Pool.IsFree(uint(i)) {
			f(p.nodes[i])
		}
	}
}

func (p *node_pool) ForeachIndex(f func(i uint)) {
	for i := range p.nodes {
		if !p.Pool.IsFree(uint(i)) {
			f(uint(i))
		}
	}
}

func (p *node_pool) Reset() {
	p.Pool.Reset()
	if len(p.nodes) > 0 {
		p.nodes = p.nodes[:0]
	}
}
