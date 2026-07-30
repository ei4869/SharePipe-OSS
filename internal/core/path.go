package core

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

func NormalizeRelativePath(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", fmt.Errorf("%w: path is not valid UTF-8", ErrValidation)
	}
	raw = strings.ReplaceAll(raw, "\\", "/")
	if raw == "" || strings.HasPrefix(raw, "/") || (len(raw) >= 2 && raw[1] == ':') {
		return "", fmt.Errorf("%w: path must be relative", ErrValidation)
	}
	if strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("%w: path contains a null byte", ErrValidation)
	}
	if len([]byte(raw)) > 4096 {
		return "", fmt.Errorf("%w: path is too long", ErrValidation)
	}
	segments := strings.Split(raw, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("%w: path contains an invalid segment", ErrValidation)
		}
		if len([]byte(segment)) > 255 {
			return "", fmt.Errorf("%w: path segment is too long", ErrValidation)
		}
		for _, character := range segment {
			if character < 0x20 || character == 0x7f {
				return "", fmt.Errorf("%w: path contains a control character", ErrValidation)
			}
		}
	}
	cleaned := path.Clean(strings.Join(segments, "/"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: path escapes its share", ErrValidation)
	}
	return cleaned, nil
}

func ParentDirectories(filePath string) []string {
	parts := strings.Split(filePath, "/")
	if len(parts) <= 1 {
		return nil
	}
	result := make([]string, 0, len(parts)-1)
	for index := 1; index < len(parts); index++ {
		result = append(result, strings.Join(parts[:index], "/"))
	}
	return result
}
