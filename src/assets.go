package main

import (
	"crypto/sha256"
	"fmt"
	"html/template"
	"io/fs"
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
	return template.New("").Funcs(template.FuncMap{
		"asset":        func(path string) string { return path + "?v=" + version },
		"assetVersion": func() string { return version },
	}).ParseFS(assets, "templates/*.tmpl")
}
