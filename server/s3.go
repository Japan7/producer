// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 odrling

package server

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func getS3Client() (*minio.Client, error) {
	client, err := minio.New(CONFIG.S3.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(CONFIG.S3.KeyID, CONFIG.S3.Secret, ""),
		Secure: CONFIG.S3.Secure,
	})
	return client, err
}

func UploadToS3(ctx context.Context, file io.Reader, file_id string, filename string, filesize int64) error {
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
			UserMetadata: map[string]string{"Filename": filename},
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
