// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 Japan7
package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
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

func uploadData(t *testing.T, api humatest.TestAPI, data []byte, filename string) UploadOutput {
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

	headers := "Content-Type: multipart/form-data; boundary=" + multipart_writer.Boundary()
	resp := assertRespCode(t,
		api.Post("/", headers, buf),
		200,
	)

	data_upload := UploadOutput{}
	dec := json.NewDecoder(resp.Body)
	dec.Decode(&data_upload.Body)

	return data_upload
}

func TestUploadDownload(t *testing.T) {
	api := getTestAPI(t)

	test_data := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
	filename := "test_file"
	upload_data := uploadData(t, api, test_data, filename)

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

	matched, _ := regexp.MatchString(
		fmt.Sprintf("filename=%s", regexp.QuoteMeta(filename)),
		content_disposition_header,
	)

	if !matched {
		t.Fatalf(
			"filename is not set in content disposition header: %s",
			content_disposition_header,
		)
	}

}
