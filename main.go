package main

import (
	"archive/zip"
	"github.com/sethgrid/pester"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/urfave/negroni"

	_ "github.com/heroku/x/hmetrics/onload"
)

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
	log.Fatal(http.ListenAndServe(":"+port, server()))
}
func server() http.Handler {
	n := negroni.Classic()
	n.UseHandlerFunc(handleZip)
	return n
}

func handleZip(responseWriter http.ResponseWriter, request *http.Request) {
	_ = request.ParseForm()

	responseWriter.Header().Add("Content-Disposition", "attachment; filename=\""+fileName(request)+"\"")
	responseWriter.Header().Add("Access-Control-Allow-Origin", "*")

	inputQueue, completionSignal := zipInto(responseWriter)

	for path, urls := range request.PostForm {
		inputQueue <- FileMetadata{path: path, url: urls[0]}
	}
	close(inputQueue)

	err := <-completionSignal
	if err != nil {
		handleError(err, responseWriter)
		return
	}
}

func zipInto(writer http.ResponseWriter) (chan<- FileMetadata, <-chan error) {
	fileMetadataQueue := make(chan FileMetadata)
	downloadedFileQueue := make(chan RemoteFile)
	completionSignal := make(chan error)

	downloadsCompletionSignal := runDownloadWorkers(fileMetadataQueue, downloadedFileQueue)
	go func() {
		err := <-downloadsCompletionSignal
		if err != nil {
			completionSignal <- err
		}
		close(downloadedFileQueue)
	}()

	zipperCompletionSignal := runZipper(writer, downloadedFileQueue)
	go func() {
		err := <-zipperCompletionSignal
		if err != nil {
			completionSignal <- err
		}
		completionSignal <- nil
	}()

	return fileMetadataQueue, completionSignal

}

func runZipper(writer http.ResponseWriter, downloadedFileQueue chan RemoteFile) chan error {
	// Run zipper
	zipperCompletionSignal := make(chan error)
	go func() {
		archive := zip.NewWriter(writer)
		for rf := range downloadedFileQueue {
			zipEntryHeader := &zip.FileHeader{
				Name:   rf.path,
				Method: zip.Deflate,
			}
			zipEntryHeader.Modified = time.Now()
			entryWriter, err := archive.CreateHeader(zipEntryHeader)
			if err != nil {
				zipperCompletionSignal <- err
				return
			}
			_, err = io.Copy(entryWriter, rf.response.Body)
			if err != nil {
				zipperCompletionSignal <- err
				return
			}
			_ = rf.response.Body.Close()
		}
		err := archive.Close()
		if err != nil {
			zipperCompletionSignal <- err
			return
		}
		zipperCompletionSignal <- nil
	}()
	return zipperCompletionSignal
}

func runDownloadWorkers(fileMetadataQueue chan FileMetadata, downloadedFileQueue chan RemoteFile) <-chan error {
	// Run downloadWorker pool
	workersRunning := sync.WaitGroup{}
	workersCompletionSignal := make(chan error)
	for i := 1; i <= downloadConcurrency(); i++ {
		workersRunning.Add(1)
		go func() {
			for df := range fileMetadataQueue {
				download, err := pester.Get(df.url)
				if err != nil {
					workersCompletionSignal <- err
				} else {
					downloadedFileQueue <- RemoteFile{df.path, download}
				}
			}
			workersRunning.Done()
		}()
	}
	go func() {
		workersRunning.Wait()
		workersCompletionSignal <- nil
	}()
	return workersCompletionSignal
}

func fileName(request *http.Request) (name string) {
	name = request.FormValue("filename")
	if name == "" {
		name = "download.zip"
	}
	return
}

func downloadConcurrency() (dc int) {
	dc, err := strconv.Atoi(os.Getenv("DOWNLOAD_CONCURRENCY"))
	if err != nil || dc <= 0 {
		dc = 10
	}
	return
}

func handleError(err error, w http.ResponseWriter) {
	http.Error(w, "error", http.StatusBadRequest)
	log.Println(err)
	return
}
