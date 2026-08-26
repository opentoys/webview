package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/opentoys/webview"
	"github.com/opentoys/webview/cmd/app/vfs"
)

type Config struct {
	Title     string `json:"title"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Debug     bool   `json:"debug"`
	Incognito bool   `json:"incognito"`
	DataDir   string `json:"dir"`
	Version   string `json:"version"`
	Entry     string `json:"entry"`
	Dist      string `json:"dist"`
	Scheme    string `json:"scheme"`
}

func (c *Config) defaults() {
	if c.Title == "" {
		c.Title = "App"
	}
	if c.Width <= 0 {
		c.Width = 800
	}
	if c.Height <= 0 {
		c.Height = 600
	}
	if c.Entry == "" {
		c.Entry = "index.html"
	}
	if c.Dist == "" {
		c.Dist = "dist"
	}
	if c.Scheme == "" {
		c.Scheme = "app"
	}
}

func main() {
	efs, err := vfs.NewZip("app.data")
	if err != nil {
		efs = os.DirFS("data")
	}

	buf, err := fs.ReadFile(efs, "config.json")
	if err != nil {
		log.Fatalln("read config:", err)
	}

	var cfg Config
	if err := json.Unmarshal(buf, &cfg); err != nil {
		log.Fatalln("parse config:", err)
	}
	cfg.defaults()

	w, err := webview.New(webview.Options{
		Debug:     cfg.Debug,
		Incognito: cfg.Incognito,
		DataDir:   cfg.DataDir,
	})
	if err != nil {
		log.Fatalln("create webview:", err)
	}
	defer w.Close()

	w.SetTitle(cfg.Title)
	w.SetSize(cfg.Width, cfg.Height, webview.SizeNone)

	w.Bind("app.version", func() string { return cfg.Version })
	w.Bind("app.close", func() { w.Close() })
	w.Bind("app.debug", func(msg string) { fmt.Println("[app]", msg) })

	w.InterceptResource(cfg.Scheme, serveFS(efs, cfg.Dist))
	w.Navigate(cfg.Scheme + "://host/" + cfg.Entry)

	if err := w.Run(); err != nil {
		log.Fatalln("run:", err)
	}
}

func serveFS(efs fs.FS, dist string) webview.ResourceHandler {
	return func(req webview.ResourceRequest, respond func(*webview.ResourceResponse)) {
		uri := req.URL
		if idx := strings.Index(uri, "://"); idx >= 0 {
			uri = uri[idx+3:]
		}
		if idx := strings.Index(uri, "/"); idx >= 0 {
			uri = uri[idx+1:]
		}
		if uri == "" {
			uri = "index.html"
		}

		filePath := path.Join(dist, uri)
		buf, err := fs.ReadFile(efs, filePath)
		if err != nil {
			respond(nil)
			return
		}

		respond(&webview.ResourceResponse{
			StatusCode: 200,
			Headers:    http.Header{"Content-Type": []string{detectMIME(filePath, buf)}},
			Body:       buf,
		})
	}
}

func detectMIME(name string, data []byte) string {
	ext := strings.ToLower(path.Ext(name))
	if ct, ok := mimeByExt[ext]; ok {
		return ct
	}
	return http.DetectContentType(data)
}

var mimeByExt = map[string]string{
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "application/javascript; charset=utf-8",
	".mjs":   "application/javascript; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".eot":   "application/vnd.ms-fontobject",
	".wasm":  "application/wasm",
	".mp3":   "audio/mpeg",
	".mp4":   "video/mp4",
	".webm":  "video/webm",
	".txt":   "text/plain; charset=utf-8",
	".xml":   "text/xml; charset=utf-8",
	".pdf":   "application/pdf",
	".zip":   "application/zip",
	".map":   "application/json",
}
