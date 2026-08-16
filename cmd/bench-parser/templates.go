package main

import "fmt"

const css = `
:root{
  --bg:#0f172a; --bg-alt:#0b1220; --card:#1e293b; --card-alt:#182231;
  --border:#334155; --text:#e2e8f0; --muted:#94a3b8;
  --go:#38bdf8; --rust:#f97362; --accent:#34d399; --warn:#fbbf24;
}
*{box-sizing:border-box}
body{
  font-family:'Segoe UI',-apple-system,BlinkMacSystemFont,Roboto,sans-serif;
  background:var(--bg); color:var(--text); margin:0; padding:0;
}
.wrap{max-width:1100px; margin:0 auto; padding:0 1.5rem 3rem;}
a{color:var(--go); text-decoration:none}
a:hover{text-decoration:underline}
.topnav{
  background:var(--bg-alt); border-bottom:1px solid var(--border);
  padding:0.9rem 1.5rem; display:flex; align-items:center; gap:1.5rem;
  flex-wrap:wrap;
}
.topnav .brand{font-weight:800; color:#fff; letter-spacing:.02em}
.topnav a{color:var(--muted); font-weight:600; font-size:.92rem}
.topnav a.active{color:var(--text)}
.topnav .spacer{flex:1}
.breadcrumb{color:var(--muted); font-size:.85rem; margin:1.5rem 0 0.5rem;}
.breadcrumb a{color:var(--muted)}
h1{color:#fff; margin:0.2rem 0 0.4rem; font-size:1.9rem}
h2{color:#f1f5f9; font-size:1.25rem; margin-top:2.2rem}
.subtitle{color:var(--muted); margin-bottom:1.5rem}
.grid{display:grid; grid-template-columns:repeat(auto-fit,minmax(210px,1fr)); gap:1rem; margin:1.2rem 0;}
.card{background:var(--card); border:1px solid var(--border); border-radius:10px; padding:1.1rem 1.2rem;}
.card .label{color:var(--muted); font-size:.82rem; text-transform:uppercase; letter-spacing:.04em}
.card .value{font-size:1.55rem; font-weight:800; color:var(--accent); margin-top:.3rem}
.card .sub{color:var(--muted); font-size:.8rem; margin-top:.25rem}
.card.go .value{color:var(--go)}
.card.rust .value{color:var(--rust)}
table{width:100%; border-collapse:collapse; background:var(--card); border-radius:10px; overflow:hidden; margin-bottom:1.2rem;}
th,td{text-align:left; padding:.6rem .9rem; border-bottom:1px solid var(--border); font-size:.88rem;}
th{background:var(--card-alt); color:var(--muted); font-weight:700; text-transform:uppercase; font-size:.72rem; letter-spacing:.04em;}
tr:last-child td{border-bottom:none}
tr.best td{color:var(--accent); font-weight:700}
.badge{display:inline-block; padding:.15rem .5rem; border-radius:99px; font-size:.72rem; font-weight:700;}
.badge.go{background:rgba(56,189,248,.15); color:var(--go)}
.badge.rust{background:rgba(249,115,98,.15); color:var(--rust)}
.badge.zero{background:rgba(52,211,153,.15); color:var(--accent)}
.family-links{display:grid; grid-template-columns:repeat(auto-fit,minmax(240px,1fr)); gap:1rem; margin:1.2rem 0 2rem;}
.family-links a.tile{
  display:block; background:var(--card); border:1px solid var(--border); border-radius:10px;
  padding:1.1rem 1.2rem; color:var(--text);
}
.family-links a.tile:hover{border-color:var(--go); text-decoration:none}
.family-links .tile .name{font-weight:700; color:#fff}
.family-links .tile .desc{color:var(--muted); font-size:.85rem; margin-top:.3rem}
.note{color:var(--muted); font-size:.85rem; margin:1rem 0;}
footer{color:var(--muted); font-size:.8rem; text-align:center; padding:2rem 0 1rem;}
code{background:var(--card-alt); padding:.1rem .35rem; border-radius:4px; font-size:.85em;}
`

func navHTML(active, root string) string {
	link := func(href, label, key string) string {
		cls := ""
		if key == active {
			cls = ` class="active"`
		}
		return fmt.Sprintf(`<a href="%s"%s>%s</a>`, href, cls, label)
	}
	return `<div class="topnav">` +
		`<span class="brand">🌲 RadixIP Benchmarks</span>` +
		link(root+"index.html", "Overview", "overview") +
		link(root+"go/index.html", "Go", "go") +
		link(root+"rust/index.html", "Rust", "rust") +
		link(root+"compare/index.html", "Go vs Rust", "compare") +
		`<span class="spacer"></span>` +
		fmt.Sprintf(`<a href="%sdev/bench/index.html">Interactive History →</a>`, root) +
		`</div>`
}

func breadcrumbHTML(root string, parts ...[2]string) string {
	out := fmt.Sprintf(`<div class="breadcrumb"><a href="%sindex.html">Overview</a>`, root)
	for _, p := range parts {
		out += " / "
		if p[1] != "" {
			out += fmt.Sprintf(`<a href="%s">%s</a>`, p[1], p[0])
		} else {
			out += p[0]
		}
	}
	out += `</div>`
	return out
}

func pageShell(title, active, root, body string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s · RadixIP Benchmarks</title>
<style>%s</style>
</head>
<body>
%s
<div class="wrap">
%s
<footer>Generated automatically by <code>cmd/bench-parser</code> from CI benchmark logs. Not hand-edited — re-run the workflow to refresh.</footer>
</div>
</body>
</html>`, title, css, navHTML(active, root), body)
}

func fmtNs(ns float64) string {
	if ns >= 1_000_000 {
		return fmt.Sprintf("%.2f ms", ns/1_000_000)
	}
	if ns >= 1000 {
		return fmt.Sprintf("%.0f ns", ns)
	}
	return fmt.Sprintf("%.2f ns", ns)
}

func fmtOps(ops float64) string {
	switch {
	case ops >= 1_000_000:
		return fmt.Sprintf("%.2fM ops/s", ops/1_000_000)
	case ops >= 1000:
		return fmt.Sprintf("%.1fK ops/s", ops/1000)
	default:
		return fmt.Sprintf("%.0f ops/s", ops)
	}
}

func fmtBytes(b float64) string {
	if b == 0 {
		return "0 B"
	}
	if b >= 1024*1024 {
		return fmt.Sprintf("%.2f MB", b/(1024*1024))
	}
	if b >= 1024 {
		return fmt.Sprintf("%.1f KB", b/1024)
	}
	return fmt.Sprintf("%.0f B", b)
}
