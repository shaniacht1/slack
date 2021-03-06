package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Comment holds information about a file comment
type Comment struct {
	ID        string     `json:"id"`
	Timestamp int64      `json:"timestamp"`
	User      string     `json:"user"`
	Comment   string     `json:"comment"`
	Created   int64      `json:"created,omitempty"`
	Reactions []Reaction `json:"reactions,omitempty"`
}

// File holds information about a file
type File struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`

	Name       string `json:"name"`
	Title      string `json:"title,omitempty"`
	Mimetype   string `json:"mimetype"`
	Filetype   string `json:"filetype"`
	PrettyType string `json:"pretty_type"`
	UserID     string `json:"user"`

	Mode         string `json:"mode,omitempty"`
	Editable     bool   `json:"editable"`
	IsExternal   bool   `json:"is_external"`
	ExternalType string `json:"external_type,omitempty"`

	Size int `json:"size"`

	URL                string `json:"url,omitempty"`
	URLDownload        string `json:"url_download,omitempty"`
	URLPrivate         string `json:"url_private,omitempty"`
	URLPrivateDownload string `json:"url_private_download,omitempty"`

	Thumb64     string `json:"thumb_64,omitempty"`
	Thumb80     string `json:"thumb_80,omitempty"`
	Thumb360    string `json:"thumb_360,omitempty"`
	Thumb360Gif string `json:"thumb_360_gif,omitempty"`
	Thumb360W   int    `json:"thumb_360_w"`
	Thumb360H   int    `json:"thumb_360_h"`

	Permalink        string `json:"permalink,omitempty"`
	EditLink         string `json:"edit_link,omitempty"`
	Preview          string `json:"preview,omitempty"`
	PreviewHighlight string `json:"preview_highlight,omitempty"`
	Lines            int    `json:"lines"`
	LinesMore        int    `json:"lines_more"`

	IsPublic        bool     `json:"is_public"`
	PublicURLShared bool     `json:"public_url_shared"`
	Channels        []string `json:"channels,omitempty"`
	Groups          []string `json:"groups,omitempty"`
	InitialComment  Comment  `json:"initial_comment,omitempty"`
	NumStars        int      `json:"num_stars"`
	IsStarred       bool     `json:"is_starred"`

	Reactions []Reaction `json:"reactions,omitempty"`
}

// FileUploadResponse is the response to the file upload command
type FileUploadResponse struct {
	slackResponse
	File File `json:"file"`
}

type paging struct {
	Count int `json:"count"`
	Total int `json:"total"`
	Page  int `json:"page"`
	Pages int `json:"pages"`
}

// FileListResponse is the response to the file list command
type FileListResponse struct {
	slackResponse
	Files  []File `json:"files"`
	Paging paging `json:"paging"`
}

// FileResponse for file info command
type FileResponse struct {
	slackResponse
	File     File      `json:"file"`
	Comments []Comment `json:"comments"`
	Paging   paging    `json:"paging"`
}

// CommentResponse is returned for comment actions
type CommentResponse struct {
	slackResponse
	Comment Comment `json:"comment"`
}

// doUpload executes the API request for file upload
// Returns the response if the status code is between 200 and 299
func (s *Slack) doUpload(path, filename string, params url.Values, data io.Reader, result interface{}) error {
	appendNotEmpty("token", s.token, params)
	var t time.Time
	if s.tracelog != nil {
		t = time.Now()
		s.tracef("Start request %s at %v", path, t)
	}
	// Pipe the file so as not to read it into memory
	bodyReader, bodyWriter := io.Pipe()
	// create a multipat/mime writer
	writer := multipart.NewWriter(bodyWriter)
	// get the Content-Type of our form data
	fdct := writer.FormDataContentType()
	// Read file errors from the channel
	errChan := make(chan error, 1)
	go func() {
		defer bodyWriter.Close()
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			errChan <- err
			return
		}
		if _, err := io.Copy(part, data); err != nil {
			errChan <- err
			return
		}
		for k, v := range params {
			if err := writer.WriteField(k, v[0]); err != nil {
				errChan <- err
				return
			}
		}
		errChan <- writer.Close()
	}()

	// create a HTTP request with our body, that contains our file
	postReq, err := http.NewRequest("POST", s.url+path, bodyReader)
	if err != nil {
		return err
	}
	// add the Content-Type we got earlier to the request header.
	postReq.Header.Add("Content-Type", fdct)

	s.dumpRequest(postReq)

	// send our request off, get response and/or error
	resp, err := s.c.Do(postReq)
	if cerr := <-errChan; cerr != nil {
		return cerr
	}

	if s.tracelog != nil {
		s.tracef("End request %s at %v - took %v", path, time.Now(), time.Since(t))
	}

	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err = s.handleError(resp); err != nil {
		return err
	}
	s.dumpResponse(resp)
	if result != nil {
		if err = json.NewDecoder(resp.Body).Decode(result); err != nil {
			return err
		}
		// Handle ok response parameter
		sm := result.(Response)
		if !sm.IsOK() {
			s.errorf("%s\n", sm.Error())
			return sm
		}
	}
	return nil
}

// Upload a file to Slack optionally sharing it on given channels
func (s *Slack) Upload(title, filetype, filename, initialComment string, channels []string, data io.Reader) (*FileUploadResponse, error) {
	if filename == "" {
		return nil, fmt.Errorf("You must specify the filename for the upload")
	}
	params := url.Values{}
	appendNotEmpty("title", title, params)
	appendNotEmpty("filetype", filetype, params)
	appendNotEmpty("filename", filename, params)
	appendNotEmpty("initial_comment", initialComment, params)
	if len(channels) > 0 {
		params.Set("channels", strings.Join(channels, ","))
	}
	r := &FileUploadResponse{}
	err := s.doUpload("files.upload", filename, params, data, r)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// FileList the files for the team
func (s *Slack) FileList(user, tsFrom, tsTo string, types []string, count, page int) (*FileListResponse, error) {
	params := url.Values{}
	appendNotEmpty("user", user, params)
	appendNotEmpty("ts_from", tsFrom, params)
	appendNotEmpty("ts_to", tsTo, params)
	appendNotEmpty("types", strings.Join(types, ","), params)
	if page > 1 {
		appendNotEmpty("page", strconv.Itoa(page), params)
	}
	if count > 0 {
		appendNotEmpty("count", strconv.Itoa(count), params)
	}
	r := &FileListResponse{}
	err := s.do("files.list", params, r)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// FileInfo command
func (s *Slack) FileInfo(file string, count, page int) (*FileResponse, error) {
	params := url.Values{"file": {file}}
	if page > 1 {
		appendNotEmpty("page", strconv.Itoa(page), params)
	}
	if count > 0 {
		appendNotEmpty("count", strconv.Itoa(count), params)
	}
	r := &FileResponse{}
	err := s.do("files.info", params, r)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// FileAddComment to a given file
func (s *Slack) FileAddComment(file, comment string, setActive bool) (*CommentResponse, error) {
	params := url.Values{"file": {file}, "comment": {comment}, "set_active": {strconv.FormatBool(setActive)}}
	r := &CommentResponse{}
	err := s.do("files.comments.add", params, r)
	if err != nil {
		return nil, err
	}
	return r, nil
}
