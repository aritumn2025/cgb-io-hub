package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var embeddedWeb embed.FS

func staticAssets() (http.FileSystem, error) {
	sub, err := fs.Sub(embeddedWeb, "static")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}
