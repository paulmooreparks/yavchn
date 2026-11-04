package main

import (
	"crypto/sha256"
	"encoding/json"
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
	skins, err := pudlSkins()
	if err != nil {
		return nil, err
	}
	return template.New("").Funcs(template.FuncMap{
		"asset":        func(path string) string { return path + "?v=" + version },
		"assetVersion": func() string { return version },
		"analytics":    func() string { return analytics },
		"join":         strings.Join,
		"skins":        func() []pudlSkin { return skins },
	}).ParseFS(assets, "templates/*.tmpl")
}

// A skin from PUDL's catalogue, pudl-skins.json, which says which skins
// passed PUDL's admission. Pages load every admitted skin and Settings
// offers them, Standard first, so a PUDL release that adds or withdraws
// one needs no change here.
type pudlSkin struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Stylesheet string `json:"stylesheet"`
	Admitted   bool   `json:"admitted"`
}

func pudlSkins() ([]pudlSkin, error) {
	data, err := assets.ReadFile("static/pudl/pudl-skins.json")
	if err != nil {
		return nil, err
	}
	var catalogue struct {
		Version int        `json:"version"`
		Skins   []pudlSkin `json:"skins"`
	}
	if err := json.Unmarshal(data, &catalogue); err != nil {
		return nil, fmt.Errorf("pudl-skins.json: %w", err)
	}
	if catalogue.Version != 1 {
		return nil, fmt.Errorf("pudl-skins.json: version %d is not one YAVCHN reads", catalogue.Version)
	}
	var out []pudlSkin
	for _, s := range catalogue.Skins {
		if !s.Admitted || !skinIDRE.MatchString(s.ID) {
			continue
		}
		if s.ID != "standard" {
			if _, err := assets.ReadFile("static/pudl/min/" + s.Stylesheet); err != nil {
				return nil, fmt.Errorf("pudl-skins.json: skin %s: %w", s.ID, err)
			}
		}
		out = append(out, s)
	}
	return out, nil
}

var skinIDRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

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
