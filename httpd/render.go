package main

import (
	"net/http"

	"github.com/flosch/pongo2"
)

type Render struct {
	cache map[string]*pongo2.Template
}

func NewRender() *Render {
	return &Render{map[string]*pongo2.Template{}}
}

func (p *Render) Render(w http.ResponseWriter, code int, data ...interface{}) error {
	file := "templates/" + data[0].(string)
	ctx := data[1].(pongo2.Context)
	var t *pongo2.Template

	if tmpl, ok := p.cache[file]; ok {
		t = tmpl
	} else {
		if options.Debug {
			tmpl, err := pongo2.FromFile(file)
			if err != nil {
				return err
			}
			t = tmpl
		} else {
			buff, err := Asset(file)
			if err == nil {
				tmpl, err := pongo2.FromString(string(buff))
				if err != nil {
					return err
				}
				t = tmpl
			} else {
				tmpl, err := pongo2.FromFile(file)
				if err != nil {
					return err
				}
				t = tmpl
			}

		}
		p.cache[file] = t
	}
	writeHeader(w, code, "text/html")
	return t.ExecuteWriter(ctx, w)
}

func writeHeader(w http.ResponseWriter, code int, contentType string) {
	if code >= 0 {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(code)
	}
}
