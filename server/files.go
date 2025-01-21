// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 Japan7
package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"github.com/ironsmile/nedomi/utils/httputils"
	"github.com/minio/minio-go/v7"
	"github.com/valyala/fasthttp"
)

type UploadData struct {
	UploadFile multipart.File `form-data:"file" required:"true"`
}

type UploadInputDefinition struct {
	Auth     string `header:"Authorization"`
	Expires  int64  `header:"Expires"`
	FileName string `header:"Filename"`
	RawBody  huma.MultipartFormFiles[UploadData]
}

type UploadInput struct {
	Auth     string `header:"Authorization"`
	Expires  int64  `header:"Expires"`
	FileName string `header:"Filename"`
	File     UploadTempFile
}

var _ huma.Resolver = (*UploadInput)(nil)

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

type UploadTempFile struct {
	Fd    *os.File
	Size  int64
	CRC32 uint32
}

func CreateTempFile(ctx context.Context, tempfile *UploadTempFile, reader io.Reader) error {
	fd, err := os.CreateTemp("", "karaberus-*")
	if err != nil {
		return err
	}

	hasher := crc32.NewIEEE()
	// roughly io.Copy but with a small buffer
	// don't change mindlessly
	buf := make([]byte, 1024*8)
	for {
		n, err := reader.Read(buf)
		if errors.Is(err, io.EOF) {
			if n == 0 {
				break
			}
		} else if err != nil {
			return err
		}
		_, err = hasher.Write(buf[:n])
		if err != nil {
			return err
		}
		_, err = fd.Write(buf[:n])
		if err != nil {
			return err
		}
	}

	tempfile.CRC32 = hasher.Sum32()
	tempfile.Fd = fd

	stat, err := fd.Stat()
	if err != nil {
		return err
	}
	tempfile.Size = stat.Size()

	_, err = fd.Seek(0, 0)
	if err != nil {
		return err
	}

	return nil
}

func (i *UploadInput) Resolve(ctx huma.Context) []error {
	err := CreateTempFile(ctx.Context(), &i.File, ctx.BodyReader())
	if err != nil {
		return []error{err}
	}
	return nil
}

func Upload(ctx context.Context, input *UploadInput) (*UploadOutput, error) {
	fd := input.File.Fd

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

	err = UploadToS3(ctx, fd, file_id.String(), input.FileName, input.File.Size, mime.String(), expires)
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
	Filename    string `path:"id" example:"1.webm"`
	Range       string `header:"Range"`
	IfNoneMatch string `header:"If-None-Match"`
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

func Download(ctx context.Context, input *DownloadInput) (*huma.StreamResponse, error) {
	return serveObject(input.Filename, input.Range, input.IfNoneMatch)
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
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	_, err = obj.Seek(0, 0)
	if err != nil {
		return nil, err
	}

	mime := mimetype.Detect(buf)
	return mime, nil
}

func serveObject(filename string, range_header string, if_none_match string) (*huma.StreamResponse, error) {

	// if stat.Expires.Unix() != 0 && stat.Expires.Before(time.Now()) {
	// 	go DeleteObject(context.Background(), stat.Key)
	// 	return nil, huma.Error410Gone("File expired")
	// }

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			fiber_ctx := ctx.BodyWriter().(*fasthttp.RequestCtx)
			obj, err := GetFileObject(context.Background(), filename)
			if err != nil {
				ctx.SetStatus(500)
				return
			}

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

			stat, err := obj.Stat()
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

			if stat.ETag == if_none_match {
				ctx.SetStatus(304)
				return
			}

			content_type, err := detectType(obj)
			if err != nil {
				println(err.Error())
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
					println(err.Error())
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

			ctx.SetHeader("ETag", stat.ETag)
			ctx.SetHeader("Last-Modified", stat.LastModified.Format(http.TimeFormat))

			_, err = obj.Seek(int64(reqRange.Start), 0)
			if err != nil {
				println(err.Error())
				return
			}

			filesender := FileSender{obj, reqRange, 0}

			fiber_ctx.SetBodyStream(&filesender, int(reqRange.Length))
		},
	}, nil
}
