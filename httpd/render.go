package main

import (
	"bytes"
	"fmt"
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
	Err      error
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

func NewFriendRender(assets fs.FS, debug bool) (*FriendRender, error) {
	var loader pongo2.TemplateLoader
	if debug {
		localLoader, err := pongo2.NewLocalFileSystemLoader("templates")
		if err != nil {
			return nil, fmt.Errorf("create template loader: %w", err)
		}
		loader = localLoader
	} else {
		loader = embeddedTemplateLoader{fs: assets}
	}
	templates := pongo2.NewSet("templates", loader)
	templates.Debug = debug
	return &FriendRender{templates: templates}, nil
}

func (p *FriendRender) Instance(name string, data any) render.Render {
	template, err := p.templates.FromCache(name)
	if err != nil {
		return &HTMLRender{Name: name, Err: fmt.Errorf("load template %q: %w", name, err)}
	}
	context, ok := data.(pongo2.Context)
	if !ok {
		return &HTMLRender{Name: name, Err: fmt.Errorf("render template %q: expected pongo2.Context, got %T", name, data)}
	}

	return &HTMLRender{
		Template: template,
		Name:     name,
		Data:     context,
	}
}

func (p *HTMLRender) Render(w http.ResponseWriter) error {
	p.WriteContentType(w)
	if p.Err != nil {
		return p.Err
	}
	return p.Template.ExecuteWriter(p.Data, w)
}

func (p *HTMLRender) WriteContentType(w http.ResponseWriter) {
	header := w.Header()
	if val := header["Content-Type"]; len(val) == 0 {
		header["Content-Type"] = []string{"text/html; charset=utf-8"}
	}
}
