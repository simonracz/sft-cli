package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"
)

func encryptFiles(files []string, options *Options) string {
	const url = "https://filetransfer.kpn.com/download/"
	maxSize := getMaxUploadSize()
	checkFiles(files, maxSize)
	readPasswordIfNeeded(options)
	transfer := createUploadRequest()
	uploadedFiles := uploadFiles(files, &transfer)
	uploadedData := uploadMetadata(uploadedFiles, &transfer)
	return url + uploadedData.Uuid + "#" + uploadedData.Secret
}

func uploadMetadata(uploadedFiles []UploadedFile, transfer *Transfer) UploadedData {
	const url = "https://filetransfer.kpn.com/api/v1/upload/metadata/"
	metadata := Metadata{
		"",
		uploadedFiles,
	}

	data, err := json.Marshal(metadata)
	if err != nil {
		panic(err)
	}

	cipherText, encryptionData := encryptData(data)

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "blob")
	if err != nil {
		panic(err)
	}
	part.Write(cipherText)
	err = writer.WriteField("transfer_management_token", transfer.Token)
	if err != nil {
		panic(err)
	}
	err = writer.Close()
	if err != nil {
		panic(err)
	}
	request, err := http.NewRequest("PUT", url, body)
	if err != nil {
		panic(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	statusOK := response.StatusCode >= 200 && response.StatusCode < 300
	if !statusOK {
		fmt.Println(response)
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			panic(err)
		}
		fmt.Println(string(responseBody))
		panic("Upload metadata failed")
	}
	base64key := base64.URLEncoding.EncodeToString(encryptionData.KeyMaterial)
	base64key = strings.ReplaceAll(base64key, "=", ".")

	return UploadedData{
		transfer.Uuid,
		base64key,
	}
}

type MaxUploadSize struct {
	MaxSize int `json:"max_upload_size_bytes"`
}

func getMaxUploadSize() int {
	const url = "https://filetransfer.kpn.com/api/v1/upload/info/"
	response, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		// We could also just panic
		return 4294967296
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	maxSizeResponse := MaxUploadSize{}
	err = json.Unmarshal(responseBody, &maxSizeResponse)
	if err != nil {
		panic(err)
	}
	return maxSizeResponse.MaxSize
}

func checkFiles(files []string, maxSize int) {
	totalSize := int64(0)
	for _, file := range files {
		stat, err := os.Stat(file)
		if err != nil {
			panic(err)
		}
		totalSize += stat.Size()
	}
	if totalSize > int64(maxSize) {
		fmt.Println("The toal size of files are too big.")
		fmt.Println("Maximum upload size is: ", maxSize)
		os.Exit(1)
	}
}

func readPasswordIfNeeded(options *Options) {
	if !options.Password {
		return
	}

	fmt.Println("Please enter the password:")
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		panic(err)
	}
	options.PasswordString = string(bytePassword)
}

type UploadParameters struct {
	DeleteAfter      string `json:"delete_after"`
	DeleteAfterCount string `json:"delete_after_count"`
}

type UploadResponse struct {
	CreatedTransfer Transfer `json:"created_transfer"`
}

type Transfer struct {
	Token            string `json:"management_token"`
	Uuid             string `json:"id"`
	DeleteAfter      string `json:"delete_after"`
	DeleteAfterCount string `json:"delete_after_count"`
}

func createUploadRequest() Transfer {
	const url = "https://filetransfer.kpn.com/api/v1/upload/request/"
	uploadParameters := UploadParameters{
		"7d",
		"2l",
	}

	uploadParamsJson, err := json.Marshal(uploadParameters)
	if err != nil {
		panic(err)
	}

	response, err := http.Post(url, "application/json", bytes.NewBuffer(uploadParamsJson))
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()

	statusOK := response.StatusCode >= 200 && response.StatusCode < 300
	if !statusOK {
		panic("Upload request failed")
	}

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	uploadResponse := UploadResponse{}
	err = json.Unmarshal(responseBody, &uploadResponse)
	if err != nil {
		panic(err)
	}
	return uploadResponse.CreatedTransfer
}

func contentTypeForFile(filePath string) string {
	head := make([]byte, 512)
	file, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	n, err := file.Read(head)
	if err != nil {
		panic(err)
	}
	return http.DetectContentType(head[:n])
}

func uploadFiles(files []string, transfer *Transfer) []UploadedFile {
	uploadedFiles := make([]UploadedFile, 0)
	for _, file := range files {
		fmt.Println("Uploading ", file)
		fileUuids := uploadFile(file, transfer)
		stat, err := os.Stat(file)
		if err != nil {
			panic(err)
		}
		fileData := UploadedFile{
			stat.Name(),
			int(stat.Size()),
			fileUuids,
			contentTypeForFile(file),
		}

		uploadedFiles = append(uploadedFiles, fileData)
		fmt.Println(" done")
	}
	fmt.Println("")
	return uploadedFiles
}

func uploadFile(filePath string, transfer *Transfer) []UploadedData {
	const chunkSize = 16 * 1024 * 1024 // 16 MB
	uploadedChunks := make([]UploadedData, 0)
	file, err := os.Open(filePath)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	buffer := make([]byte, chunkSize)
	for {
		n, err := file.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break // Normal end of file
			}
			panic(err)
		}
		uploadedData := uploadData(buffer[:n], transfer)
		uploadedChunks = append(uploadedChunks, uploadedData)
		fmt.Print(".")
	}
	return uploadedChunks
}

type EncryptionData struct {
	Key            []byte
	Iv             []byte
	AdditionalData []byte
	KeyMaterial    []byte
}

type TransferFileId struct {
	Uuid string `json:"id"`
}

type TransferRemainingUploadSize struct {
	Available int `json:"available_upload_size_in_bytes"`
}

type FileUploadResponse struct {
	Uuid      TransferFileId              `json:"created_transfer_file"`
	Available TransferRemainingUploadSize `json:"transfer"`
}

func uploadData(data []byte, transfer *Transfer) UploadedData {
	const url = "https://filetransfer.kpn.com/api/v1/upload/file/"
	cipherText, encryptionData := encryptData(data)
	hash := sha256.New()
	hash.Write(cipherText)
	cipherTextHash := hash.Sum(nil)
	encodedCipherText := hex.EncodeToString(cipherTextHash)
	hash.Reset()

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "blob")
	if err != nil {
		panic(err)
	}
	part.Write(cipherText)
	err = writer.WriteField("encrypted_contents_hash", encodedCipherText)
	if err != nil {
		panic(err)
	}

	err = writer.WriteField("transfer_management_token", transfer.Token)
	if err != nil {
		panic(err)
	}

	err = writer.Close()
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest(
		"POST",
		url,
		body,
	)

	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()

	statusOK := response.StatusCode >= 200 && response.StatusCode < 300
	if !statusOK {
		panic("Upload failed")
	}

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	fileUploadResponse := FileUploadResponse{}
	err = json.Unmarshal(responseBody, &fileUploadResponse)
	if err != nil {
		panic(err)
	}

	base64key := base64.URLEncoding.EncodeToString(encryptionData.KeyMaterial)
	base64key = strings.ReplaceAll(base64key, "=", ".")

	return UploadedData{
		fileUploadResponse.Uuid.Uuid,
		base64key,
	}
}

func encryptData(data []byte) ([]byte, EncryptionData) {
	encryptionData := calcEncryptionData(data)

	block, err := aes.NewCipher(encryptionData.Key)
	if err != nil {
		panic(err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}

	cipherText := aesgcm.Seal(nil, encryptionData.Iv, data, encryptionData.AdditionalData)
	return cipherText, encryptionData
}

func calcEncryptionData(data []byte) EncryptionData {
	hash := sha256.New()
	hash.Write(data)
	plainHash := hash.Sum(nil)
	hash.Reset()

	br := make([]byte, 16)
	_, err := rand.Read(br)
	if err != nil {
		panic(err)
	}

	hash.Write(br)
	randomHash := hash.Sum(nil)
	hash.Reset()

	hash.Write(plainHash)
	hash.Write(randomHash)
	keyMaterial := hash.Sum(nil)
	hash.Reset()

	encryptionKey, iv := keysFromKeyMaterial(keyMaterial)

	return EncryptionData{
		encryptionKey,
		iv,
		make([]byte, 1),
		keyMaterial,
	}
}
