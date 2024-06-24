// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modload

import (
	"fmt"
	"golang.org/x/mod/module"
)

type perPruning[T any] struct {
	pruned   T
	unpruned T
}

func (pp perPruning[T]) from(p modPruning) T {
	if p == unpruned {
		return pp.unpruned
	}
	return pp.pruned
}

// A dqTracker tracks and propagates the reason that each module version
// cannot be included in the module graph.
type dqTracker struct {
	// extendedRootPruning is the modPruning given the go.mod file for each root
	// in the extended module graph.
	extendedRootPruning map[module.Version]modPruning

	// dqReason records whether and why each each encountered version is
	// disqualified in a pruned or unpruned context.
	dqReason map[module.Version]perPruning[dqState]

	// requiring maps each not-yet-disqualified module version to the versions
	// that would cause that module's requirements to be included in a pruned or
	// unpruned context. If that version becomes disqualified, the
	// disqualification will be propagated to all of the versions in the
	// corresponding list.
	//
	// This map is similar to the module requirement graph, but includes more
	// detail about whether a given dependency edge appears in a pruned or
	// unpruned context. (Other commands do not need this level of detail.)
	requiring map[module.Version][]module.Version
}

// A dqState indicates whether and why a module version is “disqualified” from
// being used in a way that would incorporate its requirements.
//
// The zero dqState indicates that the module version is not known to be
// disqualified, either because it is ok or because we are currently traversing
// a cycle that includes it.
type dqState struct {
	err error          // if non-nil, disqualified because the requirements of the module could not be read
	dep module.Version // disqualified because the module is or requires dep
}

func (dq dqState) isDisqualified() bool {
	return dq != dqState{}
}

func (dq dqState) String() string {
	if dq.err != nil {
		return dq.err.Error()
	}
	if dq.dep != (module.Version{}) {
		return dq.dep.String()
	}
	return "(no conflict)"
}

// require records that m directly requires r, in case r becomes disqualified.
// (These edges are in the opposite direction from the edges in an mvs.Graph.)
//
// If r is already disqualified, require propagates the disqualification to m
// and returns the reason for the disqualification.
func (t *dqTracker) require(m, r module.Version) (ok bool) {
	rdq := t.dqReason[r]
	rootPruning, isRoot := t.extendedRootPruning[r]
	if isRoot && rdq.from(rootPruning).isDisqualified() {
		// When we pull in m's dependencies, we will have an edge from m to r, and r
		// is disqualified (it is a root, which causes its problematic dependencies
		// to always be included). So we cannot pull in m's dependencies at all:
		// m is completely disqualified.
		t.disqualify(m, pruned, dqState{dep: r})
		return false
	}

	if dq := rdq.from(unpruned); dq.isDisqualified() {
		t.disqualify(m, unpruned, dqState{dep: r})
		if _, ok := t.extendedRootPruning[m]; !ok {
			// Since m is not a root, its dependencies can't be included in the pruned
			// part of the module graph, and will never be disqualified from a pruned
			// reason. We've already disqualified everything that matters.
			return false
		}
	}

	// Record that m is a dependant of r, so that if r is later disqualified
	// m will be disqualified as well.
	if t.requiring == nil {
		t.requiring = make(map[module.Version][]module.Version)
	}
	t.requiring[r] = append(t.requiring[r], m)
	return true
}

// disqualify records why the dependencies of m cannot be included in the module
// graph if reached from a part of the graph with the given pruning.
//
// Since the pruned graph is a subgraph of the unpruned graph, disqualifying a
// module from a pruned part of the graph also disqualifies it in the unpruned
// parts.
func (t *dqTracker) disqualify(m module.Version, fromPruning modPruning, reason dqState) {
	if !reason.isDisqualified() {
		panic("internal error: disqualify called with a non-disqualifying dqState")
	}

	dq := t.dqReason[m]
	if dq.from(fromPruning).isDisqualified() {
		return // Already disqualified for some other reason; don't overwrite it.
	}
	rootPruning, isRoot := t.extendedRootPruning[m]
	if fromPruning == pruned {
		dq.pruned = reason
		if !dq.unpruned.isDisqualified() {
			// Since the pruned graph of m is a subgraph of the unpruned graph, if it
			// is disqualified due to something in the pruned graph, it is certainly
			// disqualified in the unpruned graph from the same reason.
			dq.unpruned = reason
		}
	} else {
		dq.unpruned = reason
		if dq.pruned.isDisqualified() {
			panic(fmt.Sprintf("internal error: %v is marked as disqualified when pruned, but not when unpruned", m))
		}
		if isRoot && rootPruning == unpruned {
			// Since m is a root that is always unpruned, any other roots — even
			// pruned ones! — that cause it to be selected would also cause the reason
			// for is disqualification to be included in the module graph.
			dq.pruned = reason
		}
	}
	if t.dqReason == nil {
		t.dqReason = make(map[module.Version]perPruning[dqState])
	}
	t.dqReason[m] = dq

	if isRoot && (fromPruning == pruned || rootPruning == unpruned) {
		// Either m is disqualified even when its dependencies are pruned,
		// or m's go.mod file causes its dependencies to *always* be unpruned.
		// Everything that depends on it must be disqualified.
		for _, p := range t.requiring[m] {
			t.disqualify(p, pruned, dqState{dep: m})
			// Note that since the pruned graph is a subset of the unpruned graph,
			// disqualifying p in the pruned graph also disqualifies it in the
			// unpruned graph.
		}
		// Everything in t.requiring[m] is now fully disqualified.
		// We won't need to use it again.
		delete(t.requiring, m)
		return
	}

	// Either m is not a root, or it is a pruned root but only being disqualified
	// when reached from the unpruned parts of the module graph.
	// Either way, the reason for this disqualification is only visible to the
	// unpruned parts of the module graph.
	for _, p := range t.requiring[m] {
		t.disqualify(p, unpruned, dqState{dep: m})
	}
	if !isRoot {
		// Since m is not a root, its dependencies can't be included in the pruned
		// part of the module graph, and will never be disqualified from a pruned
		// reason. We've already disqualified everything that matters.
		delete(t.requiring, m)
	}
}

// check reports whether m is disqualified in the given pruning context.
func (t *dqTracker) check(m module.Version, pruning modPruning) dqState {
	return t.dqReason[m].from(pruning)
}

// path returns the path from m to the reason it is disqualified, which may be
// either a module that violates constraints or an error in loading
// requirements.
//
// If m is not disqualified, path returns (nil, nil).
func (t *dqTracker) path(m module.Version, pruning modPruning) (path []module.Version, err error) {
	for {
		dq := t.dqReason[m].from(pruning)
		if !dq.isDisqualified() {
			return path, nil
		}
		path = append(path, m)
		if dq.err != nil || dq.dep == m {
			return path, dq.err // m itself is the conflict.
		}
		m = dq.dep
	}
}
