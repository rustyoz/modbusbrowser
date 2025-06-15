package main

import (
	"embed"
	"html/template"
	"net/http"
)

//go:embed static/index.html
var staticFiles embed.FS

// ServeIndex serves the main index page
func ServeIndex(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(staticFiles, "static/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = tmpl.Execute(w, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
