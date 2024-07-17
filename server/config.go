// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2024 Japan7
package server

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
)

type ProducerListenConfig struct {
	Host string `envkey:"HOST" default:"0.0.0.0"`
	Port int    `envkey:"PORT" default:"8140"`
}

func (c ProducerListenConfig) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

type ProducerUploadConfig struct {
	BodyLimit int    `envkey:"BODY_LIMIT" default:"1073741824"`
	BaseURL   string `envkey:"BASE_URL"`
	// Default expiration time in seconds
	DefaultExpirationTime int    `envkey:"DEFAULT_EXPIRATION_TIME" default:"1800"`
	AdminToken            string `envkey:"ADMIN_TOKEN"`
}

type ProducerS3Config struct {
	// S3 host
	Endpoint string `envkey:"ENDPOINT"`
	// use HTTPS
	Secure     bool   `envkey:"SECURE"`
	KeyID      string `envkey:"KEYID"`
	Secret     string `envkey:"SECRET"`
	BucketName string `envkey:"BUCKET_NAME" default:"producer"`
}

type ProducerConfig struct {
	Listen ProducerListenConfig `env_prefix:"LISTEN"`
	Upload ProducerUploadConfig `env_prefix:"UPLOAD"`
	S3     ProducerS3Config     `env_prefix:"S3"`
}

func getEnvDefault(name string, defaultValue string) string {
	envVar := os.Getenv(name)
	if envVar != "" {
		return envVar
	}

	return defaultValue
}

func getFieldValue(field_type reflect.StructField, prefix string) string {
	envkey := field_type.Tag.Get("envkey")
	if envkey == "" {
		panic(fmt.Sprintf("envkey is not set for field %s", field_type.Name))
	}
	default_value := field_type.Tag.Get("default")
	return getEnvDefault(prefix+envkey, default_value)
}

func setConfigValue(config_value reflect.Value, config_type reflect.Type, prefix string) {
	for i := 0; i < config_type.NumField(); i++ {
		field_type := config_type.Field(i)
		field := config_value.FieldByName(field_type.Name)

		switch field_type.Type.Kind() {
		case reflect.String:
			field.SetString(getFieldValue(field_type, prefix))
		case reflect.Int:
			value := getFieldValue(field_type, prefix)
			int_value, err := strconv.Atoi(value)
			if err != nil {
				panic(err)
			}
			field.SetInt(int64(int_value))
		case reflect.Bool:
			value := getFieldValue(field_type, prefix)
			field.SetBool(value != "" && value != "false" && value != "0")
		case reflect.Struct:
			field_prefix := prefix + field_type.Tag.Get("env_prefix") + "_"
			setConfigValue(field, field_type.Type, field_prefix)
		default:
			panic(fmt.Sprintf("unknown field type for field %s: %s", field_type.Name, field_type.Type.Name()))
		}
	}
}

func getProducerConfig() ProducerConfig {
	config := ProducerConfig{}

	config_value := reflect.ValueOf(&config).Elem()
	config_type := reflect.TypeOf(config)

	setConfigValue(config_value, config_type, "PRODUCER_")

	if config.Upload.BaseURL == "" {
		// default to listen address
		config.Upload.BaseURL = "http://" + config.Listen.Addr()
		getLogger().Printf("Base URL implicitly set to %s", config.Upload.BaseURL)
	}

	return config
}

var CONFIG = getProducerConfig()
