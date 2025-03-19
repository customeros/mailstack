package utils

import "strings"

func GetFileExtensionFromContentType(contentType string) string {
	// Convert content type to lowercase for consistency
	contentType = strings.ToLower(contentType)

	// Map common MIME types to simple folder names
	switch {
	case strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg"):
		return "jpg"
	case strings.Contains(contentType, "png"):
		return "png"
	case strings.Contains(contentType, "svg"):
		return "svg"
	case strings.Contains(contentType, "gif"):
		return "gif"
	case strings.Contains(contentType, "pdf"):
		return "pdf"
	case strings.Contains(contentType, "word") || strings.Contains(contentType, "doc"):
		return "docx"
	case strings.Contains(contentType, "excel") || strings.Contains(contentType, "xls"):
		return "xlsx"
	case strings.Contains(contentType, "powerpoint") || strings.Contains(contentType, "ppt"):
		return "pptx"
	case strings.Contains(contentType, "text/plain"):
		return "txt"
	case strings.Contains(contentType, "html"):
		return "html"
	case strings.Contains(contentType, "zip") || strings.Contains(contentType, "compressed"):
		return "zip"
	case strings.Contains(contentType, "webp"):
		return "webp"
	case strings.Contains(contentType, "tiff") || strings.Contains(contentType, "tif"):
		return "tiff"
	case strings.Contains(contentType, "bmp"):
		return "bmp"
	case strings.Contains(contentType, "heif") || strings.Contains(contentType, "heic"):
		return "heic"
	case strings.Contains(contentType, "csv"):
		return "csv"
	case strings.Contains(contentType, "json"):
		return "json"
	case strings.Contains(contentType, "xml"):
		return "xml"
	case strings.Contains(contentType, "markdown") || strings.Contains(contentType, "md"):
		return "md"
	case strings.Contains(contentType, "audio"):
		return "audio"
	case strings.Contains(contentType, "video"):
		return "video"
	case strings.Contains(contentType, "calendar") || strings.Contains(contentType, "ics"):
		return "ics"
	case strings.Contains(contentType, "vcard") || strings.Contains(contentType, "vcf"):
		return "vcf"
	case strings.Contains(contentType, "rar"):
		return "rar"
	case strings.Contains(contentType, "7z"):
		return "7z"
	case strings.Contains(contentType, "rtf"):
		return "rtf"
	default:
		return "other"
	}
}
