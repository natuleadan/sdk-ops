package bunny

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// --- Video Library (from Core API) ---

type VideoLibrary struct {
	ID            int64  `json:"Id"`
	Name          string `json:"Name"`
	APIKey        string `json:"ApiKey,omitempty"`
	DateCreated   string `json:"DateCreated,omitempty"`
	VideoCount    int    `json:"VideoCount,omitempty"`
	TotalStorage  int64  `json:"TotalStorage,omitempty"`
	EnableTranscribing bool `json:"EnableTranscribing,omitempty"`
}

type AddVideoLibraryModel struct {
	Name string `json:"Name"`
}

// --- Video Objects (from Stream API) ---

type Video struct {
	ID              string `json:"id"`
	GUID            string `json:"guid"`
	Title           string `json:"title"`
	Status          int    `json:"status"` // 0=queued, 1=processing, 2=ready, 3=failed
	DateCreated     string `json:"dateCreated,omitempty"`
	DateUploaded    string `json:"dateUploaded,omitempty"`
	Length          int    `json:"length,omitempty"`
	Framerate       int    `json:"framerate,omitempty"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	AvailableResolutions string `json:"availableResolutions,omitempty"`
	ThumbnailCount  int    `json:"thumbnailCount,omitempty"`
	EncodeProgress  int    `json:"encodeProgress,omitempty"`
	StorageSize     int64  `json:"storageSize,omitempty"`
	Views           int    `json:"views,omitempty"`
}

type CreateVideoResponse struct {
	GUID            string `json:"guid"`
	Title           string `json:"title"`
	Status          int    `json:"status"`
	VideoLibraryID  int    `json:"videoLibraryId"`
	DateCreated     string `json:"dateCreated"`
	UploadURL       string `json:"uploadUrl"`
}

type VideoCollection struct {
	ID          int64  `json:"id"`
	LibraryID   int64  `json:"videoLibraryId"`
	Name        string `json:"name"`
	VideoCount  int    `json:"videoCount,omitempty"`
}

type FetchVideoRequest struct {
	URL string `json:"url"`
}

type ThumbnailRequest struct {
	ThumbnailURL string `json:"thumbnailUrl"`
}

// --- Library CRUD (Core API, uses main API key) ---

func (c *Client) ListVideoLibraries(ctx context.Context) ([]VideoLibrary, error) {
	var libs []VideoLibrary
	err := c.Get(ctx, APICore, "/videolibrary", &libs)
	if err != nil {
		return nil, err
	}
	return libs, nil
}

func (c *Client) CreateVideoLibrary(ctx context.Context, name string) (*VideoLibrary, error) {
	var lib VideoLibrary
	err := c.Post(ctx, APICore, "/videolibrary", AddVideoLibraryModel{Name: name}, &lib)
	if err != nil {
		return nil, err
	}
	return &lib, nil
}

func (c *Client) GetVideoLibrary(ctx context.Context, id int64) (*VideoLibrary, error) {
	var lib VideoLibrary
	err := c.Get(ctx, APICore, fmt.Sprintf("/videolibrary/%d", id), &lib)
	if err != nil {
		return nil, err
	}
	return &lib, nil
}

func (c *Client) DeleteVideoLibrary(ctx context.Context, id int64) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/videolibrary/%d", id), nil)
}

// --- Video Operations (Stream API, uses library API key) ---

func (c *Client) streamDo(ctx context.Context, libID int64, accessKey, method, path string, body, dst any) error {
	url := fmt.Sprintf("https://video.bunnycdn.com/library/%d%s", libID, path)

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("stream marshal: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("stream request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if accessKey != "" {
		req.Header.Set("AccessKey", accessKey)
	} else if c.apiKey != "" {
		req.Header.Set("AccessKey", c.apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("stream do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("stream: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if dst != nil {
		return json.NewDecoder(resp.Body).Decode(dst)
	}
	return nil
}

func (c *Client) GetLibraryAPIKey(ctx context.Context, libID int64) (string, error) {
	lib, err := c.GetVideoLibrary(ctx, libID)
	if err != nil {
		return "", err
	}
	if lib.APIKey == "" {
		return "", fmt.Errorf("library %d has no API key", libID)
	}
	return lib.APIKey, nil
}

func (c *Client) CreateVideo(ctx context.Context, libID int64, accessKey, title string) (*CreateVideoResponse, error) {
	var resp CreateVideoResponse
	err := c.streamDo(ctx, libID, accessKey, "POST", "/videos", map[string]string{"title": title}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListVideos(ctx context.Context, libID int64, accessKey string, page, perPage int) ([]Video, error) {
	var resp struct {
		Items      []Video `json:"items"`
		TotalItems int     `json:"totalItems"`
	}
	path := fmt.Sprintf("/videos?page=%d&itemsPerPage=%d", page, perPage)
	err := c.streamDo(ctx, libID, accessKey, "GET", path, nil, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Items == nil {
		return []Video{}, nil
	}
	return resp.Items, nil
}

func (c *Client) GetVideo(ctx context.Context, libID int64, accessKey, videoID string) (*Video, error) {
	var video Video
	err := c.streamDo(ctx, libID, accessKey, "GET", "/videos/"+videoID, nil, &video)
	if err != nil {
		return nil, err
	}
	return &video, nil
}

func (c *Client) DeleteVideo(ctx context.Context, libID int64, accessKey, videoID string) error {
	return c.streamDo(ctx, libID, accessKey, "DELETE", "/videos/"+videoID, nil, nil)
}

func (c *Client) FetchVideo(ctx context.Context, libID int64, accessKey, url string) (*Video, error) {
	var video Video
	err := c.streamDo(ctx, libID, accessKey, "POST", "/videos/fetch", FetchVideoRequest{URL: url}, &video)
	if err != nil {
		return nil, err
	}
	return &video, nil
}

func (c *Client) SetVideoThumbnail(ctx context.Context, libID int64, accessKey, videoID, thumbnailURL string) error {
	return c.streamDo(ctx, libID, accessKey, "POST", "/videos/"+videoID+"/thumbnail", ThumbnailRequest{ThumbnailURL: thumbnailURL}, nil)
}

func (c *Client) ReencodeVideo(ctx context.Context, libID int64, accessKey, videoID string) error {
	return c.streamDo(ctx, libID, accessKey, "POST", "/videos/"+videoID+"/reencode", nil, nil)
}

func (c *Client) ListCollections(ctx context.Context, libID int64, accessKey string) ([]VideoCollection, error) {
	var resp struct {
		Items      []VideoCollection `json:"items"`
		TotalItems int               `json:"totalItems"`
	}
	err := c.streamDo(ctx, libID, accessKey, "GET", "/collections", nil, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Items == nil {
		return []VideoCollection{}, nil
	}
	return resp.Items, nil
}

func (c *Client) CreateCollection(ctx context.Context, libID int64, accessKey, name string) (*VideoCollection, error) {
	var col VideoCollection
	err := c.streamDo(ctx, libID, accessKey, "POST", "/collections", map[string]string{"name": name}, &col)
	if err != nil {
		return nil, err
	}
	return &col, nil
}

func (c *Client) GetCollection(ctx context.Context, libID int64, accessKey string, collectionID int64) (*VideoCollection, error) {
	var col VideoCollection
	err := c.streamDo(ctx, libID, accessKey, "GET", fmt.Sprintf("/collections/%d", collectionID), nil, &col)
	if err != nil {
		return nil, err
	}
	return &col, nil
}

func (c *Client) UpdateCollection(ctx context.Context, libID int64, accessKey string, collectionID int64, name string) (*VideoCollection, error) {
	var col VideoCollection
	err := c.streamDo(ctx, libID, accessKey, "POST", fmt.Sprintf("/collections/%d", collectionID),
		map[string]string{"name": name}, &col)
	if err != nil {
		return nil, err
	}
	return &col, nil
}

func (c *Client) DeleteCollection(ctx context.Context, libID int64, accessKey string, collectionID int64) error {
	return c.streamDo(ctx, libID, accessKey, "DELETE", fmt.Sprintf("/collections/%d", collectionID), nil, nil)
}

func (c *Client) UpdateVideo(ctx context.Context, libID int64, accessKey, videoID string, updates map[string]any) (*Video, error) {
	var video Video
	err := c.streamDo(ctx, libID, accessKey, "POST", "/videos/"+videoID, updates, &video)
	if err != nil {
		return nil, err
	}
	return &video, nil
}

func (c *Client) GetVideoHeatmap(ctx context.Context, libID int64, accessKey, videoID string) ([]map[string]any, error) {
	var resp []map[string]any
	err := c.streamDo(ctx, libID, accessKey, "GET", "/videos/"+videoID+"/heatmap", nil, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetVideoPlayData(ctx context.Context, libID int64, accessKey, videoID string) (*map[string]any, error) {
	var resp map[string]any
	err := c.streamDo(ctx, libID, accessKey, "GET", "/videos/"+videoID+"/play", nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetVideoStatistics(ctx context.Context, libID int64, accessKey string) (*map[string]any, error) {
	var resp map[string]any
	err := c.streamDo(ctx, libID, accessKey, "GET", "/statistics", nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) AddOutputCodec(ctx context.Context, libID int64, accessKey, videoID string, outputCodecID int64) error {
	return c.streamDo(ctx, libID, accessKey, "PUT",
		fmt.Sprintf("/videos/%s/outputs/%d", videoID, outputCodecID), nil, nil)
}

func (c *Client) RepackageVideo(ctx context.Context, libID int64, accessKey, videoID string) error {
	return c.streamDo(ctx, libID, accessKey, "POST", "/videos/"+videoID+"/repackage", nil, nil)
}

func (c *Client) GetVideoResolutions(ctx context.Context, libID int64, accessKey, videoID string) ([]map[string]any, error) {
	var resp []map[string]any
	err := c.streamDo(ctx, libID, accessKey, "GET", "/videos/"+videoID+"/resolutions", nil, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetVideoStorageSize(ctx context.Context, libID int64, accessKey, videoID string) (*map[string]any, error) {
	var resp map[string]any
	err := c.streamDo(ctx, libID, accessKey, "GET", "/videos/"+videoID+"/storage", nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) CleanupResolutions(ctx context.Context, libID int64, accessKey, videoID string) error {
	return c.streamDo(ctx, libID, accessKey, "POST", "/videos/"+videoID+"/resolutions/cleanup", nil, nil)
}

func (c *Client) AddCaption(ctx context.Context, libID int64, accessKey, videoID, srclang, label string, captionsFile io.Reader) error {
	url := fmt.Sprintf("https://video.bunnycdn.com/library/%d/videos/%s/captions/%s", libID, videoID, srclang)
	req, err := http.NewRequestWithContext(ctx, "POST", url, captionsFile)
	if err != nil {
		return fmt.Errorf("add caption request: %w", err)
	}
	req.Header.Set("AccessKey", accessKey)
	if c.apiKey != "" {
		req.Header.Set("AccessKey", c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("add caption do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("add caption: HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) DeleteCaption(ctx context.Context, libID int64, accessKey, videoID, srclang string) error {
	return c.streamDo(ctx, libID, accessKey, "DELETE",
		fmt.Sprintf("/videos/%s/captions/%s", videoID, srclang), nil, nil)
}
