package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ei4869/SharePipe-OSS/internal/core"
)

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, code, message string) {
	writeJSON(response, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func respondError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrValidation):
		writeError(response, http.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, core.ErrUnauthorized):
		writeError(response, http.StatusUnauthorized, "unauthorized", "凭据无效")
	case errors.Is(err, core.ErrForbidden):
		writeError(response, http.StatusForbidden, "forbidden", "无权执行此操作")
	case errors.Is(err, core.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", "内容不存在")
	case errors.Is(err, core.ErrExpired):
		writeError(response, http.StatusGone, "expired", "分享或授权已失效")
	case errors.Is(err, core.ErrDownloadLimit):
		writeError(response, http.StatusGone, "download_limit", "下载次数已用完")
	case errors.Is(err, core.ErrUploadIncomplete):
		writeError(response, http.StatusConflict, "upload_incomplete", "上传内容尚不完整")
	case errors.Is(err, core.ErrUnsafeUpload):
		writeError(response, http.StatusUnprocessableEntity, "unsafe_upload", "安全扫描未通过，分享已撤销")
	case errors.Is(err, core.ErrConflict):
		writeError(response, http.StatusConflict, "conflict", "当前状态不允许此操作")
	default:
		writeError(response, http.StatusInternalServerError, "internal_error", "服务器处理失败")
	}
}

func decodeJSON(response http.ResponseWriter, request *http.Request, maximum int64, target any) bool {
	request.Body = http.MaxBytesReader(response, request.Body, maximum)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_json", "请求格式不正确")
		return false
	}
	return true
}

func bearerToken(request *http.Request) string {
	const prefix = "Bearer "
	value := request.Header.Get("Authorization")
	if len(value) <= len(prefix) || value[:len(prefix)] != prefix {
		return ""
	}
	return value[len(prefix):]
}
