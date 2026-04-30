package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: rdn <url> [output_file]")
		fmt.Println("\nA blazingly fast parallel downloader for the modern terminal.")
		fmt.Println("Uses optimized goroutines to get your files in a flash.")
		os.Exit(1)
	}

	url := os.Args[1]
	outputFile := ""
	if len(os.Args) >= 3 {
		outputFile = os.Args[2]
	}

	downloader := NewDownloader(url, outputFile)
	if err := downloader.Download(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
