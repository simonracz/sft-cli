package main

import (
	"crypto/sha256"
	"io"

	"golang.org/x/crypto/hkdf"
)

type Options struct {
	Password       bool
	PasswordString string
	Show           bool
	Help           bool
}

type Metadata struct {
	Description string         `json:"description"`
	Files       []UploadedFile `json:"filesMetadata"`
}

type UploadedData struct {
	Uuid   string `json:"id"`
	Secret string `json:"encryptionRawSecret"`
}

type UploadedFile struct {
	Name     string         `json:"name"`
	Size     int            `json:"size"`
	Chunks   []UploadedData `json:"chunks"`
	FileType string         `json:"type"`
}

type MessageResponse struct {
	Message string `json:"message"`
}

func keysFromKeyMaterial(keyMaterial []byte) (key []byte, iv []byte) {
	hash := sha256.New
	info := []byte("fileEncryptionKey")
	salt := make([]byte, 8)
	hkdf_key := hkdf.New(hash, keyMaterial, salt, info)
	key = make([]byte, 32)
	if _, err := io.ReadFull(hkdf_key, key); err != nil {
		panic(err)
	}

	info_iv := []byte("iv")
	salt_iv := make([]byte, 8)
	iv = make([]byte, 12)
	hkdf_iv := hkdf.New(hash, keyMaterial, salt_iv, info_iv)
	if _, err := io.ReadFull(hkdf_iv, iv); err != nil {
		panic(err)
	}
	return key, iv
}
