package main

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin/render"
)

type FriendRender struct {
	templates *pongo2.TemplateSet
}

type HTMLRender struct {
	Template *pongo2.Template
	Name     string
	Data     pongo2.Context
}

type embeddedTemplateLoader struct {
	fs fs.FS
}

func (loader embeddedTemplateLoader) Abs(base, name string) string {
	if base != "" && !path.IsAbs(name) {
		name = path.Join(path.Dir(base), name)
	}
	return path.Clean(name)
}

func (loader embeddedTemplateLoader) Get(name string) (io.Reader, error) {
	data, err := fs.ReadFile(loader.fs, name)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func NewFriendRender(assets fs.FS, debug bool) *FriendRender {
	var loader pongo2.TemplateLoader
	if debug {
		loader = pongo2.MustNewLocalFileSystemLoader("templates")
	} else {
		loader = embeddedTemplateLoader{fs: assets}
	}
	templates := pongo2.NewSet("templates", loader)
	templates.Debug = debug
	return &FriendRender{templates: templates}
}

func (p *FriendRender) Instance(name string, data any) render.Render {
	template := pongo2.Must(p.templates.FromCache(name))

	return &HTMLRender{
		Template: template,
		Name:     name,
		Data:     data.(pongo2.Context),
	}
}

func (p *HTMLRender) Render(w http.ResponseWriter) error {
	p.WriteContentType(w)
	return p.Template.ExecuteWriter(p.Data, w)
}

func (p *HTMLRender) WriteContentType(w http.ResponseWriter) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = []string{"text/html; charset=utf-8"}
	}
}
