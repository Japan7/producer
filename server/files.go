// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 Japan7
package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
)

type UploadData struct {
	UploadFile multipart.File `form-data:"file" required:"true"`
}

type UploadInput struct {
	Auth    string `header:"Authorization"`
	Expires int64  `header:"Expires"`
	RawBody huma.MultipartFormFiles[UploadData]
}

type UploadOutput struct {
	Body struct {
		ID      string `json:"id"`
		URL     string `json:"url"`
		Expires int64  `json:"expires"`
	}
}

var BEARER_PREFIX = "Bearer "

func isAuthenticated(authorization string) bool {
	if CONFIG.Upload.AdminToken == "" {
		return false
	}

	if len(authorization) != len(BEARER_PREFIX)+len(CONFIG.Upload.AdminToken) {
		return false
	}

	if authorization[:len(BEARER_PREFIX)] == BEARER_PREFIX {
		header_token := authorization[len(BEARER_PREFIX):]

		return subtle.ConstantTimeCompare([]byte(header_token), []byte(CONFIG.Upload.AdminToken)) == 1
	} else {
		return false
	}
}

func Upload(ctx context.Context, input *UploadInput) (*UploadOutput, error) {
	file := input.RawBody.Form.File["file"][0]
	fd, err := file.Open()
	if err != nil {
		return nil, err
	}

	file_id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	det_buf := make([]byte, 1024)
	n, err := fd.Read(det_buf)
	if err != nil {
		return nil, err
	}
	mime := mimetype.Detect(det_buf[:n])
	fd.Seek(0, 0)

	expires := time.Now().Add(time.Duration(CONFIG.Upload.DefaultExpirationTime) * time.Second)
	if isAuthenticated(input.Auth) {
		expires = time.Unix(input.Expires, 0)
	}

	err = UploadToS3(ctx, fd, file_id.String(), file.Filename, file.Size, mime.String(), expires)
	if err != nil {
		return nil, err
	}

	resp := &UploadOutput{}
	resp.Body.ID = file_id.String()
	resp.Body.URL = fmt.Sprintf("%s/%s", CONFIG.Upload.BaseURL, file_id.String())
	resp.Body.Expires = expires.Unix()

	return resp, nil
}

type DownloadInput struct {
	Filename string `path:"id" example:"1.webm"`
	Range    string `header:"Range"`
}

type DownloadHeadOutput struct {
	AcceptRange   string `header:"Accept-Range"`
	ContentLength int    `header:"Content-Length"`
}

func DownloadHead(ctx context.Context, input *DownloadInput) (*DownloadHeadOutput, error) {
	obj, err := GetFileObject(ctx, input.Filename)

	if err != nil {
		return nil, err
	}

	stat, err := obj.Stat()
	if err != nil {
		return nil, err
	}

	return &DownloadHeadOutput{
		AcceptRange:   "bytes",
		ContentLength: int(stat.Size),
	}, nil
}

func parseRangeHeader(range_header string) (int64, int64, error) {
	before, after, _ := strings.Cut(range_header, "=")
	if before != "bytes" {
		return 0, 0, huma.Error400BadRequest("could not parse Range header.")
	}

	before, after, _ = strings.Cut(after, "/")
	// we could check the length

	before, after, _ = strings.Cut(before, "-")

	start, err := strconv.Atoi(before)
	if err != nil {
		return 0, 0, huma.Error400BadRequest("could not parse Range start integer")
	}

	end, err := strconv.Atoi(after)
	if err != nil {
		return 0, 0, huma.Error400BadRequest("could not parse Range end integer")
	}

	return int64(start), int64(end), nil
}

func Download(ctx context.Context, input *DownloadInput) (*huma.StreamResponse, error) {
	obj, err := GetFileObject(ctx, input.Filename)
	if err != nil {
		return nil, err
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			defer obj.Close()

			writer := ctx.BodyWriter()

			stat, err := obj.Stat()
			filename := url.PathEscape(stat.UserMetadata["Filename"])
			content_type := stat.UserMetadata["Type"]

			ctx.SetHeader("Accept-Range", "bytes")
			ctx.SetHeader("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"; filename*=UTF-8''%s", filename, filename))
			ctx.SetHeader("Content-Type", content_type)

			var start int64
			var end int64
			if input.Range == "" {
				start = 0
				end = stat.Size
			} else {
				start, end, err = parseRangeHeader(input.Range)
				ctx.SetStatus(206)
				ctx.SetHeader("Range", fmt.Sprintf("bytes %d-%d/%d", start, end, stat.Size))
			}

			obj.Seek(start, 0)

			bytes_to_read := end - start
			ctx.SetHeader("Content-Length", fmt.Sprintf("%d", bytes_to_read))

			var n int
			var buf []byte
			for {
				if bytes_to_read < 1024*1024 {
					buf = make([]byte, bytes_to_read)
				} else {
					buf = make([]byte, 1024*1024)
				}
				n, err = obj.Read(buf)
				writer.Write(buf[:n])
				bytes_to_read -= int64(n)
				if err != nil {
					if errors.Is(err, io.EOF) {
						err = nil
					}
					break
				}
				if bytes_to_read <= 0 {
					break
				}
			}
		},
	}, err
}
