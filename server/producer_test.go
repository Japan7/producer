// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 Japan7
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/gabriel-vasile/mimetype"
)

func getTestAPI(t *testing.T) humatest.TestAPI {
	_, api := humatest.New(t)

	routes(api)

	return api
}

func assertRespCode(t *testing.T, resp *httptest.ResponseRecorder, expected_code int) *httptest.ResponseRecorder {
	if resp.Code != expected_code {
		t.Fatal("returned an invalid status code", resp.Code)
	}
	return resp
}

func uploadData(t *testing.T, api humatest.TestAPI, data []byte, filename string, admin bool, expires int) UploadOutput {
	buf := new(bytes.Buffer)
	multipart_writer := multipart.NewWriter(buf)
	fwriter, err := multipart_writer.CreateFormFile("file", filename)
	if err != nil {
		panic("failed to create multipart file")
	}

	_, err = fwriter.Write(data)
	if err != nil {
		panic(err)
	}

	multipart_writer.Close()

	content_type_headers := "Content-Type: multipart/form-data; boundary=" + multipart_writer.Boundary()
	var resp *httptest.ResponseRecorder
	if admin {
		resp = assertRespCode(t,
			api.Post("/",
				content_type_headers,
				fmt.Sprintf("Authorization: Bearer %s", CONFIG.Upload.AdminToken),
				fmt.Sprintf("Expires: %d", expires),
				buf,
			),
			200,
		)
	} else {
		resp = assertRespCode(t,
			api.Post("/", content_type_headers, buf),
			200,
		)
	}

	data_upload := UploadOutput{}
	dec := json.NewDecoder(resp.Body)
	dec.Decode(&data_upload.Body)

	return data_upload
}

func TestUploadDownload(t *testing.T) {
	api := getTestAPI(t)

	test_data := []byte("hello world")
	mime := mimetype.Detect(test_data)
	filename := "test file"
	upload_data := uploadData(t, api, test_data, filename, false, 0)

	expected_expires := time.Now().Add(time.Duration(CONFIG.Upload.DefaultExpirationTime) * time.Second).Unix()

	if expected_expires+1 < upload_data.Body.Expires || expected_expires-1 > upload_data.Body.Expires {
		t.Fatalf("Expected expire value of %d (+-1), found %d", expected_expires, upload_data.Body.Expires)
	}

	path := fmt.Sprintf("/%s", upload_data.Body.ID)
	resp := assertRespCode(t, api.Get(path), 200)

	data := make([]byte, 1024)
	n, err := resp.Body.Read(data)
	if err != nil {
		panic(err)
	}

	if n != len(test_data) {
		t.Fatal("downloaded data has a different size")
	}

	if bytes.Compare(test_data, data[:n]) != 0 {
		t.Fatal("downloaded data is different")
	}

	res := resp.Result()
	content_disposition_header := res.Header["Content-Disposition"][0]
	content_type := res.Header["Content-Type"][0]

	if content_type == "" {
		t.Fatal("Content-Type header is not set")
	}

	if content_type != mime.String() {
		t.Fatalf("Content-Type header is not the detected file type: %s != %s", content_type, mime.String())
	}

	matched, _ := regexp.MatchString(
		fmt.Sprintf("filename=\"%s\"", regexp.QuoteMeta(url.PathEscape(filename))),
		content_disposition_header,
	)

	if !matched {
		t.Fatalf(
			"filename is not set in content disposition header: %s",
			content_disposition_header,
		)
	}

}

func TestAdminUploadDownload(t *testing.T) {
	api := getTestAPI(t)

	CONFIG.Upload.AdminToken = "verysecuretoken"

	test_data := []byte("hello world")
	mime := mimetype.Detect(test_data)
	filename := "test file"
	upload_data := uploadData(t, api, test_data, filename, true, 0)

	expected_expires := int64(0)

	if expected_expires+1 < upload_data.Body.Expires || expected_expires-1 > upload_data.Body.Expires {
		t.Fatalf("Expected expire value of %d (+-1), found %d", expected_expires, upload_data.Body.Expires)
	}

	path := fmt.Sprintf("/%s", upload_data.Body.ID)
	resp := assertRespCode(t, api.Get(path), 200)

	data := make([]byte, 1024)
	n, err := resp.Body.Read(data)
	if err != nil {
		panic(err)
	}

	if n != len(test_data) {
		t.Fatal("downloaded data has a different size")
	}

	if bytes.Compare(test_data, data[:n]) != 0 {
		t.Fatal("downloaded data is different")
	}

	res := resp.Result()
	content_disposition_header := res.Header["Content-Disposition"][0]
	content_type := res.Header["Content-Type"][0]

	if content_type == "" {
		t.Fatal("Content-Type header is not set")
	}

	if content_type != mime.String() {
		t.Fatalf("Content-Type header is not the detected file type: %s != %s", content_type, mime.String())
	}

	matched, _ := regexp.MatchString(
		fmt.Sprintf("filename=\"%s\"", regexp.QuoteMeta(url.PathEscape(filename))),
		content_disposition_header,
	)

	if !matched {
		t.Fatalf(
			"filename is not set in content disposition header: %s",
			content_disposition_header,
		)
	}

}
