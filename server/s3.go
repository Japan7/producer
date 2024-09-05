// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 odrling

package server

import (
	"context"
	"io"
	"sync/atomic"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var S3_CLIENTS map[string]*minio.Client = map[string]*minio.Client{}
var client_to_pick uint64 = 0

func getS3Client() (*minio.Client, error) {
	client_i := atomic.AddUint64(&client_to_pick, 1) % uint64(len(CONFIG.S3.Endpoints))
	endpoint := CONFIG.S3.Endpoints[client_i]

	var err error = nil
	if S3_CLIENTS[endpoint] == nil {
		S3_CLIENTS[endpoint], err = minio.New(endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(CONFIG.S3.KeyID, CONFIG.S3.Secret, ""),
			Secure: CONFIG.S3.Secure,
		})
	}
	return S3_CLIENTS[endpoint], err
}

func UploadToS3(ctx context.Context, file io.Reader, file_id string, filename string, filesize int64, content_type string, expires time.Time) error {
	client, err := getS3Client()
	if err != nil {
		return err
	}

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
	client, err := getS3Client()
	if err != nil {
		return nil, err
	}

	obj, err := client.GetObject(ctx, CONFIG.S3.BucketName, filename, minio.GetObjectOptions{})
	return obj, err
}

func DeleteObject(ctx context.Context, filename string) error {
	client, err := getS3Client()
	if err != nil {
		return err
	}

	err = client.RemoveObject(ctx, CONFIG.S3.BucketName, filename, minio.RemoveObjectOptions{})
	return err
}
