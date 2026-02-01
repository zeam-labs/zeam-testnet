package content

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
)

type SiteManifest struct {
	Version   int                    `json:"version"`
	RootPath  string                 `json:"root_path"`
	Files     map[string]SiteFile    `json:"files"`
	TotalSize uint64                 `json:"total_size"`
}

type SiteFile struct {
	ContentID   string `json:"content_id"`
	Size        uint64 `json:"size"`
	ContentType string `json:"content_type"`
}

type WebsitePublisher struct {
	dag   *DAGBuilder
	store *Store
}

func NewWebsitePublisher(dag *DAGBuilder, store *Store) *WebsitePublisher {
	return &WebsitePublisher{
		dag:   dag,
		store: store,
	}
}

func (wp *WebsitePublisher) PublishTarGz(data []byte) (ContentID, *SiteManifest, error) {

	gzReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return ContentID{}, nil, fmt.Errorf("invalid gzip: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	manifest := &SiteManifest{
		Version:  1,
		RootPath: "/",
		Files:    make(map[string]SiteFile),
	}

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ContentID{}, nil, fmt.Errorf("tar read error: %w", err)
		}

		if header.Typeflag == tar.TypeDir {
			continue
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		fileData, err := io.ReadAll(io.LimitReader(tarReader, 50*1024*1024))
		if err != nil {
			return ContentID{}, nil, fmt.Errorf("read file %s: %w", header.Name, err)
		}

		contentID, err := wp.dag.Add(fileData)
		if err != nil {
			return ContentID{}, nil, fmt.Errorf("store file %s: %w", header.Name, err)
		}

		path := normalizePath(header.Name)

		contentType := detectContentType(path)

		manifest.Files[path] = SiteFile{
			ContentID:   contentID.Hex(),
			Size:        uint64(len(fileData)),
			ContentType: contentType,
		}
		manifest.TotalSize += uint64(len(fileData))
	}

	if len(manifest.Files) == 0 {
		return ContentID{}, nil, fmt.Errorf("archive contains no files")
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return ContentID{}, nil, fmt.Errorf("marshal manifest: %w", err)
	}

	manifestID, err := wp.dag.Add(manifestJSON)
	if err != nil {
		return ContentID{}, nil, fmt.Errorf("store manifest: %w", err)
	}

	return manifestID, manifest, nil
}

func (wp *WebsitePublisher) GetFile(manifestID ContentID, path string) ([]byte, string, error) {

	manifestData, err := wp.dag.Get(manifestID)
	if err != nil {
		return nil, "", fmt.Errorf("get manifest: %w", err)
	}

	var manifest SiteManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, "", fmt.Errorf("parse manifest: %w", err)
	}

	path = normalizePath(path)
	if path == "" || path == "/" {
		path = "index.html"
	}

	file, ok := manifest.Files[path]
	if !ok {

		if !strings.HasSuffix(path, "/") {
			file, ok = manifest.Files[path+"/index.html"]
		}
		if !ok {
			file, ok = manifest.Files[path+"index.html"]
		}
		if !ok {
			return nil, "", fmt.Errorf("file not found: %s", path)
		}
	}

	contentID, err := ParseContentID(file.ContentID)
	if err != nil {
		return nil, "", fmt.Errorf("invalid content ID: %w", err)
	}

	data, err := wp.dag.Get(contentID)
	if err != nil {
		return nil, "", fmt.Errorf("get content: %w", err)
	}

	return data, file.ContentType, nil
}

func (wp *WebsitePublisher) GetManifest(manifestID ContentID) (*SiteManifest, error) {
	data, err := wp.dag.Get(manifestID)
	if err != nil {
		return nil, err
	}

	var manifest SiteManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

func normalizePath(path string) string {

	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")

	path = filepath.Clean(path)

	path = strings.ReplaceAll(path, "\\", "/")

	return path
}

func detectContentType(path string) string {
	ext := filepath.Ext(path)

	switch strings.ToLower(ext) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".webp":
		return "image/webp"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	case ".otf":
		return "font/otf"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".pdf":
		return "application/pdf"
	case ".xml":
		return "application/xml"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".wasm":
		return "application/wasm"
	default:

		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return mimeType
		}
		return "application/octet-stream"
	}
}
