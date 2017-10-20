package main

import (
	"archive/zip"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"sync"

	"strconv"

	_ "github.com/heroku/x/hmetrics"
)

type fileDef struct {
	path string
	url  string
}
type remoteFile struct {
	path     string
	response *http.Response
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "7777"
	}
	concurrency, err := strconv.Atoi(os.Getenv("DOWNLOAD_CONCURRENCY"))
	if err != nil {
		concurrency = 10
	}

	log.Println("Listening on port " + port)
	http.ListenAndServe(":"+port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		fileName := r.FormValue("filename")
		if fileName == "" {
			fileName = "download.zip"
		}
		w.Header().Add("Content-Disposition", "attachment; filename=\""+fileName+"\"")
		log.Println("Generating zip for " + fileName)

		archive := zip.NewWriter(w)

		downloadQueue := make(chan fileDef)
		archiveQueue := make(chan remoteFile)
		downloaderWaitGroup := sync.WaitGroup{}
		finished := make(chan error)

		// Run archiver
		go func() {
			for rf := range archiveQueue {
				log.Println("Received data for " + rf.path)
				entryHeader := &zip.FileHeader{
					Name:   rf.path,
					Method: zip.Deflate,
				}
				entryHeader.SetModTime(time.Now())
				entryHeader.SetMode(os.ModePerm)
				entry, err := archive.CreateHeader(entryHeader)
				if err != nil {
					finished <- err
					return
				}
				log.Println(rf.response.Header.Get("Last-Modified"))
				modifiedTime, err := time.Parse("Mon, 2 Jan 2006 15:04:05 MST", rf.response.Header.Get("Last-Modified"))
				if err != nil {
					entryHeader.SetModTime(modifiedTime)
				}

				io.Copy(entry, rf.response.Body)
				rf.response.Body.Close()
				log.Println("Zipped and closed " + rf.path)
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
		for w := 1; w <= concurrency; w++ {
			go func(w int) {
				log.Println("Downloader Ready: " + strconv.Itoa(w))
				for df := range downloadQueue {
					log.Println("Downloading[" + strconv.Itoa(w) + "] path: " + df.path + " URL: " + df.url)
					download, err := http.Get(df.url)
					if err != nil {
						downloaderWaitGroup.Done()
						finished <- err
						return
					}
					archiveQueue <- remoteFile{path: df.path, response: download}
					downloaderWaitGroup.Done()
				}
				log.Println("Downloader Shutting Down: " + strconv.Itoa(w))
			}(w)
		}

		for path, urls := range r.PostForm {
			downloaderWaitGroup.Add(1)
			downloadQueue <- fileDef{path: path, url: urls[0]}
		}
		close(downloadQueue)
		downloaderWaitGroup.Wait()
		close(archiveQueue)
		err := <-finished
		if err != nil {
			handleError(err, w)
			return
		}
	}))
}

func handleError(err error, w http.ResponseWriter) {
	http.Error(w, "error", http.StatusBadRequest)
	log.Println(err)
	return
}
