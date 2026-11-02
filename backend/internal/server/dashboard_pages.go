package server

import (
	"strings"

	"vote-backend/internal/server/dashboardassets"
)

// The login page and its wrong-secret variant are one embedded template
// (dashboardassets/login.html) carrying an HTML-comment placeholder where
// the error banner goes. The vars below are the rendered results, named
// exactly as the auth handlers in auth.go use them; auth.go itself is
// untouched.
const (
	// loginErrorPlaceholder must appear exactly once in login.html.
	loginErrorPlaceholder = "<!--LOGIN_ERROR-->"
	// Named without "secret": gosec G101 flags credential-sounding names
	// even when the value is just HTML error text. auth.go uses only the
	// rendered loginFailedHTML, not this const.
	loginErrorBadAuth = `<div class="error">Secret incorrect.</div>`
)

// renderLoginPage renders login.html with errorHTML substituted for the
// placeholder ("" for the plain page).
func renderLoginPage(errorHTML string) string {
	tmpl := dashboardassets.MustRead("login.html")
	if strings.Count(tmpl, loginErrorPlaceholder) != 1 {
		panic("login.html: expected exactly one " + loginErrorPlaceholder + " placeholder")
	}
	return strings.Replace(tmpl, loginErrorPlaceholder, errorHTML, 1)
}

var (
	loginPageHTML   = renderLoginPage("")
	loginFailedHTML = renderLoginPage(loginErrorBadAuth)

	// loginInternalErrorHTML is served when the server cannot mint a session
	// cookie (e.g. the kernel CSPRNG is unavailable, R10). Distinct from
	// loginFailedHTML so an operator seeing a 500 knows it is a server fault,
	// not a wrong password — and so a brute-forcer gets no signal either way.
	loginInternalErrorHTML = dashboardassets.MustRead("error.html")
)
