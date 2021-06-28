package main

import (
	"net/http"
	"path"

	"github.com/flosch/pongo2"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
)

type FriendRender struct {
}

type HTMLRender struct {
	Template *pongo2.Template
	Name     string
	Data     pongo2.Context
}

func NewFriendRender() *FriendRender {
	return &FriendRender{}
}

func (p *FriendRender) Instance(name string, data interface{}) render.Render {
	var template *pongo2.Template
	// fileName := path.Join("templates", name)
	// buff, err := server.Asset(fileName)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	if gin.Mode() == gin.DebugMode {
		// template = pongo2.Must(pongo2.FromString(string(buff)))
		name := path.Join("templates", name)
		template = pongo2.Must(pongo2.FromFile(name))
	} else {
		template = pongo2.Must(pongo2.FromCache(name))
	}

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
