package main

import (
	"archive/zip"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/sethgrid/pester"
	"github.com/urfave/negroni"

	_ "github.com/heroku/x/hmetrics/onload"
)

var (
	downloadConcurrency  int
	maxPayloadSizeInByte int64
)

func init() {
	downloadConcurrency, _ = strconv.Atoi(os.Getenv("DOWNLOAD_CONCURRENCY"))
	if downloadConcurrency <= 0 {
		downloadConcurrency = 10
		log.Println("DOWNLOAD_CONCURRENCY not specified. Setting it to", downloadConcurrency)
	}

	maxPayloadSizeInByte, _ = strconv.ParseInt(os.Getenv("MAX_PAYLOAD_SIZE_IN_BYTES"), 10, 64)
	if maxPayloadSizeInByte <= 0 {
		maxPayloadSizeInByte = 32 * 1024 * 1024
		log.Println("MAX_PAYLOAD_SIZE_IN_BYTES not specified. Setting it to", maxPayloadSizeInByte)
	}
}

type FileMetadata struct {
	path string
	url  string
}

type RemoteFile struct {
	path     string
	response *http.Response
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "7777"
	}
	err := http.ListenAndServe(":"+port, server())
	if err != nil {
		log.Fatal("Unable to start the server", err)
	}
}
func server() http.Handler {
	n := negroni.Classic()
	n.UseHandlerFunc(handleZip)
	return n
}

func handleZip(responseWriter http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(responseWriter, request.Body, maxPayloadSizeInByte)

	err := request.ParseForm()
	if err != nil {
		handleError(err, responseWriter)
		return
	}

	fileName := request.FormValue("filename")
	if fileName == "" {
		fileName = "download.zip"
	}
	responseWriter.Header().Add("Content-Disposition", "attachment; filename=\""+fileName+"\"")
	responseWriter.Header().Add("Access-Control-Allow-Origin", "*")

	downloaderQueue := make(chan FileMetadata)
	fileQueue := make(chan RemoteFile)
	zipperQueue := make(chan RemoteFile)
	downloadWaitGroup := sync.WaitGroup{}

	completionSignal := make(chan error)

	// Run zipper
	go zipper(responseWriter, zipperQueue, completionSignal)

	// Run downloader pool
	for i := 1; i <= downloadConcurrency; i++ {
		go downloader(downloaderQueue, fileQueue)
	}

	go func() {
		for df := range fileQueue {
			downloadWaitGroup.Done()
			zipperQueue <- df
		}
		close(zipperQueue)
	}()

	for path, urls := range request.PostForm {
		downloadWaitGroup.Add(1)
		downloaderQueue <- FileMetadata{path: path, url: urls[0]}
	}
	close(downloaderQueue)

	downloadWaitGroup.Wait()
	close(fileQueue)

	err = <-completionSignal

	if err != nil {
		handleError(err, responseWriter)
		return
	}

}

func handleError(err error, w http.ResponseWriter) {
	http.Error(w, "error", http.StatusBadRequest)
	log.Println(err)
	return
}

func zipper(writer io.Writer, incoming chan RemoteFile, complete chan error) {
	archive := zip.NewWriter(writer)
	for rf := range incoming {

		zipEntryHeader := &zip.FileHeader{
			Name:   rf.path,
			Method: zip.Deflate,
		}

		zipEntryHeader.Modified = time.Now()
		entryWriter, err := archive.CreateHeader(zipEntryHeader)
		if err != nil {
			complete <- err
			return
		}

		_, err = io.Copy(entryWriter, rf.response.Body)
		if err != nil {
			log.Println("Error in copy to response", err)
			complete <- err
			return
		}

		err = rf.response.Body.Close()
		if err != nil {
			log.Println("Error in closing response body", err)
			complete <- err
			return
		}
	}

	err := archive.Close()
	if err != nil {
		complete <- err
		return
	}
	complete <- nil
}

func downloader(inputQueue chan FileMetadata, outputQueue chan RemoteFile) {
	for df := range inputQueue {
		download, err := pester.Get(df.url)
		if err != nil {
			log.Println("ERROR: " + err.Error())
		} else {
			outputQueue <- RemoteFile{path: df.path, response: download}
		}
	}
}
