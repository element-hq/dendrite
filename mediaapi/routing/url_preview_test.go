// Copyright 2024 New Vector Ltd.
// Copyright 2017 Vector Creations Ltd
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package routing

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/element-hq/dendrite/internal"
	"github.com/element-hq/dendrite/internal/httputil"
	"github.com/element-hq/dendrite/internal/sqlutil"
	"github.com/element-hq/dendrite/mediaapi/fileutils"
	"github.com/element-hq/dendrite/mediaapi/storage"
	"github.com/element-hq/dendrite/mediaapi/types"
	"github.com/element-hq/dendrite/setup/config"
	userapi "github.com/element-hq/dendrite/userapi/api"
	"github.com/foxcpp/go-mockdns"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

var tests = []map[string]interface{}{
	{
		"test": `<html>
	<head>
	<title>Title</title>
	<meta property="og:title" content="test_title"/>
	<meta property="og:description" content="test_description" ></meta>
	<meta property="og:image" content="test.png">
	<meta property="og:image:url" content="test2.png"/><meta>
	<meta property="og:image:secure_url" content="test3.png">
	<meta property="og:type" content="image/jpeg" />
	<meta property="og:url" content="/image.jpg" />
	</head>
	</html>
	`,
		"expected": map[string]string{
			"og:title":            "test_title",
			"og:description":      "test_description",
			"og:image":            "test.png",
			"og:image:url":        "test2.png",
			"og:image:secure_url": "test3.png",
			"og:type":             "image/jpeg",
			"og:url":              "/image.jpg",
		},
	},
}

func Test_getMetaFieldsFromHTML(t *testing.T) {
	for _, test := range tests {
		r := &http.Response{Body: io.NopCloser(strings.NewReader(test["test"].(string)))}
		result := getMetaFieldsFromHTML(r)
		fmt.Println(result)
		for k, v := range test["expected"].(map[string]string) {
			if val, ok := result[k]; ok {
				if val != v {
					t.Errorf("Values don't match: expected %s, got %s", v, val)
				}
			} else {
				t.Errorf("Not found %s in the test HTML", k)
			}
		}
	}
}

func Test_LoadStorePreview(t *testing.T) {

	wd, err := os.Getwd()
	if err != nil {
		t.Errorf("failed to get current working directory: %v", err)
	}

	maxSize := config.FileSizeBytes(8)
	logger := log.New().WithField("mediaapi", "test")
	testdataPath := filepath.Join(wd, "./testdata")

	g := &config.Global{}
	g.Defaults(config.DefaultOpts{Generate: true})
	cfg := &config.MediaAPI{
		Matrix:            g,
		MaxFileSizeBytes:  maxSize,
		BasePath:          config.Path(testdataPath),
		AbsBasePath:       config.Path(testdataPath),
		DynamicThumbnails: false,
	}

	// create testdata folder and remove when done
	_ = os.Mkdir(testdataPath, os.ModePerm)
	defer fileutils.RemoveDir(types.Path(testdataPath), nil)
	cm := sqlutil.NewConnectionManager(nil, config.DatabaseOptions{})
	db, err := storage.NewMediaAPIDatasource(cm, &config.DatabaseOptions{
		ConnectionString:       "file::memory:?cache=shared",
		MaxOpenConnections:     100,
		MaxIdleConnections:     2,
		ConnMaxLifetimeSeconds: -1,
	})
	if err != nil {
		t.Errorf("error opening mediaapi database: %v", err)
	}

	testPreview := &types.UrlPreview{
		Title:       "test_title",
		Description: "test_description",
		ImageUrl:    "test_url.png",
		ImageType:   "image/png",
		ImageSize:   types.FileSizeBytes(100),
		ImageHeight: 100,
		ImageWidth:  100,
		Type:        "video",
		Url:         "video.avi",
	}

	hash := getHashFromString("testhash")
	device := userapi.Device{
		ID:     "1",
		UserID: "user",
	}
	err = storeUrlPreviewResponse(context.Background(), cfg, db, device, hash, testPreview, logger)
	if err != nil {
		t.Errorf("Can't store urel preview response: %v", err)
	}

	filePath, err := fileutils.GetPathFromBase64Hash(hash, cfg.AbsBasePath)
	if err != nil {
		t.Errorf("Can't get stored file path: %v", err)
	}
	_, err = os.Stat(filePath)
	if err != nil {
		t.Errorf("Can't get stored file info: %v", err)

	}

	loadedPreview, err := loadUrlPreviewResponse(context.Background(), cfg, db, hash)
	if err != nil {
		t.Errorf("Can't load the preview: %v", err)
	}

	if !reflect.DeepEqual(loadedPreview, testPreview) {
		t.Errorf("Stored and loaded previews not equal: stored=%v, loaded=%v", testPreview, loadedPreview)
	}
}

func Test_Blacklist(t *testing.T) {

	tests := map[string]interface{}{
		"entrys": []string{
			"drive.google.com",
			"https?://altavista.com/someurl",
			"https?://(www.)?google.com",
			"http://stackoverflow.com",
		},
		"tests": map[string]bool{
			"https://drive.google.com/path": true,
			"http://altavista.com":          false,
			"http://altavista.com/someurl":  true,
			"https://altavista.com/someurl": true,
			"https://stackoverflow.com":     false,
		},
	}

	cfg := &config.MediaAPI{
		UrlPreviewDenylist: tests["entrys"].([]string),
	}
	denylist := createUrlDenyList(cfg)

	for url, expected := range tests["tests"].(map[string]bool) {
		value := checkIsURLDenied(denylist, url)
		if value != expected {
			t.Errorf("Blacklist %v: expected=%v, got=%v", url, expected, value)
		}
	}
}

func Test_ActiveRequestWaiting(t *testing.T) {
	activeRequests := &types.ActiveUrlPreviewRequests{
		Url: map[string]*types.UrlPreviewResult{
			"someurl": &types.UrlPreviewResult{
				Cond:    sync.NewCond(&sync.Mutex{}),
				Preview: &types.UrlPreview{},
				Error:   nil,
			},
		},
	}

	successResults := 0
	successResultsLock := &sync.Mutex{}

	for i := 0; i < 3; i++ {
		go func() {
			if res, ok := checkActivePreviewResponse(activeRequests, "someurl"); ok {
				if res.Code != 200 {
					t.Errorf("Unsuccess result: %v", res)
				}
				successResultsLock.Lock()
				defer successResultsLock.Unlock()
				successResults++
				return
			}
			t.Errorf("url %v not found in active requests", "someurl")
		}()
	}

	time.Sleep(time.Duration(1) * time.Second)
	successResultsLock.Lock()
	if successResults != 0 {
		t.Error("Subroutines haven't waited for the result")
	}
	successResultsLock.Unlock()
	activeRequests.Url["someurl"].Cond.Broadcast()
	to := time.After(1 * time.Second)
	for {
		select {
		case <-to:
			t.Errorf("Test timed out, results=%v", successResults)
			return
		default:
		}
		successResultsLock.Lock()
		if successResults == 3 {
			break
		}
		successResultsLock.Unlock()
	}
}

func Test_UrlPreviewHandler(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Errorf("failed to get current working directory: %v", err)
	}

	maxSize := config.FileSizeBytes(1024 * 1024)
	testdataPath := filepath.Join(wd, "./testdata")

	g := &config.Global{}
	g.Defaults(config.DefaultOpts{Generate: true})
	cfg := &config.MediaAPI{
		Matrix:            g,
		MaxFileSizeBytes:  maxSize,
		BasePath:          config.Path(testdataPath),
		AbsBasePath:       config.Path(testdataPath),
		DynamicThumbnails: false,
	}
	cfg2 := &config.MediaAPI{
		Matrix:           g,
		MaxFileSizeBytes: maxSize,
		BasePath:         config.Path(testdataPath),
		AbsBasePath:      config.Path(testdataPath),
		UrlPreviewThumbnailSize: config.ThumbnailSize{
			Width:  10,
			Height: 10,
		},
		MaxThumbnailGenerators: 10,
		DynamicThumbnails:      false,
	}

	// create testdata folder and remove when done
	_ = os.Mkdir(testdataPath, os.ModePerm)
	defer fileutils.RemoveDir(types.Path(testdataPath), nil)
	cm := sqlutil.NewConnectionManager(nil, config.DatabaseOptions{})
	db, err := storage.NewMediaAPIDatasource(cm, &config.DatabaseOptions{
		ConnectionString:       "file::memory:?cache=shared",
		MaxOpenConnections:     100,
		MaxIdleConnections:     2,
		ConnMaxLifetimeSeconds: -1,
	})
	if err != nil {
		t.Errorf("error opening mediaapi database: %v", err)
	}
	db2, err2 := storage.NewMediaAPIDatasource(cm, &config.DatabaseOptions{
		ConnectionString:       "file::memory:",
		MaxOpenConnections:     100,
		MaxIdleConnections:     2,
		ConnMaxLifetimeSeconds: -1,
	})
	if err2 != nil {
		t.Errorf("error opening mediaapi database: %v", err)
	}

	activeThumbnailGeneration := &types.ActiveThumbnailGeneration{
		PathToResult: map[string]*types.ThumbnailGenerationResult{},
	}
	rateLimits := &httputil.RateLimits{}
	device := userapi.Device{
		ID:     "1",
		UserID: "user",
	}

	handler := makeUrlPreviewHandler(cfg, nil, rateLimits, db, activeThumbnailGeneration)
	// this handler is to test filecache
	handler2 := makeUrlPreviewHandler(cfg, nil, rateLimits, db, activeThumbnailGeneration)
	// this handler is to test image resize
	handler3 := makeUrlPreviewHandler(cfg2, nil, rateLimits, db2, activeThumbnailGeneration)

	responseBody := `<html>
	<head>
	<title>Title</title>
	<meta property="og:title" content="test_title"/>
	<meta property="og:description" content="test_description" ></meta>
	<meta property="og:image:url" content="/test.png">
	<meta property="og:type" content="image/jpeg" />
	<meta property="og:url" content="/image.jpg" />
	</head>
	</html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RequestURI == "/test.png" || r.RequestURI == "/test2.png" {
			w.Header().Add("Content-Type", "image/jpeg")
			http.ServeFile(w, r, "../bimg-96x96-crop.jpg")
			return
		}
		w.Write([]byte(responseBody))
	}))

	ur, _ := url.Parse("/?url=" + srv.URL)
	req := &http.Request{
		Method: "GET",
		URL:    ur,
	}
	result := handler(req, &device)
	assert.Equal(t, result.Code, 200, "Response code mismatch")
	assert.Equal(t, result.JSON.(*types.UrlPreview).Title, "test_title")
	assert.Equal(t, result.JSON.(*types.UrlPreview).ImageUrl[:6], "mxc://", "Image response not found")
	assert.Greater(t, result.JSON.(*types.UrlPreview).ImageSize, types.FileSizeBytes(0), "Image size missmatch")

	// Test only image response
	ur2, _ := url.Parse("/?url=" + srv.URL + "/test.png")
	result = handler(&http.Request{
		Method: "GET",
		URL:    ur2,
	}, &device)
	assert.Equal(t, result.Code, 200, "Response code mismatch")
	assert.Equal(t, result.JSON.(*types.UrlPreview).Title, "")
	assert.Equal(t, result.JSON.(*types.UrlPreview).ImageUrl[:6], "mxc://", "Image response not found")
	assert.Greater(t, result.JSON.(*types.UrlPreview).ImageHeight, int(0), "height missmatch")
	assert.Greater(t, result.JSON.(*types.UrlPreview).ImageWidth, int(0), "width missmatch")

	srcSize := result.JSON.(*types.UrlPreview).ImageSize
	srcHeight := result.JSON.(*types.UrlPreview).ImageHeight
	srcWidth := result.JSON.(*types.UrlPreview).ImageWidth

	// Test image resize
	ur3, _ := url.Parse("/?url=" + srv.URL + "/test2.png")
	result = handler3(&http.Request{
		Method: "GET",
		URL:    ur3,
	}, &device)
	assert.Equal(t, result.Code, 200, "Response code mismatch")
	assert.Equal(t, result.JSON.(*types.UrlPreview).ImageUrl[:6], "mxc://", "Image response not found")
	assert.Less(t, result.JSON.(*types.UrlPreview).ImageSize, srcSize, "thumbnail file size missmatch")
	assert.Less(t, result.JSON.(*types.UrlPreview).ImageHeight, srcHeight, "thumbnail height missmatch")
	assert.Less(t, result.JSON.(*types.UrlPreview).ImageWidth, srcWidth, "thumbnail width missmatch")

	// Test denied addresses

	dns := SetupFakeResolver()
	defer func(t *testing.T) {
		t.Helper()
		err = dns.Close()
		assert.NoError(t, err)
	}(t)
	defer mockdns.UnpatchNet(net.DefaultResolver)

	// this handler is to test allow/deny nets
	denyNets := []string{"192.168.1.1/24", "172.15.1.0/24"}
	allowNets := []string{"127.0.0.1/24"}
	dialer := internal.GetDialer(allowNets, denyNets, time.Duration(5*time.Second))
	handler4 := makeUrlPreviewHandler(cfg, dialer, rateLimits, db, activeThumbnailGeneration)

	serverUrlParsed, err := url.Parse(srv.URL)
	assert.NoError(t, err)
	tests := map[string]int{
		"http://deny1.example.com/test.png":                                         500,
		"http://deny2.example.com/test.png":                                         500,
		fmt.Sprintf("http://allow.example.com:%s/test.png", serverUrlParsed.Port()): 200,
	}
	for serverUrl, code := range tests {
		ur4, _ := url.Parse("/?url=" + serverUrl)
		result = handler4(&http.Request{
			Method: "GET",
			URL:    ur4,
		}, &device)
		assert.Equal(t, result.Code, code, "Deny: Response code mismatch: %s", result.JSON)
	}

	srv.Close()

	// Test in-memory cache
	result = handler(req, &device)
	assert.Equal(t, result.Code, 200, "Response code mismatch")
	assert.Equal(t, result.JSON.(*types.UrlPreview).Title, "test_title")
	assert.Equal(t, result.JSON.(*types.UrlPreview).ImageUrl[:6], "mxc://", "Image response not found")

	// Test response file cache
	result = handler2(req, &device)
	assert.Equal(t, result.Code, 200, "Response code mismatch")
	assert.Equal(t, result.JSON.(*types.UrlPreview).Title, "test_title")
	assert.Equal(t, result.JSON.(*types.UrlPreview).ImageUrl[:6], "mxc://", "Image response not found")

}

// SetupFakeResolver sets up Fake DNS server to resolve SRV records.
func SetupFakeResolver() *mockdns.Server {

	testZone := map[string]mockdns.Zone{
		"allow.example.com.": {
			A: []string{"127.0.0.1"},
		},
		"deny1.example.com.": {
			A: []string{"192.168.1.10"},
		},
		"deny2.example.com.": {
			A: []string{"172.15.1.10"},
		},
	}

	srv, _ := mockdns.NewServer(testZone, true)
	srv.PatchNet(net.DefaultResolver)

	return srv
}
