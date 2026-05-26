package main

import "github.com/microcosm-cc/bluemonday"

var htmlSanitizer = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.AllowAttrs("loading").OnElements("img")
	return p
}()

func sanitizeHTML(s string) string {
	return htmlSanitizer.Sanitize(s)
}
