// Package dashboardassets embeds the maintainer dashboard's static files
// (SPA shell, login pages, stylesheet, dashboard script). Serving them as
// files instead of Go raw strings lets the server apply a strict CSP with
// no 'unsafe-inline': scripts and styles are external same-origin assets.
//
// This package only exposes file contents; the server package decides how
// and where they are mounted under /dashboard.
package dashboardassets

import "embed"

// files holds the embedded asset set. The //go:embed list is explicit
// (no wildcards) so every served file is listed here deliberately and
// nothing else in the directory can leak into the binary unnoticed.
//
//go:embed index.html login.html error.html app.js style.css login.css
var files embed.FS

// MustRead returns the contents of the named asset. Names are compile-time
// pinned by the embed list above, so a miss is a programming error and
// panics at startup rather than failing per-request.
func MustRead(name string) string {
	b, err := files.ReadFile(name)
	if err != nil {
		panic("dashboardassets: " + err.Error())
	}
	return string(b)
}
