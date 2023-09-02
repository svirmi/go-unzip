package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

func main() {

}

func setupPipeLine() error {
	done := make(chan struct{})
	defer close(done)
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
			if contentType != "" { // TODO
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
