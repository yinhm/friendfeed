// A naive friendfeed client do the last job.
package ff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"reflect"

	"github.com/google/go-querystring/query"
	pb "github.com/yinhm/friendfeed/proto"
)

const (
	version   = "0.1"
	apiV1URL  = "http://friendfeed.com"
	apiV2URL  = "http://friendfeed-api.com/v2"
	userAgent = "lastff/" + version
)

// A Client manages communication with the GitHub API.
type Client struct {
	client *http.Client

	BaseURL   *url.URL
	UserAgent string

	username string
	authKey  string
}

// NewClient returns a new Friendfeed API client.
func NewClient(httpClient *http.Client, username, authKey string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL, _ := url.Parse(apiV2URL)

	return &Client{
		client:    httpClient,
		BaseURL:   baseURL,
		UserAgent: userAgent,
		username:  username,
		authKey:   authKey,
	}
}

func NewV1Client(httpClient *http.Client, username, authKey string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL, _ := url.Parse(apiV1URL)

	return &Client{
		client:    httpClient,
		BaseURL:   baseURL,
		UserAgent: userAgent,
		username:  username,
		authKey:   authKey,
	}
}

func (c *Client) Feed(feedId string, opt *FeedOptions) (*pb.Feed, *http.Response, error) {
	path := fmt.Sprintf("/feed/%v", feedId)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	feed := new(pb.Feed)
	resp, err := c.fetch(path, feed)
	if err != nil {
		return nil, resp, err
	}

	return feed, resp, err
}

func (c *Client) Feedinfo(feedId string) (*pb.Feedinfo, *http.Response, error) {
	path := fmt.Sprintf("/feedinfo/%v", feedId)
	info := new(pb.Feedinfo)
	resp, err := c.fetch(path, info)
	if err != nil {
		return nil, resp, err
	}

	return info, resp, err
}

func (c *Client) Entry(eid string, opt *FeedOptions) (*pb.Entry, *http.Response, error) {
	path := fmt.Sprintf("/entry/%v", eid)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	entry := new(pb.Entry)
	resp, err := c.fetch(path, entry)
	if err != nil {
		return nil, resp, err
	}

	return entry, resp, err
}

// Restrict to GET method. No POST Call.
func (c *Client) fetch(path string, v interface{}) (*http.Response, error) {
	req, err := c.NewRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req, v)
}

// v1 api
// /api/user/NICKNAME/profile - Get services and subscriptions
// Returns list of all of the user's subscriptions (people) and services connected to their account (Authentication required for private users):
// eg:
// http://friendfeed.com/api/user/bret/profile
func (c *Client) V1Profile(feedId string, feedType string) (*pb.V1Profile, *http.Response, error) {
	path := fmt.Sprintf("/api/user/%s/profile", feedId)
	if feedType == "group" {
		path = fmt.Sprintf("/api/room/%s/profile", feedId)
	}
	profile := new(pb.V1Profile)
	resp, err := c.fetch(path, profile)
	if err != nil {
		return nil, resp, err
	}

	return profile, resp, err
}

// NewRequest creates an API request. A relative URL can be provided in path,
// in which case it is resolved relative to the BaseURL of the Client.
// Relative URLs should always be specified without a preceding slash.  If
// specified, the value pointed to by body is JSON encoded and included as the
// request body.
func (c *Client) NewRequest(method, path string, body interface{}) (*http.Request, error) {
	_, err := url.Parse(path)
	if err != nil {
		return nil, err
	}

	u := c.BaseURL.String() + path
	log.Printf("request: %s", u)

	var buf io.ReadWriter
	if body != nil {
		buf = new(bytes.Buffer)
		err := json.NewEncoder(buf).Encode(body)
		if err != nil {
			return nil, err
		}
	}

	req, err := http.NewRequest(method, u, buf)
	if err != nil {
		return nil, err
	}

	req.Header.Add("User-Agent", c.UserAgent)
	req.SetBasicAuth(c.username, c.authKey)
	return req, nil
}

// Do sends an API request and returns the API response.  The API response is
// JSON decoded and stored in the value pointed to by v, or returned as an
// error if an API error has occurred.  If v implements the io.Writer
// interface, the raw response body will be written to v, without attempting to
// first decode it.
func (c *Client) Do(req *http.Request, v interface{}) (*http.Response, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	err = CheckResponse(resp)
	if err != nil {
		// even though there was an error, we still return the response
		// in case the caller wants to inspect it further
		return resp, err
	}

	if v != nil {
		if w, ok := v.(io.Writer); ok {
			io.Copy(w, resp.Body)
		} else {
			err = json.NewDecoder(resp.Body).Decode(v)
		}
	}
	return resp, err
}

/*
An ErrorResponse reports one or more errors caused by an API request.

*/
type ErrorResponse struct {
	Response *http.Response // HTTP response that caused this error
	Message  string         `json:"errorCode"` // error message
}

func (r *ErrorResponse) Error() string {
	return fmt.Sprintf("%v %v: %d %v",
		r.Response.Request.Method, r.Response.Request.URL,
		r.Response.StatusCode, r.Message)
}

// CheckResponse checks the API response for errors, and returns them if
// present.  A response is considered an error if it has a status code outside
// the 200 range.  API error responses are expected to have either no response
// body, or a JSON response body that maps to ErrorResponse.  Any other
// response body will be silently ignored.
func CheckResponse(r *http.Response) error {
	if c := r.StatusCode; 200 <= c && c <= 299 {
		return nil
	}
	errorResponse := &ErrorResponse{Response: r}
	data, err := ioutil.ReadAll(r.Body)
	if err == nil && data != nil {
		json.Unmarshal(data, errorResponse)
	}
	return errorResponse
}

// All feeds support the following optional arguments:

// start=index - Return entries starting with the given index, e.g., start=30
// num=number - Return the specified number of entries starting from start, e.g., num=10
// maxcomments=limit - If limit is 0, then do not include any comments. If limit is auto, then return the same number of comments as displayed by default on friendfeed.com. See Collapsing comments and likes
// maxlikes=limit - If limit is 0, then do not include any likes. If limit is auto, then return the same number of likes as displayed by default on friendfeed.com. See Collapsing comments and likes
// hidden=1 - If specified, include hidden entries in the response. By default, hidden entries are excluded from the response. Hidden entries include the additional property hidden indicating the entry should be hidden based on the user's preferences.
// fof=1 - Include "friend-of-friend" entries in the response. By default, friend-of-friend entries are excluded from the response. See Friend-of-friend entries
// raw=1 - Include raw text entry and comment bodies in addition to the HTML bodies included by default. The raw text bodies are available as rawBody on all returned entries and comments. This also adds rawLink on all entries.
type FeedOptions struct {
	Start       int    `url:"start,omitempty"`
	Num         int    `url:"num,omitempty"`
	Maxcomments int    `url:"maxcomments,omitempty"`
	Maxlikes    int    `url:"maxlikes,omitempty"`
	Hidden      int    `url:"hidden,omitempty"`
	FoF         int    `url:"fof,omitempty"`
	RawBody     int    `url:"raw,omitempty"`
	MaxComments string `url:"maxcomments,omitempty"`
	MaxLikes    string `url:"maxlikes,omitempty"`
}

// addOptions adds the parameters in opt as URL query parameters to s.  opt
// must be a struct whose fields may contain "url" tags.
func addOptions(s string, opt interface{}) (string, error) {
	v := reflect.ValueOf(opt)
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return s, nil
	}

	u, err := url.Parse(s)
	if err != nil {
		return s, err
	}

	qs, err := query.Values(opt)
	if err != nil {
		return s, err
	}

	u.RawQuery = qs.Encode()
	return u.String(), nil
}

func IsMediaServer(host string) bool {
	mediaServers := map[string]bool{
		"i.friendfeed.com":                  true,
		"m.friendfeed-media.com":            true,
		"friendfeed-media.s3.amazonaws.com": true,
	}
	ok, _ := mediaServers[host]
	return ok
}
