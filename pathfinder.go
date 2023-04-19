package main

import (
	"context"
	"runtime"
	"sync"

	"golang.org/x/mod/module"

	"github.com/CrowdStrike/perseus/perseusapi"
)

// newPathFinder initializes and returns a new [pathFinder] instance using the provided Perseus
// client, maximum depth, and status callback.
func newPathFinder(c perseusapi.PerseusServiceClient, maxDepth int, status func(string)) pathFinder {
	return pathFinder{
		c:        c,
		maxDepth: maxDepth,
		status:   status,
	}
}

// pathFinder queries the Perseus database to contruct dependency paths of up to maxDepth steps between
// two modules.
type pathFinder struct {
	c        perseusapi.PerseusServiceClient
	status   func(string)
	maxDepth int

	sem chan struct{}
	wg  *sync.WaitGroup
}

// pathFinderResult defines the result items produced by [pathFinder.findPathsBetween].  Each result
// contains either a slice of [module.Version] instances representing a path between the specified modules
// or an error.
type pathFinderResult struct {
	path []module.Version
	err  error
}

// findPathsBetween repeatedly queries the Perseus server to find one or more dependency paths between
// the two specified modules.
func (pf *pathFinder) findPathsBetween(ctx context.Context, from, to module.Version) chan pathFinderResult {
	// semaphore to limit concurrency to the number of available CPUs
	n := runtime.NumCPU()
	pf.sem = make(chan struct{}, n)
	for i := 0; i < n; i++ {
		pf.sem <- struct{}{}
	}
	// wait group to monitor outstanding async tasks
	pf.wg = &sync.WaitGroup{}

	results := make(chan pathFinderResult)
	pf.wg.Add(1)
	go func() {
		defer func() {
			pf.wg.Done()
			pf.wg.Wait()
			close(results)
			close(pf.sem)
		}()
		pf.xxx(ctx, []module.Version{from}, to, 1, results)
	}()
	return results
}

// xxx recursively queries the Perseus graph searching for dependencies between the last element of chain
// and to.  If a dependency is found, a result is produced to rc.
func (pf *pathFinder) xxx(ctx context.Context, chain []module.Version, to module.Version, depth int, rc chan pathFinderResult) {
	// grab the semaphore b/c unbounded concurrency is :(
	<-pf.sem
	defer func() { pf.sem <- struct{}{} }()

	select {
	case <-ctx.Done():
		rc <- pathFinderResult{err: ctx.Err()}
		return
	default:
		from := chain[len(chain)-1]
		// query the graph for direct dependencies of from
		deps, err := walkDependencies(ctx, pf.c, from, perseusapi.DependencyDirection_dependencies, 1, 1, pf.status)
		if err != nil {
			rc <- pathFinderResult{err: err}
			return
		}
		children := make([]module.Version, 0, len(deps.Deps))
		for _, d := range deps.Deps {
			select {
			case <-ctx.Done():
				rc <- pathFinderResult{err: ctx.Err()}
				return
			default:
				if d.Module.Path == to.Path && (to.Version == "" || d.Module.Version == to.Version) {
					debugLog("found path", "chain", chain, "to", d.Module)
					// data sharing == bad
					cc := make([]module.Version, len(chain))
					copy(cc, chain)
					rc <- pathFinderResult{path: append(cc, d.Module)}
				}
				children = append(children, d.Module)
			}
		}
		// recurse down the graph if we haven't hit max yet
		if depth <= pf.maxDepth {
			for _, c := range children {
				pf.wg.Add(1)
				go func(c module.Version) {
					defer pf.wg.Done()
					// data sharing == bad
					cc := make([]module.Version, len(chain))
					copy(cc, chain)
					pf.xxx(ctx, append(cc, c), to, depth+1, rc)
				}(c)
			}
		}
	}
}
