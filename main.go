package main

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

	// first stage pipeline, do the files walk in folder
	paths, errc := walkDir(done, root)

	// second stage
	results := processFiles(done, paths)

	for r := range results {
		if r.err != nil {
			return r.err
		}
	}

	// check for error on the channel, from walkDir stage.
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

			err := unzip(srcFilePath, "out")

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

	numThreads := runtime.GOMAXPROCS(-1) * 2 // TODO : set this number more clever

	for i := 0; i < numThreads; i++ {
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

// taken from https://stackoverflow.com/questions/20357223/easy-way-to-unzip-file
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := r.Close(); err != nil {
			panic(err)
		}
	}()

	os.MkdirAll(dest, 0755)

	// Closure to address file descriptors issue with all the deferred .Close() methods
	extractAndWriteFile := func(f *zip.File) error {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() {
			if err := rc.Close(); err != nil {
				panic(err)
			}
		}()

		path := filepath.Join(dest, f.Name)

		// Check for ZipSlip (Directory traversal)
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", path)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(path, f.Mode())
		} else {
			os.MkdirAll(filepath.Dir(path), f.Mode())
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				return err
			}
			defer func() {
				if err := f.Close(); err != nil {
					panic(err)
				}
			}()

			_, err = io.Copy(f, rc)
			if err != nil {
				return err
			}
		}
		return nil
	}

	for _, f := range r.File {
		err := extractAndWriteFile(f)
		if err != nil {
			return err
		}
	}

	return nil
}
