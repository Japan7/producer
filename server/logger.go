// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 Japan7
package server

import (
	"log"
	"os"
)

var _logger *log.Logger = nil

func getLogger() *log.Logger {
	if _logger == nil {
		_logger = log.New(os.Stderr, "", log.Ldate|log.Ltime|log.Lshortfile)
	}
	return _logger
}
