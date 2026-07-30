package httpapi

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/ei4869/SharePipe-OSS/internal/service"
	"github.com/ei4869/SharePipe-OSS/internal/storage"
)

func (s *Server) download(response http.ResponseWriter, request *http.Request) {
	resource, err := s.service.ResolveDownload(request.Context(), request.PathValue("token"))
	if err != nil {
		respondError(response, err)
		return
	}
	s.serveResource(response, request, resource, false)
}

func (s *Server) serveResource(response http.ResponseWriter, request *http.Request, resource service.DownloadResource, preview bool) {
	disposition := "attachment"
	if preview {
		disposition = "inline"
		response.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	}
	response.Header().Set("Content-Disposition", formatDisposition(disposition, resource.Filename))
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Accept-Ranges", "bytes")
	contentType := resource.MIMEType
	if strings.HasPrefix(contentType, "text/") && !strings.Contains(contentType, "charset=") {
		contentType += "; charset=utf-8"
	}
	response.Header().Set("Content-Type", contentType)

	if resource.StreamingZip {
		if request.Header.Get("Range") != "" {
			response.Header().Set("Content-Range", "bytes */*")
			writeError(response, http.StatusRequestedRangeNotSatisfiable, "range_unsupported", "流式压缩包不支持断点续传")
			return
		}
		response.Header().Set("Accept-Ranges", "none")
		if err := s.service.WriteStreamingZip(request.Context(), resource.ShareID, response); err != nil {
			return
		}
		return
	}

	info, err := s.service.StatResource(request.Context(), resource)
	if err != nil {
		respondError(response, err)
		return
	}
	if info.ETag != "" {
		response.Header().Set("ETag", `"`+info.ETag+`"`)
		if request.Header.Get("If-None-Match") == response.Header().Get("ETag") && request.Header.Get("Range") == "" {
			response.WriteHeader(http.StatusNotModified)
			return
		}
	}
	var byteRange *storage.ByteRange
	status := http.StatusOK
	if raw := request.Header.Get("Range"); raw != "" {
		parsed, err := parseRange(raw, info.Size)
		if err != nil {
			response.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", info.Size))
			writeError(response, http.StatusRequestedRangeNotSatisfiable, "invalid_range", "请求的文件范围无效")
			return
		}
		byteRange = &parsed
		status = http.StatusPartialContent
		response.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", parsed.Start, parsed.End, info.Size))
	}
	object, err := s.service.OpenResource(request.Context(), resource, byteRange)
	if err != nil {
		respondError(response, err)
		return
	}
	defer object.Body.Close()
	response.Header().Set("Content-Length", strconv.FormatInt(object.ContentLength, 10))
	response.Header().Set("Last-Modified", info.ModTime.UTC().Format(http.TimeFormat))
	response.WriteHeader(status)
	_, _ = io.Copy(response, object.Body)
}

func parseRange(raw string, size int64) (storage.ByteRange, error) {
	if size <= 0 || !strings.HasPrefix(raw, "bytes=") || strings.Contains(raw, ",") {
		return storage.ByteRange{}, fmt.Errorf("unsupported range")
	}
	value := strings.TrimPrefix(raw, "bytes=")
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 {
		return storage.ByteRange{}, fmt.Errorf("invalid range")
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 {
			return storage.ByteRange{}, fmt.Errorf("invalid suffix range")
		}
		if suffix > size {
			suffix = size
		}
		return storage.ByteRange{Start: size - suffix, End: size - 1}, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return storage.ByteRange{}, fmt.Errorf("invalid range start")
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return storage.ByteRange{}, fmt.Errorf("invalid range end")
		}
		if end >= size {
			end = size - 1
		}
	}
	return storage.ByteRange{Start: start, End: end}, nil
}

func formatDisposition(kind, filename string) string {
	value := mime.FormatMediaType(kind, map[string]string{"filename": filename})
	if value == "" {
		return kind
	}
	return value
}
