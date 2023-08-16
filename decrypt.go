package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"
)

type TransferResponse struct {
	DeleteAfter string `json:"delete_after"`
	CreatedAt   string `json:"created_at"`
	ExpiresIn   string `json:"expires_in"`
	HasPassword bool   `json:"has_password"`
}

type DownloadRequest struct {
	TransferId string `json:"transfer_id"`
}

type DownloadRequestResponse struct {
	DownloadToken string           `json:"download_token"`
	Transfer      TransferResponse `json:"transfer"`
}

type FileInfo struct {
	DownloadCount  int
	RemainingCount int
	Name           string
	Size           int
	FileType       string
	Chunks         []UploadedData
}

func decryptFromUrl(url string, options *Options) {
	token, key := parseUrl(url)
	transferInfo := initiateDownloadRequest(token)
	printBasicInfo(&transferInfo)
	metadata := downloadMetadata(&transferInfo, key)
	fileInfo := validateFiles(metadata.Files, transferInfo.DownloadToken)
	if options.Show {
		printFiles(metadata.Description, fileInfo)
		return
	}
	downloadFiles(fileInfo, transferInfo.DownloadToken)
	fmt.Println("Successfully downloaded all file(s).")
}

func parseUrl(url string) (string, []byte) {
	const prefix = "https://filetransfer.kpn.com/download/"
	if !strings.HasPrefix(url, prefix) {
		panic("Url expected to be in the format of https://filetransfer.kpn.com/download/<uuid>#<base64key>")
	}
	url = url[len(prefix):]
	data := strings.SplitN(url, "#", 2)
	uuid, base64key := data[0], data[1]
	base64key = strings.ReplaceAll(base64key, ".", "=")
	key := make([]byte, base64.URLEncoding.DecodedLen(len(base64key)))
	n, err := base64.URLEncoding.Decode(key, []byte(base64key))
	if err != nil {
		fmt.Println("Please double check the url. Missing dot at the end?")
		panic(err)
	}
	key = key[:n]
	return uuid, key
}

func printFiles(description string, fileInfo []FileInfo) {
	fmt.Println("\nDescription: ", description)
	fmt.Println("")
	if len(fileInfo) == 0 {
		fmt.Println("No files detected.")
		return
	}
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetTitle("Files")
	t.AppendHeader(table.Row{"#", "Name", "Downloads", "Size (bytes)", "FileType"})
	for i, info := range fileInfo {
		t.AppendRow(table.Row{i, info.Name, info.RemainingCount, info.Size, info.FileType})
	}
	t.Render()
}

func initiateDownloadRequest(token string) DownloadRequestResponse {
	const url = "https://filetransfer.kpn.com/api/v1/download/request/"
	downloadRequest := DownloadRequest{
		token,
	}
	downloadRequestJson, err := json.Marshal(downloadRequest)
	if err != nil {
		panic(err)
	}

	response, err := http.Post(url, "application/json", bytes.NewBuffer(downloadRequestJson))
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()

	statusOK := response.StatusCode >= 200 && response.StatusCode < 300
	if !statusOK {
		panic("Download request failed")
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	downloadResponse := DownloadRequestResponse{}
	err = json.Unmarshal(responseBody, &downloadResponse)
	if err != nil {
		panic(err)
	}
	return downloadResponse
}

func printBasicInfo(transferInfo *DownloadRequestResponse) {
	fmt.Println("Created at: ", transferInfo.Transfer.CreatedAt)
	fmt.Println("Delete after: ", transferInfo.Transfer.DeleteAfter)
	fmt.Println("Expires in: ", transferInfo.Transfer.ExpiresIn)
	fmt.Println("Has password: ", transferInfo.Transfer.HasPassword)
	fmt.Println("")
}

func downloadMetadata(transferInfo *DownloadRequestResponse, key []byte) Metadata {
	const url = "https://filetransfer.kpn.com/api/v1/download/metadata/"
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	request.Header.Set("Download-Token", transferInfo.DownloadToken)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	statusOK := response.StatusCode >= 200 && response.StatusCode < 300
	if !statusOK {
		panic("Metadata download failed")
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	return decodeMetadata(responseBody, key)
}

func decodeMetadata(cipherText []byte, keyMaterial []byte) Metadata {
	key, iv := keysFromKeyMaterial(keyMaterial)
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	add_data := make([]byte, 1)

	plaintext, err := aesgcm.Open(nil, iv, cipherText, add_data)
	if err != nil {
		panic(err)
	}

	metadata := Metadata{}
	err = json.Unmarshal(plaintext, &metadata)
	if err != nil {
		panic(err)
	}
	return metadata
}

type ValidateRequest struct {
	Token     string   `json:"download_token"`
	FileUuids []string `json:"files"`
}

type ValidateResponse struct {
	Uuid           string `json:"id"`
	Valid          bool   `json:"valid"`
	DownloadCount  int    `json:"download_count"`
	RemainingCount int    `json:"remaining_downloads"`
}

func validateFiles(files []UploadedFile, token string) []FileInfo {
	validateResponse := doValidateRequest(files, token)
	// chunk_uuid -> position in files list
	helper := map[string]int{}
	for i, file := range files {
		helper[file.Chunks[0].Uuid] = i
	}
	fileInfoMap := map[int]FileInfo{}
	for _, validated := range validateResponse {
		position := helper[validated.Uuid]
		file := files[position]
		fileInfoMap[position] = FileInfo{
			validated.DownloadCount,
			validated.RemainingCount,
			file.Name,
			file.Size,
			file.FileType,
			file.Chunks,
		}
	}
	fileInfo := make([]FileInfo, 0)
	for i := 0; i < len(fileInfoMap); i++ {
		fileInfo = append(fileInfo, fileInfoMap[i])
	}
	return fileInfo
}

func doValidateRequest(files []UploadedFile, token string) []ValidateResponse {
	const url = "https://filetransfer.kpn.com/api/v1/download/files/validate/"
	uuidsToSend := make([]string, 0)
	for _, file := range files {
		uuidsToSend = append(uuidsToSend, file.Chunks[0].Uuid)
	}
	validateRequest := ValidateRequest{
		token,
		uuidsToSend,
	}
	validateRequestJson, err := json.Marshal(validateRequest)
	if err != nil {
		panic(err)
	}

	response, err := http.Post(url, "application/json", bytes.NewBuffer(validateRequestJson))
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()

	statusOK := response.StatusCode >= 200 && response.StatusCode < 300
	if !statusOK {
		panic("Download request failed")
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	validateResponse := make([]ValidateResponse, 0)
	err = json.Unmarshal(responseBody, &validateResponse)
	if err != nil {
		panic(err)
	}
	return validateResponse
}

func downloadFiles(fileInfo []FileInfo, token string) {
	for _, file := range fileInfo {
		downloadFile(&file, token)
	}
	finalizeDownload(fileInfo, token) // ignore return value
}

func downloadFile(fileInfo *FileInfo, token string) {
	fmt.Println("Downloading ", fileInfo.Name)
	target := findNameForFile(fileInfo.Name)
	fmt.Println("Saving to ", target)
	f, err := os.Create(target)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	for _, chunk := range fileInfo.Chunks {
		data := downloadChunk(&chunk, token)
		f.Write(data)
		fmt.Print(".")
	}
	fmt.Print("\n\n")
}

func findNameForFile(name string) string {
	targetName := strings.Clone(name)
	// Avoid overwriting existing files
	_, err := os.Stat(name)
	if err == nil {
		ext := filepath.Ext(name)
		base := name[:len(name)-len(ext)]
		var newFileName string
		for i := 1; ; i++ {
			newFileName = fmt.Sprintf("%s_%d%s", base, i, ext)
			if _, err := os.Stat(newFileName); os.IsNotExist(err) {
				break
			}
		}
		targetName = newFileName
	}
	return targetName
}

func downloadChunk(chunk *UploadedData, token string) []byte {
	const url = "https://filetransfer.kpn.com/api/v1/download/file/"
	request, err := http.NewRequest("GET", url+chunk.Uuid+"/", nil)
	if err != nil {
		panic(err)
	}
	request.Header.Set("Download-Token", token)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()
	statusOK := response.StatusCode >= 200 && response.StatusCode < 300
	if !statusOK {
		panic("File download failed")
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}

	base64key := strings.ReplaceAll(chunk.Secret, ".", "=")
	key := make([]byte, base64.URLEncoding.DecodedLen(len(base64key)))
	n, err := base64.URLEncoding.Decode(key, []byte(base64key))
	if err != nil {
		fmt.Println("Wrong key in metadata")
		panic(err)
	}
	key = key[:n]

	return decodeChunk(responseBody, key)
}

func decodeChunk(cipherText []byte, keyMaterial []byte) []byte {
	key, iv := keysFromKeyMaterial(keyMaterial)
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	add_data := make([]byte, 1)

	plaintext, err := aesgcm.Open(nil, iv, cipherText, add_data)
	if err != nil {
		panic(err)
	}

	return plaintext
}

type FinalizeRequest struct {
	Chunks []string `json:"files"`
}

type FinalizeResponse struct {
	Valid bool               `json:"transfer_is_valid"`
	Uuids []ValidateResponse `json:"files"`
}

func finalizeDownload(fileInfo []FileInfo, token string) FinalizeResponse {
	const url = "https://filetransfer.kpn.com/api/v1/download/files/success/"
	uuidsToSend := make([]string, 0)
	for _, file := range fileInfo {
		for _, chunk := range file.Chunks {
			uuidsToSend = append(uuidsToSend, chunk.Uuid)
		}
	}
	finalizeRequest := FinalizeRequest{
		uuidsToSend,
	}
	finalizeRequestJson, err := json.Marshal(finalizeRequest)
	if err != nil {
		panic(err)
	}

	request, err := http.NewRequest("POST", url, bytes.NewBuffer(finalizeRequestJson))
	if err != nil {
		panic(err)
	}
	request.Header.Set("Download-Token", token)
	request.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	defer response.Body.Close()

	statusOK := response.StatusCode >= 200 && response.StatusCode < 300
	if !statusOK {
		fmt.Println(response)
		responseBody, _ := io.ReadAll(response.Body)
		fmt.Println(string(responseBody))
		panic("Finalize request failed")
	}
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	finalizeResponse := FinalizeResponse{}
	err = json.Unmarshal(responseBody, &finalizeResponse)
	if err != nil {
		panic(err)
	}
	return finalizeResponse
}
