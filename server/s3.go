// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 odrling

package server

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var S3_CLIENTS map[string]*minio.Client = map[string]*minio.Client{}
var S3_BEST_CLIENT *minio.Client

var clientsMutex = sync.Mutex{}

type TestedClient struct {
	Client      *minio.Client
	ListLatency int64
}

func initClients(ctx context.Context) {
	if len(CONFIG.S3.Endpoints) == 0 {
		panic("No S3 endpoints configured")
	}

	var err error
	for _, endpoint := range CONFIG.S3.Endpoints {
		S3_CLIENTS[endpoint], err = minio.New(endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(CONFIG.S3.KeyID, CONFIG.S3.Secret, ""),
			Secure: CONFIG.S3.Secure,
		})
		if err != nil {
			panic(err)
		}
	}

	pickBestClient(ctx)

	go func() {
		for {
			pickBestClient(ctx)
			time.Sleep(60 * time.Second)
		}
	}()
}

// We’re assuming that a garage node among one of the addresses is on the
// local host, which would have the lowest latency and probably offer the best
// bandwidth.
func pickBestClient(ctx context.Context) {
	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	var best_client *TestedClient = nil

	c := make(chan TestedClient, len(S3_CLIENTS))

	var err error
	for _, client := range S3_CLIENTS {
		go func(client *minio.Client) {
			begin_time := time.Now()
			_, err = client.ListBuckets(ctx)
			if err == nil {
				tested := TestedClient{client, time.Since(begin_time).Nanoseconds()}
				c <- tested
			}
		}(client)
	}
	timeout := 5 * time.Second

	select {
	case tested := <-c:
		if best_client == nil || tested.ListLatency < best_client.ListLatency {
			best_client = &tested
			timeout = 500 * time.Millisecond
		}
	case <-time.After(timeout):
		if best_client == nil {
			if err == nil {
				panic("timeout")
			} else {
				panic(err)
			}
		}
	}

	S3_BEST_CLIENT = best_client.Client
}

func getS3Client() *minio.Client {
	return S3_BEST_CLIENT
}

func UploadToS3(ctx context.Context, file io.Reader, file_id string, filename string, filesize int64, content_type string, expires time.Time) error {
	client := getS3Client()

	info, err := client.PutObject(ctx,
		CONFIG.S3.BucketName,
		file_id,
		file,
		filesize,
		minio.PutObjectOptions{
			UserMetadata: map[string]string{
				"Filename": filename,
				"Type":     content_type,
			},
			Expires: expires,
		},
	)
	getLogger().Printf("upload info: %+v\n", info)

	return err
}

func GetFileObject(ctx context.Context, filename string) (*minio.Object, error) {
	client := getS3Client()
	obj, err := client.GetObject(ctx, CONFIG.S3.BucketName, filename, minio.GetObjectOptions{})
	return obj, err
}

func DeleteObject(ctx context.Context, filename string) error {
	client := getS3Client()
	err := client.RemoveObject(ctx, CONFIG.S3.BucketName, filename, minio.RemoveObjectOptions{})
	return err
}
