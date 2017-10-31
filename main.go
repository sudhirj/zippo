package main

import (
	"archive/zip"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"sync"

	"github.com/sethgrid/pester"

	"strconv"

	_ "github.com/heroku/x/hmetrics/onload"
)

type FileDef struct {
	path string
	url  string
}

type RemoteFile struct {
	path     string
	response *http.Response
}

func main() {
	Port := os.Getenv("PORT")
	if Port == "" {
		Port = "7777"
	}
	log.Println("Listening on PORT " + Port)
	http.ListenAndServe(":"+Port, zipperHandler())
}

func zipperHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		DownloadConcurrency, err := strconv.Atoi(os.Getenv("DOWNLOAD_CONCURRENCY"))
		if err != nil {
			DownloadConcurrency = 10
		}
		r.ParseForm()
		fileName := r.FormValue("filename")
		if fileName == "" {
			fileName = "download.zip"
		}
		w.Header().Add("Content-Disposition", "attachment; filename=\""+fileName+"\"")
		log.Println("Generating zip for " + fileName)

		archive := zip.NewWriter(w)

		archiverQueue := make(chan RemoteFile)
		downloaderQueue := make(chan FileDef)
		downloaderWaitGroup := sync.WaitGroup{}

		finished := make(chan error)

		// Run archiver
		go func() {
			for rf := range archiverQueue {
				log.Println("Received data for " + rf.path)
				zipEntryHeader := &zip.FileHeader{
					Name:   rf.path,
					Method: zip.Deflate,
				}
				zipEntryHeader.SetModTime(time.Now())
				entryWriter, err := archive.CreateHeader(zipEntryHeader)
				if err != nil {
					finished <- err
					return
				}
				io.Copy(entryWriter, rf.response.Body)
				rf.response.Body.Close()
				log.Println("Zipped " + rf.path)
			}
			log.Println("All zipping complete, closing up.")
			err := archive.Close()
			if err != nil {
				finished <- err
				return
			}
			log.Println("Acrhive closed.")
			finished <- nil
		}()

		// Run downloaders
		for w := 1; w <= DownloadConcurrency; w++ {
			go func(w int) {
				index := strconv.Itoa(w)
				log.Println("Downloader Ready: " + index)
				for df := range downloaderQueue {
					log.Println("Downloading [" + index + "] path: " + df.path + " URL: " + df.url)
					download, err := pester.Get(df.url)
					if err != nil {
						log.Println("ERROR: " + err.Error())
					} else {
						archiverQueue <- RemoteFile{path: df.path, response: download}
					}
					downloaderWaitGroup.Done()
				}
				log.Println("Downloader Shutting Down: " + index)
			}(w)
		}

		for path, urls := range r.PostForm {
			downloaderWaitGroup.Add(1)
			downloaderQueue <- FileDef{path: path, url: urls[0]}
		}
		close(downloaderQueue)

		downloaderWaitGroup.Wait()
		close(archiverQueue)

		err = <-finished
		if err != nil {
			handleError(err, w)
			return
		}
	})
}

func handleError(err error, w http.ResponseWriter) {
	http.Error(w, "error", http.StatusBadRequest)
	log.Println(err)
	return
}
