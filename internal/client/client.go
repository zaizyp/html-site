// Package client 是调用 html-site server 的轻量 HTTP 客户端，供 CLI 子命令使用。
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"html-site/internal/model"
)

// Client 封装远端 server 地址与 token。
type Client struct {
	BaseURL string // 形如 https://site.example.com，不含尾斜杠
	Token   string
	HTTP    *http.Client
}

// New 构造一个客户端。http 留空则用默认 DefaultClient。
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTP:    http.DefaultClient,
	}
}

// APIError 表示服务端返回的非 2xx 响应。
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("http %d: %s", e.Status, e.Body)
}

// do 发起带 token 的请求，返回响应体字节。非 2xx 转成 APIError。
func (c *Client) do(method, path string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal body: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-API-Token", c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, resp.StatusCode, &APIError{Status: resp.StatusCode, Body: string(data)}
	}
	return data, resp.StatusCode, nil
}

// ----------------------------------------------------------------------------
// 各操作封装
// ----------------------------------------------------------------------------

// Upload 新建页面。
//
// 返回 URL 的拼接优先用客户端配置的 BaseURL（客户端最清楚自己用哪个地址+端口连上来的），
// 这样即使服务端配置的 publicURL 缺端口或填错，客户端拿到的链接也能正确访问。
func (c *Client) Upload(req model.CreatePageRequest) (model.CreatePageResponse, error) {
	var out model.CreatePageResponse
	data, _, err := c.do(http.MethodPost, "/api/pages", req)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("decode response: %w", err)
	}
	// 用客户端 BaseURL 重写 URL，保证端口正确
	if out.Slug != "" {
		out.URL = c.BaseURL + "/v/" + out.Slug
	}
	return out, nil
}

// UpdateResponse 是 Update 的响应，url 用客户端 BaseURL 重写以保证端口正确。
type UpdateResponse struct {
	Page *model.Page `json:"page"`
	URL  string      `json:"url"`
}

// Update 修改页面。slug 指定页面，req 描述要改的字段。
func (c *Client) Update(slug string, req model.UpdatePageRequest) (UpdateResponse, error) {
	var out UpdateResponse
	data, _, err := c.do(http.MethodPut, "/api/pages/"+slug, req)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("decode response: %w", err)
	}
	// 用客户端 BaseURL 重写 URL，保证端口正确
	if out.Page != nil && out.Page.Slug != "" {
		out.URL = c.BaseURL + "/v/" + out.Page.Slug
	}
	return out, nil
}

// List 列出当前 owner 的全部页面。
func (c *Client) List() ([]*model.Page, error) {
	data, _, err := c.do(http.MethodGet, "/api/pages", nil)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Pages []*model.Page `json:"pages"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return wrap.Pages, nil
}

// Info 查看某页面元信息（含 content）。
type PageInfo struct {
	Page    *model.Page `json:"page"`
	Content string      `json:"content"`
}

func (c *Client) Info(slug string) (PageInfo, error) {
	var out PageInfo
	data, _, err := c.do(http.MethodGet, "/api/pages/"+slug, nil)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}

// Delete 删除某页面。
func (c *Client) Delete(slug string) error {
	_, _, err := c.do(http.MethodDelete, "/api/pages/"+slug, nil)
	return err
}
