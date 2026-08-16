package main

import "sort"

func aggregate(all []classifiedSample) []Stat {
	buckets := map[string]*Stat{}
	order := []string{}

	for _, cs := range all {
		id := cs.Key.ID()
		st, ok := buckets[id]
		if !ok {
			st = &Stat{Key: cs.Key, MinNs: -1}
			buckets[id] = st
			order = append(order, id)
		}
		st.Count++
		st.MeanNs += cs.Sample.NsOp
		if st.MinNs < 0 || cs.Sample.NsOp < st.MinNs {
			st.MinNs = cs.Sample.NsOp
		}
		if cs.Sample.NsOp > st.MaxNs {
			st.MaxNs = cs.Sample.NsOp
		}
		if cs.Sample.HasMem {
			st.HasMem = true
			st.MeanBytesOp += float64(cs.Sample.BytesOp)
			st.MeanAllocsOp += float64(cs.Sample.AllocsOp)
		}
	}

	out := make([]Stat, 0, len(buckets))
	for _, id := range order {
		st := buckets[id]
		n := float64(st.Count)
		st.MeanNs /= n
		if st.HasMem {
			st.MeanBytesOp /= n
			st.MeanAllocsOp /= n
		}
		st.OpsPerSec = 1e9 / st.MeanNs
		st.ZeroAlloc = st.HasMem && st.MeanBytesOp == 0 && st.MeanAllocsOp == 0
		out = append(out, *st)
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Key, out[j].Key
		if a.Runtime != b.Runtime {
			return a.Runtime < b.Runtime
		}
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		if a.Op != b.Op {
			return a.Op < b.Op
		}
		if a.IPVer != b.IPVer {
			return a.IPVer < b.IPVer
		}
		return a.Variant < b.Variant
	})
	return out
}

// bestByNs returns the Stat with the lowest MeanNs from a slice, or nil.
func bestByNs(stats []Stat) *Stat {
	if len(stats) == 0 {
		return nil
	}
	best := stats[0]
	for _, s := range stats[1:] {
		if s.MeanNs < best.MeanNs {
			best = s
		}
	}
	return &best
}

func filterStats(all []Stat, pred func(Stat) bool) []Stat {
	var out []Stat
	for _, s := range all {
		if pred(s) {
			out = append(out, s)
		}
	}
	return out
}

// Analysis holds every cross-cutting insight computed once and reused
// across every rendered page.
type Analysis struct {
	All []Stat

	FastestInsertOverall     *Stat
	FastestLookupOverall     *Stat
	FastestConcurrentOverall *Stat
	ZeroAllocGroups          []Stat

	// FamilySpeedup[runtime][ipver] -> ordered fastest-to-slowest family
	// comparison for single-thread lookup hits, using each family's best variant.
	FamilyRanking map[string][]FamilyRank
}

type FamilyRank struct {
	Family           string
	Best             Stat
	SpeedupVsSlowest float64
}

func buildAnalysis(all []Stat) Analysis {
	a := Analysis{All: all}

	a.FastestInsertOverall = bestByNs(filterStats(all, func(s Stat) bool {
		return s.Key.Op == "insert"
	}))
	a.FastestLookupOverall = bestByNs(filterStats(all, func(s Stat) bool {
		return s.Key.Op == "lookup" && !s.Key.Concurrent && s.Key.SubOp != "miss"
	}))
	a.FastestConcurrentOverall = bestByNs(filterStats(all, func(s Stat) bool {
		return s.Key.Op == "lookup" && s.Key.Concurrent
	}))
	a.ZeroAllocGroups = filterStats(all, func(s Stat) bool { return s.ZeroAlloc })

	a.FamilyRanking = map[string][]FamilyRank{}
	for _, runtime := range []string{"go", "rust"} {
		for _, ipver := range []string{"ipv4", "ipv6"} {
			key := runtime + "-" + ipver
			var ranks []FamilyRank
			for _, fam := range []string{"binary-tries", "patricia-tree", "art"} {
				best := bestByNs(filterStats(all, func(s Stat) bool {
					return s.Key.Runtime == runtime && s.Key.Family == fam &&
						s.Key.IPVer == ipver && s.Key.Op == "lookup" &&
						!s.Key.Concurrent && s.Key.SubOp != "miss"
				}))
				if best != nil {
					ranks = append(ranks, FamilyRank{Family: fam, Best: *best})
				}
			}
			sort.Slice(ranks, func(i, j int) bool { return ranks[i].Best.MeanNs < ranks[j].Best.MeanNs })
			if len(ranks) > 0 {
				slowest := ranks[len(ranks)-1].Best.MeanNs
				for i := range ranks {
					ranks[i].SpeedupVsSlowest = slowest / ranks[i].Best.MeanNs
				}
			}
			a.FamilyRanking[key] = ranks
		}
	}

	return a
}
