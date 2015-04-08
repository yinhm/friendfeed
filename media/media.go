package media

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yinhm/friendfeed/ff"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gcloud "google.golang.org/cloud"
	"google.golang.org/cloud/storage"
	gcs "google.golang.org/cloud/storage"
)

const (
	apiURL = "http://127.0.0.1:8902"
)

type Client struct {
	client  *http.Client
	BaseURL *url.URL
}

type thumbConfig struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Shape  string `json:"shape"`
}

type Response struct {
	Data struct {
		Width  int               `json:"width"`
		Height int               `json:"height"`
		Link   string            `json:"link"`
		Mime   string            `json:"mime"`
		Name   string            `json:"name"`
		Size   int               `json:"size"`
		Thumbs map[string]string `json:"thumbs"`
	}
	Status  int  `json:"status"`
	Success bool `json:"success"`
}

// client basic for Imgur mandible server
func NewClient() *Client {
	httpClient := http.DefaultClient
	baseURL, err := url.Parse(apiURL)
	if err != nil {
		panic("Error media server address.")
	}

	return &Client{
		client:  httpClient,
		BaseURL: baseURL,
	}
}

func (c *Client) PostUrl(imageUrl string) (*Response, error) {
	thumbs := map[string]thumbConfig{
		"small": thumbConfig{
			Width:  175,
			Height: 175,
			Shape:  "thumb",
		},
		"large": thumbConfig{
			Width:  1600,
			Height: 1600,
			Shape:  "thumb",
		},
	}
	buf := new(bytes.Buffer)
	enc := json.NewEncoder(buf)
	enc.Encode(thumbs)

	// data := map[string]interface{}{
	// 	"image":  imageUrl,
	// 	"thumbs": thumbs,
	// }
	data := url.Values{
		"image":  {imageUrl},
		"thumbs": {buf.String()},
	}

	reqUrl := c.BaseURL.String() + "/url"
	//r, err := http.Post(reqUrl, "application/json", data)
	r, err := http.PostForm(reqUrl, data)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	resp := new(Response)
	if err = json.Unmarshal(body, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

// ----------------------------
// Google Cloud Storage Mirror
// ----------------------------
type Config struct {
	AppId   string `json:"app_id"`
	Bucket  string `json:"bucket"`
	KeyFile string `json:"key_file"`
}

type Object struct {
	Filename string
	Bucket   string
	Path     string
	MimeType string
	Url      string
	Content  []byte
}

func NewConfigFromJSON(filename string) (*Config, error) {
	rawdata, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}

	config := new(Config)
	if err := json.Unmarshal(rawdata, &config); err != nil {
		return nil, err
	}
	return config, nil
}

type Storage interface {
	Exists(name string) (bool, error)
	Post(obj *Object) (*Object, error)
	Mirror(obj *Object) (*Object, error)
	FromUrl(filename, src, mimetype string) (*Object, error)
}

type LocalStorage struct{}

func NewLocalStorage(config *Config) *LocalStorage {
	return &LocalStorage{}
}

func (c *LocalStorage) Exists(name string) (bool, error) {
	return false, fmt.Errorf("not implemented yet.")
}

func (c *LocalStorage) Mirror(obj *Object) (*Object, error) {
	return nil, fmt.Errorf("Mirror not implemented yet: %s", obj.Url)
}

func (c *LocalStorage) FromUrl(filename, src, mimetype string) (*Object, error) {
	return nil, fmt.Errorf("Mirror not implemented yet: %s", src)
}

func (c *LocalStorage) Post(obj *Object) (*Object, error) {
	return nil, fmt.Errorf("not implemented yet.")
}

type GoogleStorage struct {
	ctx        context.Context
	bucket     string
	httpclient *http.Client
}

func NewGoogleStorage(config *Config) *GoogleStorage {
	jsonKey, err := ioutil.ReadFile(config.KeyFile)
	if err != nil {
		log.Fatal(err)
	}
	conf, err := google.JWTConfigFromJSON(
		jsonKey,
		gcs.ScopeFullControl,
	)
	if err != nil {
		log.Fatal(err)
	}

	httpclient := &http.Client{
		Timeout: 5 * time.Second,
	}
	ctx := gcloud.NewContext(config.AppId, conf.Client(oauth2.NoContext))

	return &GoogleStorage{
		ctx:        ctx,
		bucket:     config.Bucket,
		httpclient: httpclient,
	}
}

func (c *GoogleStorage) Exists(name string) (bool, error) {
	_, err := storage.StatObject(c.ctx, c.bucket, name)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (c *GoogleStorage) FromUrl(filename, src, mimetype string) (*Object, error) {
	parsed, err := url.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("Can not parse: %s", src)
	}
	if !ff.IsMediaServer(parsed.Host) {
		return nil, fmt.Errorf("Skip non-ff: %s", src)
	}
	newpath := strings.TrimLeft(parsed.Path, "/")
	if filename == "" {
		filename = newpath
	}
	obj := &Object{
		Filename: filename,
		Path:     newpath,
		Url:      src,
	}
	if mimetype != "" {
		obj.MimeType = mimetype
	}

	return c.Mirror(obj)
}

func (c *GoogleStorage) Mirror(obj *Object) (*Object, error) {
	gcsObj, err := storage.StatObject(c.ctx, c.bucket, obj.Path)
	if err != nil {
		return c.Post(obj)
	}

	newPath := c.bucket + "/" + gcsObj.Name
	newUrl := "https://storage.googleapis.com/" + newPath
	newObj := &Object{
		Filename: obj.Filename,
		Bucket:   c.bucket,
		Path:     newPath,
		MimeType: gcsObj.ContentType,
		Url:      newUrl,
	}
	return newObj, nil
}

func (c *GoogleStorage) Post(obj *Object) (*Object, error) {
	_, err := c.fetch(obj)
	if err != nil {
		log.Println("error on read url:", obj.Url, err)
		return nil, err
	}

	// path = obj.path
	wc := storage.NewWriter(c.ctx, c.bucket, obj.Path)
	wc.ContentType = obj.MimeType
	if _, err := wc.Write(obj.Content); err != nil {
		log.Printf("error on write data: %s", err)
		return nil, err
	}
	if err := wc.Close(); err != nil {
		log.Printf("error on close writer: %s", err)
		return nil, err
	}

	gcsObj := wc.Object()
	newUrl := "https://storage.googleapis.com/" + c.bucket + "/" + gcsObj.Name
	newObj := &Object{
		Filename: obj.Filename,
		Bucket:   c.bucket,
		Path:     gcsObj.Name, // without bucket
		MimeType: obj.MimeType,
		Url:      newUrl,
	}

	return newObj, nil
}

// fetch file from url
func (c *GoogleStorage) fetch(obj *Object) (*http.Response, error) {
	resp, err := http.Get(obj.Url)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return resp, err
	}

	mimeType := resp.Header.Get("Content-Type")
	// contentDisposition := resp.Header.Get("Content-Disposition")
	if obj.MimeType == "" {
		obj.MimeType = mimeType
	}

	obj.Content = body
	return resp, nil
}
