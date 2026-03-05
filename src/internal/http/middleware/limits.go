package middleware

const (
	// UTF-8 runes can be up to 4 bytes each.
	maxUTF8BytesPerRune int64 = 4
	// Buffer for JSON syntax/field-name overhead around the content payload.
	requestBodyJSONOverheadBytes int64 = 512

	// DefaultMaxAfterCursorLength caps `after` query length in transport validation.
	DefaultMaxAfterCursorLength = 512
)

// MaxBodyBytesForMaxContentLength computes a request-body byte cap from the
// configured max content character length.
func MaxBodyBytesForMaxContentLength(maxContentLength int) int64 {
	return int64(maxContentLength)*maxUTF8BytesPerRune + requestBodyJSONOverheadBytes
}
