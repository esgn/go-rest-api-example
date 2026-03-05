package middleware

import "testing"

func TestMaxBodyBytesForMaxContentLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		maxContentLength int
		want             int64
	}{
		{
			name:             "zero content length still allows minimal JSON envelope",
			maxContentLength: 0,
			want:             512,
		},
		{
			name:             "default content length",
			maxContentLength: 10000,
			want:             40512,
		},
		{
			name:             "small content length",
			maxContentLength: 100,
			want:             912,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := MaxBodyBytesForMaxContentLength(tc.maxContentLength)
			if got != tc.want {
				t.Fatalf("MaxBodyBytesForMaxContentLength(%d) = %d, want %d", tc.maxContentLength, got, tc.want)
			}
		})
	}
}

func TestDefaultMaxAfterCursorLength(t *testing.T) {
	t.Parallel()

	if DefaultMaxAfterCursorLength != 512 {
		t.Fatalf("DefaultMaxAfterCursorLength = %d, want 512", DefaultMaxAfterCursorLength)
	}
}
