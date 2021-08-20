package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/yinhm/friendfeed/util"
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
		"small": {
			Width:  175,
			Height: 175,
			Shape:  "thumb",
		},
		"large": {
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
	AppId   string `json:"gcs_app_id"`
	Bucket  string `json:"gcs_bucket"`
	KeyFile string `json:"gcs_key_file"`
}

type Object struct {
	Filename string
	Bucket   string
	Path     string
	MimeType string
	Url      string
	Content  []byte
}

type Storage interface {
	Exists(name string) (bool, error)
	Post(obj *Object) (*Object, error)
	Mirror(obj *Object) (*Object, error)
	FromUrl(filename, src, mimetype string) (*Object, error)
}

type LocalStorage struct{}

func NewLocalStorage(config *util.Config) *LocalStorage {
	return &LocalStorage{}
}

func (c *LocalStorage) Exists(name string) (bool, error) {
	return false, errors.New("not implemented yet")
}

func (c *LocalStorage) Mirror(obj *Object) (*Object, error) {
	return nil, fmt.Errorf("Mirror not implemented yet: %s", obj.Url)
}

func (c *LocalStorage) FromUrl(filename, src, mimetype string) (*Object, error) {
	return nil, fmt.Errorf("Mirror not implemented yet: %s", src)
}

func (c *LocalStorage) Post(obj *Object) (*Object, error) {
	return nil, errors.New("not implemented yet")
}
