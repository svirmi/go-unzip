package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type result struct {
	srcFilePath string
	err         error
}

func main() {

	folder := "./files"

	start := time.Now()
	err := setupPipeLine(folder)

	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Time taken: %s\n", time.Since(start))
}

func setupPipeLine(root string) error {
	done := make(chan struct{})
	defer close(done)

	// firts stage pipeline, do the file walk
	paths, errc := walkDir(done, root)

	// second stage
	results := processFiles(done, paths)

	for r := range results {
		if r.err != nil {
			return r.err
		}
	}

	if err := <-errc; err != nil {
		return err
	}

	return nil
}

func walkDir(done <-chan struct{}, root string) (<-chan string, <-chan error) {
	paths := make(chan string)
	errc := make(chan error, 1)

	go func() {
		defer close(paths)

		errc <- filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.Mode().IsRegular() {
				return nil
			}

			contentType, _ := getFileContentType(path)

			if contentType != "application/zip" {
				return nil
			}

			select {
			case paths <- path:
			case <-done:
				return fmt.Errorf("walk canceled")
			}
			return nil
		})
	}()

	return paths, errc
}

func getFileContentType(file string) (string, error) {
	out, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer out.Close()

	// Only the first 512 bytes are used to sniff the content type.
	buffer := make([]byte, 512)

	_, err = out.Read(buffer)
	if err != nil {
		return "", err
	}

	// Use the net/http package's handy DectectContentType function. Always returns a valid
	// content-type by returning "application/octet-stream" if no others seemed to match.
	contentType := http.DetectContentType(buffer)

	return contentType, nil
}

func processFiles(done <-chan struct{}, paths <-chan string) <-chan result {
	var wg sync.WaitGroup
	results := make(chan result)

	unzipper := func() {
		for srcFilePath := range paths {
			f, err := os.Open(srcFilePath)

			fmt.Println(f.Name())

			if err != nil {
				select {
				case results <- result{srcFilePath, err}:
				case <-done:
					return
				}
			}

			select {
			case results <- result{srcFilePath, nil}:
			case <-done:
				return
			}
		}
	}

	const numThumbnailer = 5

	for i := 0; i < numThumbnailer; i++ {
		wg.Add(1)
		go func() {
			unzipper()
			wg.Done()
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	return results
}
