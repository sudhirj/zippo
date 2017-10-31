package main

import (
	"testing"
	"net/http/httptest"
	"net/http"
	"fmt"
	"log"
	"net/url"
	"archive/zip"
	"os"
	"io"
	"io/ioutil"
	"time"
	"math/rand"
	"strconv"
)

func TestZipper(t *testing.T) {
	t.Parallel()
	fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println(r.URL.Path)
		fmt.Fprint(w, r.URL.Path)
	}))
	zipperServer := httptest.NewServer(zipperHandler())
	for n := 0; n < rand.Intn(100); n++ {
		data := &url.Values{}
		for i := 0; i < rand.Intn(1000); i++ {
			data.Set(randomString(10), fileServer.URL+"/"+randomString(20))
		}

		resp, err := http.PostForm(zipperServer.URL, *data)
		failOnError(err, t)

		tempFile, err := ioutil.TempFile("", "zippotest")

		_, err = io.Copy(tempFile, resp.Body)
		failOnError(err, t)
		resp.Body.Close()
		tempFile.Close()
		log.Println(tempFile.Name())
		downloadedZip, err := zip.OpenReader(tempFile.Name())
		failOnError(err, t)

		if len(*data) != len(downloadedZip.File) {
			t.Fatal("Size mismatch : " + strconv.Itoa(len(*data)) + " | " + strconv.Itoa(len(downloadedZip.File)))
		}
		for _, f := range downloadedZip.File {
			fileUrl := data.Get(f.Name)
			if fileUrl == "" {
				t.Fatal("Extra file found: " + f.Name)
			}
			zippedFile, err := f.Open()
			if err != nil {
				log.Fatal(err)
			}
			bytes, err := ioutil.ReadAll(zippedFile)
			link, err := url.Parse(fileUrl)
			failOnError(err, t)
			if string(bytes) != link.Path {
				log.Fatal("unexpected values : " + string(bytes) + " | " + link.Path)
			}
			if err != nil {
				log.Fatal(err)
			}
			zippedFile.Close()
		}
		os.Remove(tempFile.Name())
	}

}
func failOnError(err error, t *testing.T) {
	if err != nil {
		t.Fatal(err)
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randomString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}
