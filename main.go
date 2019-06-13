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

func handleZip(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()

	w.Header().Add("Content-Disposition", "attachment; filename=\""+fileName(r)+"\"")
	w.Header().Add("Access-Control-Allow-Origin", "*")

	inputQueue, completionSignal := zipInto(w)

	for path, urls := range r.PostForm {
		inputQueue <- FileMetadata{path: path, url: urls[0]}
	}
	close(inputQueue)

	err := <-completionSignal
	if err != nil {
		log.Println(err)
		http.Error(w, "error", http.StatusInternalServerError)
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

func runZipper(w http.ResponseWriter, input chan RemoteFile) chan error {
	zipperSignal := make(chan error)
	go func() {
		archive := zip.NewWriter(w)
		for rf := range input {
			entryWriter, err := archive.CreateHeader(rf.zipHeader())
			if err == nil {
				_, err = io.Copy(entryWriter, rf.response.Body)
				_ = rf.response.Body.Close()
			}

			if err != nil {
				zipperSignal <- err
				continue
			}
		}
		err := archive.Close()
		if err != nil {
			zipperSignal <- err
			return
		}
		zipperSignal <- nil
	}()
	return zipperSignal
}

func runDownloadWorkers(input chan FileMetadata, output chan RemoteFile) <-chan error {
	workersRunning := sync.WaitGroup{}
	workerSignal := make(chan error)
	for i := 1; i <= downloadConcurrency(); i++ {
		workersRunning.Add(1)
		go func() {
			for df := range input {
				download, err := pester.Get(df.url)
				if err != nil {
					workerSignal <- err
				} else {
					output <- RemoteFile{df.path, download}
				}
			}
			workersRunning.Done()
		}()
	}
	go func() {
		workersRunning.Wait()
		workerSignal <- nil
	}()
	return workerSignal
}

func (rf RemoteFile) zipHeader() (zh *zip.FileHeader) {
	zh = &zip.FileHeader{
		Name:   rf.path,
		Method: zip.Deflate,
	}
	zh.Modified = time.Now()
	return
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
