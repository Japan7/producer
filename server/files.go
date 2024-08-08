// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 Japan7
package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"mime/multipart"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gabriel-vasile/mimetype"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/ironsmile/nedomi/utils/httputils"
	"github.com/minio/minio-go/v7"
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
	ContentLength int64  `header:"Content-Length"`
	ContentType   string `header:"Content-Type"`
	Expires       int64  `header:"Expires"`
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

	content_type, err := detectType(obj)
	if err != nil {
		return nil, err
	}

	return &DownloadHeadOutput{
		AcceptRange:   "bytes",
		ContentLength: stat.Size,
		ContentType:   content_type.String(),
		Expires:       stat.Expires.Unix(),
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

	return serveObject(obj, input.Range)
}

type FileSender struct {
	// Reader should be already at the Range.Start location
	Fd        io.ReadCloser
	Range     httputils.Range
	BytesRead uint64
}

func (f *FileSender) Read(buf []byte) (int, error) {
	toread := f.Range.Length - f.BytesRead
	if toread < uint64(len(buf)) {
		buf = buf[:toread]
	}
	return f.Fd.Read(buf)
}

func (f *FileSender) Close() error {
	return f.Fd.Close()
}

func detectType(obj *minio.Object) (*mimetype.MIME, error) {
	buf := make([]byte, 1024)
	_, err := obj.Read(buf)
	if err != nil {
		return nil, err
	}
	_, err = obj.Seek(0, 0)
	if err != nil {
		return nil, err
	}

	mime := mimetype.Detect(buf)
	return mime, nil
}

func serveObject(obj *minio.Object, range_header string) (*huma.StreamResponse, error) {
	stat, err := obj.Stat()

	if stat.Expires.Unix() != 0 && stat.Expires.Before(time.Now()) {
		go DeleteObject(context.Background(), stat.Key)
		return nil, huma.Error410Gone("File expired")
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			defer func() {
				r := recover()
				if r != nil {
					// unlikely, but close object on panic just in case it happens
					err = obj.Close()
					if err != nil {
						panic(err)
					}
					panic(r)
				}
			}()

			if err != nil {
				resp := minio.ToErrorResponse(err)
				if resp.Code == "NoSuchKey" {
					ctx.SetStatus(404)
				} else {
					ctx.SetStatus(500)
					getLogger().Printf("%+v\n", resp)
				}
				return
			}

			content_type, err := detectType(obj)
			if err != nil {
				return
			}
			ctx.SetHeader("Content-Type", content_type.String())

			ctx.SetHeader("Accept-Range", "bytes")

			var reqRange httputils.Range
			if range_header == "" {
				reqRange = httputils.Range{Start: 0, Length: uint64(stat.Size)}
			} else {
				ranges, err := httputils.ParseRequestRange(range_header, uint64(stat.Size))
				if err != nil {
					ctx.SetStatus(416)
					ctx.SetHeader("Content-Range", fmt.Sprintf("bytes */%d", stat.Size))
					return
				}
				reqRange = ranges[0]
				ctx.SetStatus(206)
				ctx.SetHeader("Content-Range", reqRange.ContentRange(uint64(stat.Size)))
			}

			ctx.SetHeader("Content-Length", strconv.FormatUint(reqRange.Length, 10))
			if stat.Expires.Unix() != 0 {
				ctx.SetHeader("Expires", strconv.FormatInt(stat.Expires.Unix(), 10))
			}

			_, err = obj.Seek(int64(reqRange.Start), 0)
			if err != nil {
				return
			}

			filesender := FileSender{obj, reqRange, 0}

			fiber_ctx := ctx.BodyWriter().(*fiber.Ctx)
			err = fiber_ctx.SendStream(&filesender, int(reqRange.Length))
		},
	}, err
}
