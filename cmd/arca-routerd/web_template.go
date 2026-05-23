package main

import (
	_ "embed"
	"html/template"
)

//go:embed web_index.html
var webIndexHTML string

var webIndexTemplate = template.Must(template.New("web-index").Parse(webIndexHTML))
