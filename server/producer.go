// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 Japan7
package server

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func routes(api huma.API) {
	huma.Post(api, "/", Upload)
	huma.Get(api, "/{id}", Download)
	huma.Register(api, huma.Operation{
		OperationID: "head-download",
		Method:      http.MethodHead,
		Path:        "/{id}",
	}, DownloadHead)
}

func SetupProducer() (*fiber.App, huma.API) {
	app := fiber.New(fiber.Config{
		BodyLimit: 512 * 1024 * 1024,
	})

	app.Use(logger.New())

	api := humafiber.New(app, huma.DefaultConfig("Producer", "1.0.0"))
	routes(api)

	return app, api
}

func RunProducer(app *fiber.App, api huma.API) {
	listen_addr := CONFIG.Listen.Addr()
	getLogger().Printf("starting server on %s.\n", listen_addr)
	getLogger().Fatal(app.Listen(listen_addr))
}
