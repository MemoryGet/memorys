package localruntime

import "testing"

func TestParseFormatMapsSupportedUploadMIMETypes(t *testing.T) {
	cases := map[string]string{
		"text/plain; charset=utf-8": "markdown",
		"text/markdown":             "markdown",
		"application/pdf":           "pdf",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   "docx",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "xlsx",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
		"image/png":  "png",
		"image/jpeg": "jpg",
		"audio/wav":  "wav",
		"audio/mpeg": "mp3",
		"audio/mp4":  "m4a",
	}
	for mediaType, want := range cases {
		if got := parseFormat(mediaType); got != want {
			t.Fatalf("parseFormat(%q) = %q, want %q", mediaType, got, want)
		}
	}
	if got := parseFormat("not a media type"); got != "bin" {
		t.Fatalf("invalid media type mapped to %q", got)
	}
}
