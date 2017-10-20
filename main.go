package main

import (
	"archive/zip"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/heroku/x/hmetrics"
)

type remote_file struct {
	path     string
	response *http.Response
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "7777"
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

		receiver := make(chan remote_file, 10)
		done := make(chan error)

		go func() {
			for rf := range receiver {
				log.Println("Received response for " + rf.path)
				entryHeader := &zip.FileHeader{
					Name:   rf.path,
					Method: zip.Deflate,
				}
				entryHeader.SetModTime(time.Now())
				entryHeader.SetMode(os.ModePerm)
				entry, err := archive.CreateHeader(entryHeader)
				if err != nil {
					done <- err
					return
				}

				modifiedTime, err := time.Parse("Mon, 2 Jan 2006 15:04:05 MST", rf.response.Header.Get("Last-Modified"))
				if err != nil {
					entryHeader.SetModTime(modifiedTime)
				}
				io.Copy(entry, rf.response.Body)
				rf.response.Body.Close()
				log.Println("Zipped and closed " + rf.path)
			}
			log.Println("All zipping complete, finishing up.")
			done <- nil
		}()

		for path, urls := range r.PostForm {
			log.Println("Downloading key: " + path + " URL: " + urls[0])
			download, err := http.Get(urls[0])
			if err != nil {
				handleError(err, w)
				return
			}
			receiver <- remote_file{path: path, response: download}
		}
		log.Println("All downloads finished, closing receiver.")
		close(receiver)

		err := <-done
		if err != nil {
			handleError(err, w)
			return
		}
		err = archive.Close()
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
