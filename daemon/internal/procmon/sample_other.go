//go:build !linux && !darwin

package procmon

// sampleProcs is a no-op on platforms without a supported sampler (e.g. Windows): the resource
// monitor simply yields empty frames. The rest of the pipeline — stream lifecycle, SSE, UI — works
// unchanged; there are just no numbers to show.
func sampleProcs(pgids map[int]struct{}) map[int]procStat {
	return map[int]procStat{}
}
