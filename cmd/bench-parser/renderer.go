package main

import (
	"fmt"
	"sort"
	"strings"
)

var families = []string{"binary-tries", "patricia-tree", "art"}
var familyDesc = map[string]string{
	"binary-tries":  "Uncompressed bit-by-bit trie traversal. Simple, predictable, control-plane baseline.",
	"patricia-tree": "Branch-compressed Patricia/radix traversal. The data-plane / FIB workhorse.",
	"art":           "Adaptive Radix Tree — dynamic node sizes, SIMD/cache-friendly, the high-throughput read path.",
}

func famPath(root, runtime, family string) string {
	return fmt.Sprintf("%s%s/%s/index.html", root, runtime, family)
}


func renderOverview(a Analysis, root string) string {
	var b strings.Builder
	b.WriteString(breadcrumbHTML(root))
	b.WriteString(`<h1>RadixIP Benchmark Dashboard</h1>`)
	b.WriteString(`<p class="subtitle">Automatically generated from the latest CI run. Every number below is parsed straight out of <code>go test -bench</code> and <code>cargo bench</code> output — nothing hand-typed.</p>`)

	b.WriteString(`<div class="grid">`)
	b.WriteString(headlineCard("Fastest Insert", a.FastestInsertOverall))
	b.WriteString(headlineCard("Fastest Lookup (single-thread)", a.FastestLookupOverall))
	b.WriteString(headlineCard("Fastest Concurrent Lookup", a.FastestConcurrentOverall))
	b.WriteString(fmt.Sprintf(`<div class="card"><div class="label">Zero-Allocation Configs</div><div class="value">%d</div><div class="sub">benchmark groups with 0 B/op, 0 allocs/op</div></div>`, len(a.ZeroAllocGroups)))
	b.WriteString(`</div>`)

	b.WriteString(`<h2>Browse by runtime</h2>`)
	b.WriteString(`<div class="family-links">`)
	b.WriteString(fmt.Sprintf(`<a class="tile" href="%sgo/index.html"><div class="name">🐹 Go (radixip-go)</div><div class="desc">Lock-free reads, SIMD Node16 via Rust FFI, atomic pointer traversal.</div></a>`, root))
	b.WriteString(fmt.Sprintf(`<a class="tile" href="%srust/index.html"><div class="name">🦀 Rust (radixip-rs)</div><div class="desc">Zero-cost abstractions, generic StandardEngine&lt;T: RouteTree&gt;.</div></a>`, root))
	b.WriteString(fmt.Sprintf(`<a class="tile" href="%scompare/index.html"><div class="name">⚖️ Go vs Rust</div><div class="desc">Head-to-head latency comparison, family by family.</div></a>`, root))
	b.WriteString(`</div>`)

	b.WriteString(`<h2>Browse by tree implementation</h2>`)
	b.WriteString(`<div class="family-links">`)
	for _, fam := range families {
		b.WriteString(fmt.Sprintf(`<a class="tile" href="%s"><div class="name">%s</div><div class="desc">%s</div></a>`,
			famPath(root, "go", fam), familyLabel(fam)+" · Go", familyDesc[fam]))
	}
	for _, fam := range families {
		b.WriteString(fmt.Sprintf(`<a class="tile" href="%s"><div class="name">%s</div><div class="desc">%s</div></a>`,
			famPath(root, "rust", fam), familyLabel(fam)+" · Rust", familyDesc[fam]))
	}
	b.WriteString(`</div>`)

	b.WriteString(`<p class="note">Methodology: Go figures are the mean of <code>-count=10</code> samples per benchmark. Rust figures are single Criterion/bencher samples (<code>cargo bench -- --output-format bencher</code>). Throughput = 1e9 / mean(ns/op). See the interactive history link in the nav bar for commit-over-commit trend charts.</p>`)

	return b.String()
}

func headlineCard(label string, s *Stat) string {
	if s == nil {
		return fmt.Sprintf(`<div class="card"><div class="label">%s</div><div class="value">—</div></div>`, label)
	}
	cls := "go"
	if s.Key.Runtime == "rust" {
		cls = "rust"
	}
	return fmt.Sprintf(`<div class="card %s"><div class="label">%s</div><div class="value">%s</div><div class="sub"><span class="badge %s">%s</span> %s · %s · %s variant</div></div>`,
		cls, label, fmtNs(s.MeanNs), cls, runtimeLabel(s.Key.Runtime),
		familyLabel(s.Key.Family), ipverLabel(s.Key.IPVer), s.Key.Variant)
}


func renderRuntimeIndex(a Analysis, runtime, root string) string {
	rlabel := runtimeLabel(runtime)
	var b strings.Builder
	b.WriteString(breadcrumbHTML(root, [2]string{rlabel, ""}))
	b.WriteString(fmt.Sprintf(`<h1>%s Benchmarks</h1>`, rlabel))
	if runtime == "go" {
		b.WriteString(`<p class="subtitle">radixip-go — lock-free binary radix tree, SIMD-accelerated ART Node16 via Rust FFI.</p>`)
	} else {
		b.WriteString(`<p class="subtitle">radixip-rs — zero-cost generic engine, Criterion-benchmarked.</p>`)
	}

	fastestLookup := bestByNs(filterStats(a.All, func(s Stat) bool {
		return s.Key.Runtime == runtime && s.Key.Op == "lookup" && !s.Key.Concurrent && s.Key.SubOp != "miss"
	}))
	fastestConcurrent := bestByNs(filterStats(a.All, func(s Stat) bool {
		return s.Key.Runtime == runtime && s.Key.Op == "lookup" && s.Key.Concurrent
	}))
	fastestInsert := bestByNs(filterStats(a.All, func(s Stat) bool {
		return s.Key.Runtime == runtime && s.Key.Op == "insert"
	}))

	b.WriteString(`<div class="grid">`)
	b.WriteString(headlineCard("Fastest Insert", fastestInsert))
	b.WriteString(headlineCard("Fastest Lookup", fastestLookup))
	b.WriteString(headlineCard("Fastest Concurrent Lookup", fastestConcurrent))
	b.WriteString(`</div>`)

	b.WriteString(`<h2>Tree implementations</h2>`)
	b.WriteString(`<div class="family-links">`)
	for _, fam := range families {
		best := bestByNs(filterStats(a.All, func(s Stat) bool {
			return s.Key.Runtime == runtime && s.Key.Family == fam && s.Key.Op == "lookup" && !s.Key.Concurrent && s.Key.SubOp != "miss"
		}))
		sub := familyDesc[fam]
		if best != nil {
			sub = fmt.Sprintf("Best lookup: %s (%s, %s)", fmtNs(best.MeanNs), best.Key.Variant, ipverLabel(best.Key.IPVer))
		}
		b.WriteString(fmt.Sprintf(`<a class="tile" href="%s"><div class="name">%s</div><div class="desc">%s</div></a>`,
			famPath(root, runtime, fam), familyLabel(fam), sub))
	}
	b.WriteString(`</div>`)

	b.WriteString(`<h2>Best variant per configuration</h2>`)
	b.WriteString(bestConfigTable(a.All, runtime))

	return b.String()
}

func bestConfigTable(all []Stat, runtime string) string {
	type row struct {
		fam, op, ipver string
		best           *Stat
	}
	var rows []row
	ops := []struct {
		op, sub string
		conc    bool
	}{
		{"insert", "", false},
		{"lookup", "hit", false},
		{"lookup", "miss", false},
		{"lookup", "", true},
	}
	for _, fam := range families {
		for _, ip := range []string{"ipv4", "ipv6"} {
			for _, o := range ops {
				best := bestByNs(filterStats(all, func(s Stat) bool {
					return s.Key.Runtime == runtime && s.Key.Family == fam && s.Key.IPVer == ip &&
						s.Key.Op == o.op && s.Key.SubOp == o.sub && s.Key.Concurrent == o.conc
				}))
				if best != nil {
					rows = append(rows, row{fam, opLabel(best.Key), ip, best})
				}
			}
		}
	}
	if len(rows) == 0 {
		return `<p class="note">No data.</p>`
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr><th>Tree</th><th>Operation</th><th>IP</th><th>Best Variant</th><th>Latency</th><th>Throughput</th><th>Memory</th></tr></thead><tbody>`)
	for _, r := range rows {
		mem := "n/a"
		if r.best.HasMem {
			mem = fmt.Sprintf("%s / %.0f allocs", fmtBytes(r.best.MeanBytesOp), r.best.MeanAllocsOp)
		}
		b.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			familySlugTitle(r.fam), r.op, ipverLabel(r.ipver), r.best.Key.Variant, fmtNs(r.best.MeanNs), fmtOps(r.best.OpsPerSec), mem))
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}


type sectionSpec struct {
	op, sub string
	conc    bool
	title   string
}

var sectionSpecs = []sectionSpec{
	{"insert", "", false, "Insert"},
	{"lookup", "hit", false, "Lookup — Hit"},
	{"lookup", "miss", false, "Lookup — Miss"},
	{"lookup", "", true, "Concurrent Lookup"},
}

func renderFamilyPage(a Analysis, runtime, family, root string) string {
	rlabel := runtimeLabel(runtime)
	flabel := familyLabel(family)
	var b strings.Builder
	b.WriteString(breadcrumbHTML(root,
		[2]string{rlabel, root + runtime + "/index.html"},
		[2]string{familySlugTitle(family), ""}))
	b.WriteString(fmt.Sprintf(`<h1>%s · %s</h1>`, rlabel, flabel))
	b.WriteString(fmt.Sprintf(`<p class="subtitle">%s</p>`, familyDesc[family]))

	subset := filterStats(a.All, func(s Stat) bool { return s.Key.Runtime == runtime && s.Key.Family == family })
	if len(subset) == 0 {
		b.WriteString(`<p class="note">No benchmark data was found for this configuration in the latest CI run.</p>`)
		return b.String()
	}

	any := false
	for _, spec := range sectionSpecs {
		for _, ip := range []string{"ipv4", "ipv6"} {
			rows := filterStats(subset, func(s Stat) bool {
				return s.Key.Op == spec.op && s.Key.SubOp == spec.sub && s.Key.Concurrent == spec.conc && s.Key.IPVer == ip
			})
			if len(rows) == 0 {
				continue
			}
			any = true
			sort.Slice(rows, func(i, j int) bool { return rows[i].MeanNs < rows[j].MeanNs })
			b.WriteString(fmt.Sprintf(`<h2>%s — %s</h2>`, spec.title, ipverLabel(ip)))
			b.WriteString(variantTable(rows))
		}
	}

	// Package-level "n/a" ipver bucket (lib/go/art Tree_* core microbenchmarks)
	naRows := filterStats(subset, func(s Stat) bool { return s.Key.IPVer == "n/a" })
	if len(naRows) > 0 {
		any = true
		sort.Slice(naRows, func(i, j int) bool { return naRows[i].MeanNs < naRows[j].MeanNs })
		b.WriteString(`<h2>Core tree microbenchmarks</h2>`)
		b.WriteString(`<p class="note">Raw ART tree operations from the engine-agnostic tree test suite (no IP-version or size label in the benchmark name).</p>`)
		b.WriteString(variantTable(naRows))
	}

	if !any {
		b.WriteString(`<p class="note">No benchmark data was found for this configuration in the latest CI run.</p>`)
	}

	return b.String()
}

func variantTable(rows []Stat) string {
	hasMem := false
	for _, r := range rows {
		if r.HasMem {
			hasMem = true
		}
	}
	var b strings.Builder
	b.WriteString(`<table><thead><tr><th>Variant</th><th>Mean</th><th>Min / Max</th><th>Throughput</th>`)
	if hasMem {
		b.WriteString(`<th>Memory / op</th><th>Allocs / op</th>`)
	}
	b.WriteString(`<th>Samples</th></tr></thead><tbody>`)
	for i, r := range rows {
		cls := ""
		if i == 0 {
			cls = ` class="best"`
		}
		b.WriteString(fmt.Sprintf(`<tr%s><td>%s%s</td><td>%s</td><td>%s / %s</td><td>%s</td>`,
			cls, r.Key.Variant, bestBadge(i), fmtNs(r.MeanNs), fmtNs(r.MinNs), fmtNs(r.MaxNs), fmtOps(r.OpsPerSec)))
		if hasMem {
			if r.HasMem {
				zero := ""
				if r.ZeroAlloc {
					zero = ` <span class="badge zero">zero-alloc</span>`
				}
				b.WriteString(fmt.Sprintf(`<td>%s%s</td><td>%.0f</td>`, fmtBytes(r.MeanBytesOp), zero, r.MeanAllocsOp))
			} else {
				b.WriteString(`<td>n/a</td><td>n/a</td>`)
			}
		}
		b.WriteString(fmt.Sprintf(`<td>%d</td></tr>`, r.Count))
	}
	b.WriteString(`</tbody></table>`)
	return b.String()
}

func bestBadge(i int) string {
	if i == 0 {
		return " 🏆"
	}
	return ""
}


func renderComparePage(a Analysis, root string) string {
	var b strings.Builder
	b.WriteString(breadcrumbHTML(root, [2]string{"Go vs Rust", ""}))
	b.WriteString(`<h1>Go vs Rust</h1>`)
	b.WriteString(`<p class="subtitle">Best single-thread lookup latency per tree implementation, head to head. Lower is better; the speedup column shows how many times faster the winner is.</p>`)

	for _, ip := range []string{"ipv4", "ipv6"} {
		b.WriteString(fmt.Sprintf(`<h2>%s</h2>`, ipverLabel(ip)))
		b.WriteString(`<table><thead><tr><th>Tree</th><th>Go Best</th><th>Rust Best</th><th>Winner</th><th>Speedup</th></tr></thead><tbody>`)
		for _, fam := range families {
			goBest := bestByNs(filterStats(a.All, func(s Stat) bool {
				return s.Key.Runtime == "go" && s.Key.Family == fam && s.Key.IPVer == ip && s.Key.Op == "lookup" && !s.Key.Concurrent && s.Key.SubOp != "miss"
			}))
			rustBest := bestByNs(filterStats(a.All, func(s Stat) bool {
				return s.Key.Runtime == "rust" && s.Key.Family == fam && s.Key.IPVer == ip && s.Key.Op == "lookup" && !s.Key.Concurrent && s.Key.SubOp != "miss"
			}))
			goCell, rustCell, winner, speedup := "n/a", "n/a", "—", "—"
			if goBest != nil {
				goCell = fmt.Sprintf("%s (%s)", fmtNs(goBest.MeanNs), goBest.Key.Variant)
			}
			if rustBest != nil {
				rustCell = fmt.Sprintf("%s (%s)", fmtNs(rustBest.MeanNs), rustBest.Key.Variant)
			}
			if goBest != nil && rustBest != nil {
				if goBest.MeanNs < rustBest.MeanNs {
					winner = `<span class="badge go">Go</span>`
					speedup = fmt.Sprintf("%.2fx", rustBest.MeanNs/goBest.MeanNs)
				} else {
					winner = `<span class="badge rust">Rust</span>`
					speedup = fmt.Sprintf("%.2fx", goBest.MeanNs/rustBest.MeanNs)
				}
			}
			b.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
				familySlugTitle(fam), goCell, rustCell, winner, speedup))
		}
		b.WriteString(`</tbody></table>`)
	}

	b.WriteString(`<h2>Concurrent lookup</h2>`)
	b.WriteString(`<table><thead><tr><th>Tree</th><th>Go Best</th><th>Rust Best</th><th>Winner</th><th>Speedup</th></tr></thead><tbody>`)
	for _, fam := range families {
		goBest := bestByNs(filterStats(a.All, func(s Stat) bool {
			return s.Key.Runtime == "go" && s.Key.Family == fam && s.Key.Op == "lookup" && s.Key.Concurrent
		}))
		rustBest := bestByNs(filterStats(a.All, func(s Stat) bool {
			return s.Key.Runtime == "rust" && s.Key.Family == fam && s.Key.Op == "lookup" && s.Key.Concurrent
		}))
		goCell, rustCell, winner, speedup := "n/a", "n/a", "—", "—"
		if goBest != nil {
			goCell = fmt.Sprintf("%s (%s)", fmtNs(goBest.MeanNs), goBest.Key.Variant)
		}
		if rustBest != nil {
			rustCell = fmt.Sprintf("%s (%s)", fmtNs(rustBest.MeanNs), rustBest.Key.Variant)
		}
		if goBest != nil && rustBest != nil {
			if goBest.MeanNs < rustBest.MeanNs {
				winner = `<span class="badge go">Go</span>`
				speedup = fmt.Sprintf("%.2fx", rustBest.MeanNs/goBest.MeanNs)
			} else {
				winner = `<span class="badge rust">Rust</span>`
				speedup = fmt.Sprintf("%.2fx", goBest.MeanNs/rustBest.MeanNs)
			}
		}
		b.WriteString(fmt.Sprintf(`<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			familySlugTitle(fam), goCell, rustCell, winner, speedup))
	}
	b.WriteString(`</tbody></table>`)

	return b.String()
}
