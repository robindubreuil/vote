package server

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"vote-backend/internal/server/dashboardassets"
)

// dashboardCSP is the Content-Security-Policy applied to every response in
// the /dashboard group (SPA shell, login pages, assets). All script and
// style is served as same-origin files under /dashboard/assets, so neither
// script-src nor style-src needs 'unsafe-inline': inline <script> bodies,
// <style> blocks, style="" attributes and inline event handlers are all
// rejected by the browser. form-action 'self' keeps the login/logout POST
// forms working under default-src 'none'.
const dashboardCSP = "default-src 'none'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; connect-src 'self'; base-uri 'none'; " +
	"frame-ancestors 'none'; form-action 'self'"

// dashboardIndexHTML is the auth-gated SPA shell: static markup only, it
// loads /dashboard/assets/style.css and /dashboard/assets/app.js. All
// dynamic behavior lives in app.js.
var dashboardIndexHTML = dashboardassets.MustRead("index.html")

// handleDashboardIndex serves the dashboard shell (GET /dashboard, auth).
func (s *Server) handleDashboardIndex(c *gin.Context) {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(dashboardIndexHTML))
}

// serveDashboardAsset returns a handler serving one named embedded asset.
// Assets are inert (markup, style, code — no data, no secrets) so they are
// served without auth; the shell page and every data endpoint keep exactly
// their current auth semantics. Explicit per-file routes rather than a
// FileServer: no directory listing, no path traversal, and nothing new in
// the embed set is exposed without a route being written here. no-cache
// keeps upgrades immediate, matching the always-fresh inline pages this
// setup replaced.
func (s *Server) serveDashboardAsset(name, contentType string) gin.HandlerFunc {
	body := dashboardassets.MustRead(name)
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.Data(http.StatusOK, contentType, []byte(body))
	}
}
