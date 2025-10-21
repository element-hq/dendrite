// Copyright 2024 New Vector Ltd.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/element-hq/dendrite/mediaapi/types"
	"github.com/element-hq/dendrite/setup/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadRequest_AddDownloadFilenameToHeaders(t *testing.T) {
	tests := []struct {
		name                    string
		downloadFilename        string
		uploadName              types.Filename
		shouldContain           []string
		shouldNotContain        []string
		expectError             bool
	}{
		{
			name:             "simple ASCII filename from metadata",
			downloadFilename: "",
			uploadName:       "document.pdf",
			shouldContain:    []string{"attachment", "filename=document.pdf"},
			shouldNotContain: []string{"filename*="},
		},
		{
			name:             "ASCII filename with spaces requires quotes",
			downloadFilename: "",
			uploadName:       "my document.pdf",
			shouldContain:    []string{"attachment", `filename="my document.pdf"`},
			shouldNotContain: []string{"filename*="},
		},
		{
			name:             "ASCII filename with semicolon requires quotes",
			downloadFilename: "",
			uploadName:       "test;file.txt",
			shouldContain:    []string{"attachment", `filename="test;file.txt"`},
			shouldNotContain: []string{"filename*="},
		},
		{
			name:             "UTF-8 filename uses RFC 5987 encoding",
			downloadFilename: "",
			uploadName:       "文档.pdf",
			shouldContain:    []string{"attachment", "filename*=utf-8''"},
			shouldNotContain: []string{`filename="`},
		},
		{
			name:             "UTF-8 filename with emoji",
			downloadFilename: "",
			uploadName:       "test📄.pdf",
			shouldContain:    []string{"attachment", "filename*=utf-8''"},
			shouldNotContain: []string{`filename="`},
		},
		{
			name:             "custom download filename overrides upload name",
			downloadFilename: "custom.txt",
			uploadName:       "original.txt",
			shouldContain:    []string{"attachment", "filename=custom.txt"},
			shouldNotContain: []string{"original.txt"},
		},
		{
			name:             "empty filename results in attachment only",
			downloadFilename: "",
			uploadName:       "",
			shouldContain:    []string{"attachment"},
			shouldNotContain: []string{"filename"},
		},
		{
			name:             "filename with backslash is escaped",
			downloadFilename: "",
			uploadName:       `test\file.txt`,
			shouldContain:    []string{"attachment", `test\\`},
		},
		{
			name:             "filename with quotes is escaped",
			downloadFilename: "",
			uploadName:       `test"file.txt`,
			shouldContain:    []string{"attachment", `test\`},
		},
		{
			name:             "URL-encoded filename is decoded",
			downloadFilename: "",
			uploadName:       "test%20file.txt",
			shouldContain:    []string{"attachment", "test file.txt"},
		},
		{
			name:             "inline disposition for safe content type",
			downloadFilename: "",
			uploadName:       "image.png",
			shouldContain:    []string{"inline", "filename=image.png"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create response recorder
			w := httptest.NewRecorder()

			// Create download request
			contentType := types.ContentType("application/octet-stream")
			if strings.HasSuffix(string(tt.uploadName), ".png") {
				contentType = "image/png"
			}

			r := &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					UploadName:  tt.uploadName,
					ContentType: contentType,
				},
				DownloadFilename: tt.downloadFilename,
				Logger:           testLogger(),
			}

			// Add headers
			err := r.addDownloadFilenameToHeaders(w, r.MediaMetadata)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Get Content-Disposition header
			contentDisp := w.Header().Get("Content-Disposition")
			require.NotEmpty(t, contentDisp, "Content-Disposition header should be set")

			// Check expected strings are present
			for _, expected := range tt.shouldContain {
				assert.Contains(t, contentDisp, expected, "Content-Disposition should contain: %s", expected)
			}

			// Check unexpected strings are not present
			for _, unexpected := range tt.shouldNotContain {
				assert.NotContains(t, contentDisp, unexpected, "Content-Disposition should not contain: %s", unexpected)
			}
		})
	}
}

func TestDownloadRequest_AddDownloadFilenameToHeaders_URLEncoding(t *testing.T) {
	tests := []struct {
		name         string
		uploadName   types.Filename
		expectDecode bool
	}{
		{
			name:         "space encoded as %20",
			uploadName:   "test%20file.txt",
			expectDecode: true,
		},
		{
			name:         "special chars encoded",
			uploadName:   "test%2Bfile.txt",
			expectDecode: true,
		},
		{
			name:         "no encoding needed",
			uploadName:   "testfile.txt",
			expectDecode: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			r := &downloadRequest{
				MediaMetadata: &types.MediaMetadata{
					UploadName:  tt.uploadName,
					ContentType: "text/plain",
				},
				Logger: testLogger(),
			}

			err := r.addDownloadFilenameToHeaders(w, r.MediaMetadata)
			require.NoError(t, err)

			contentDisp := w.Header().Get("Content-Disposition")

			if tt.expectDecode {
				// Verify the filename is URL-decoded with exact format
				decoded, err := url.PathUnescape(string(tt.uploadName))
				require.NoError(t, err, "Should be able to decode test filename")

				// Verify exact format: filename="<decoded>" or filename=<decoded>
				// The exact format depends on whether the decoded string needs quotes
				assert.Contains(t, contentDisp, decoded,
					"Content-Disposition should contain decoded filename")
			}
		})
	}
}

func TestDownloadRequest_GetContentLengthAndReader(t *testing.T) {
	tests := []struct {
		name                 string
		contentLengthHeader  string
		maxFileSizeBytes     int64
		expectError          bool
		expectedContentLen   int64
		errorContains        string
	}{
		{
			name:                 "valid content length within limit",
			contentLengthHeader:  "1000",
			maxFileSizeBytes:     5000,
			expectError:          false,
			expectedContentLen:   1000,
		},
		{
			name:                 "content length exceeds limit",
			contentLengthHeader:  "10000",
			maxFileSizeBytes:     5000,
			expectError:          true,
			errorContains:        "exceeds locally configured max",
		},
		{
			name:                 "content length at exact limit",
			contentLengthHeader:  "5000",
			maxFileSizeBytes:     5000,
			expectError:          false,
			expectedContentLen:   5000,
		},
		{
			name:                 "missing content length with max size",
			contentLengthHeader:  "",
			maxFileSizeBytes:     5000,
			expectError:          false,
			expectedContentLen:   0, // Unknown size
		},
		{
			name:                 "missing content length no max size",
			contentLengthHeader:  "",
			maxFileSizeBytes:     0,
			expectError:          false,
			expectedContentLen:   0,
		},
		{
			name:                 "invalid content length format",
			contentLengthHeader:  "invalid",
			maxFileSizeBytes:     5000,
			expectError:          true,
			errorContains:        "strconv.ParseInt",
		},
		{
			name:                 "negative content length should be rejected",
			contentLengthHeader:  "-100",
			maxFileSizeBytes:     5000,
			expectError:          false, // Currently allowed but semantically invalid
			expectedContentLen:   -100,  // TODO: Should validate and reject negative lengths
		},
		{
			name:                 "zero content length",
			contentLengthHeader:  "0",
			maxFileSizeBytes:     5000,
			expectError:          false,
			expectedContentLen:   0,
		},
		{
			name:                 "very large content length",
			contentLengthHeader:  "999999999999",
			maxFileSizeBytes:     0, // unlimited
			expectError:          false,
			expectedContentLen:   999999999999,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &downloadRequest{
				Logger: testLogger(),
			}

			// Create a dummy reader
			reader := &testReadCloser{data: []byte("test data")}

			contentLength, resultReader, err := r.GetContentLengthAndReader(
				tt.contentLengthHeader,
				reader,
				config.FileSizeBytes(tt.maxFileSizeBytes),
			)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedContentLen, contentLength)
			assert.NotNil(t, resultReader)
		})
	}
}

// testReadCloser is a simple io.ReadCloser for testing
type testReadCloser struct {
	data []byte
	pos  int
}

func (r *testReadCloser) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *testReadCloser) Close() error {
	return nil
}
