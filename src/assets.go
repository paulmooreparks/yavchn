package main

import (
	"crypto/sha256"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

// Version the whole asset set so lazy applets and the shell always agree.
func parseTemplates() (*template.Template, error) {
	hash := sha256.New()
	err := fs.WalkDir(assets, "static", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", path, len(data))
		hash.Write(data)
		return nil
	})
	if err != nil {
		return nil, err
	}
	version := fmt.Sprintf("%x", hash.Sum(nil))[:16]
	analytics := analyticsScript()
	return template.New("").Funcs(template.FuncMap{
		"asset":        func(path string) string { return path + "?v=" + version },
		"assetVersion": func() string { return version },
		"analytics":    func() string { return analytics },
		"join":         strings.Join,
	}).ParseFS(assets, "templates/*.tmpl")
}

// analyticsScript is the Plausible Analytics script this deployment loads,
// from YAVCHN_PLAUSIBLE_SCRIPT, or empty for none. Only production sets it,
// so beta and local runs count nothing. Any other address is refused, since
// the privacy policy says Plausible is the only analytics YAVCHN uses.
func analyticsScript() string {
	src := os.Getenv("YAVCHN_PLAUSIBLE_SCRIPT")
	if src == "" {
		return ""
	}
	if !plausibleScriptRE.MatchString(src) {
		slog.Warn("YAVCHN_PLAUSIBLE_SCRIPT is not a Plausible script address; analytics stay off")
		return ""
	}
	return src
}

var plausibleScriptRE = regexp.MustCompile(`^https://plausible\.io/js/[A-Za-z0-9_.-]+\.js$`)
