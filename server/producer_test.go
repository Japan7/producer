package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

func runTestServer() *httptest.Server {
	CONFIG.Listen.Host = "127.0.0.1"
	CONFIG.Listen.Port = 12030
	CONFIG.Upload.BaseURL = fmt.Sprintf("http://%s", CONFIG.Listen.Addr())

	backend := s3mem.New()
	faker := gofakes3.New(backend)
	ts := httptest.NewServer(faker.Server())

	CONFIG.S3.Endpoints = []string{ts.URL[len("http://"):]}
	CONFIG.S3.Secure = false
	CONFIG.S3.KeyID = ""
	CONFIG.S3.Secret = ""
	CONFIG.S3.BucketName = "producer"
	CONFIG.Upload.Tokens = []string{"aaaaaaaa"}

	err := backend.CreateBucket(CONFIG.S3.BucketName)
	if err != nil {
		panic(err)
	}

	app, api := SetupProducer()
	go RunProducer(app, api)
	return ts
}

func TestMain(m *testing.M) {
    ts := runTestServer()
    defer ts.Close()
    code := m.Run()
    os.Exit(code)
}

type TestUploadInfo struct {
	URL string `json:"url"`
}

func TestUpload(t *testing.T) {
	test_bytes := []byte("aabcde")

	var err error = nil
	var resp *http.Response
	for range 10 {
		b := bytes.NewBuffer(test_bytes)
		url := fmt.Sprintf("http://%s/", CONFIG.Listen.Addr())
		req, err := http.NewRequest(http.MethodPost, url, b)

		if err != nil {
			panic(err)
		}

		authorization := BEARER_PREFIX + CONFIG.Upload.Tokens[0]
		req.Header.Add("Authorization", authorization)

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(time.Millisecond * 100)
		} else {
			if resp.StatusCode != 200 {
				t.Fatalf("Upload request returned status code %d", resp.StatusCode)
			}
			break
		}

	}

	if err != nil {
		panic(err)
	}

	if resp.StatusCode != 200 {
		resp_data := make([]byte, 1024*8)
		resp.Body.Read(resp_data)
		panic(string(resp_data))
	}

	dec := json.NewDecoder(resp.Body)
	data := &TestUploadInfo{}
	err = dec.Decode(data)
	if err != nil {
		panic(err)
	}

	println(data.URL)
	resp2, err := http.Get(data.URL)
	if err != nil {
		panic(err)
	}

	if resp2.StatusCode != 200 {
		resp_data := make([]byte, 1024*8)
		resp2.Body.Read(resp_data)
		panic(string(resp_data))
	}

	resp_body := make([]byte, 1024)
	n, err := resp2.Body.Read(resp_body)
	if err != nil && !errors.Is(err, io.EOF) {
		panic(err)
	}

	if n != len(test_bytes) {
		t.Fatal("received", n, "bytes when we sent", len(test_bytes))
	}

	for i, b := range resp_body[:n] {
		if b != test_bytes[i] {
			t.Fatalf("pos %d byte is different from expected: %d != %d", i, b, test_bytes[i])
		}
	}
}

func TestUnauthenticatedUpload(t *testing.T) {
	test_bytes := []byte("aabcde")

	var resp *http.Response
	for range 10 {
		b := bytes.NewBuffer(test_bytes)
		url := fmt.Sprintf("http://%s/", CONFIG.Listen.Addr())
		req, err := http.NewRequest(http.MethodPost, url, b)

		if err != nil {
			panic(err)
		}

		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(time.Millisecond * 100)
		} else {
			if resp.StatusCode != 401 {
				t.Fatalf("Unauthenticated upload request returned status code %d", resp.StatusCode)
			}
			break
		}

	}
}
