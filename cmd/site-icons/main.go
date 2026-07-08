package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"

	"FindSiteIcons"
)

func main() {
	flag.Usage = printHelp
	fast := flag.Bool("fast", false, "Stop early when best matches are found")
	jsonOut := flag.Bool("json", false, "Output in JSON format")
	debug := flag.Bool("debug", false, "Print debug information to stderr")
	listFile := flag.String("l", "", "Read URLs from file (one per line) and process concurrently")
	jobs := flag.Int("j", 10, "Max concurrent workers when using -l")
	quiet := flag.Bool("q", false, "Quiet mode, suppress banner output")
	flag.Parse()

	if !*quiet {
		fmt.Fprintln(os.Stderr, "FindSiteIcons v0.1.0 - Sniff out every icon a site hides.")
	}

	if *listFile != "" {
		processBatch(*listFile, *fast, *jsonOut, *debug, *jobs)
		return
	}

	if flag.NArg() < 1 {
		printHelp()
		os.Exit(1)
	}

	entries := processURL(flag.Arg(0), *fast, *jsonOut, *debug)
	if entries == nil {
		os.Exit(1)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.Encode(entries)
	} else {
		for _, icon := range entries {
			sizeStr := ""
			if icon.Info.Size != nil {
				sizeStr = fmt.Sprintf(" %dx%d", icon.Info.Size.Width, icon.Info.Size.Height)
			} else if icon.Info.Sizes != nil {
				sizeStr = " " + icon.Info.Sizes.String()
			}
			fmt.Printf("%s %s %s%s\n", icon.URL, icon.Kind, icon.Info.Type, sizeStr)
		}
	}
}

func processURL(urlStr string, fast, jsonOut, debug bool) []*site_icons.Icon {
	if debug {
		fmt.Fprintf(os.Stderr, "Fetching icons for: %s\n", urlStr)
	}

	icons := site_icons.NewSiteIcons()
	entries, err := icons.LoadWebsite(urlStr, fast)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching %s: %v\n", urlStr, err)
		return nil
	}
	return entries
}

func outputIcon(w *json.Encoder, urlStr string, entries []*site_icons.Icon, jsonOut bool) {
	if entries == nil {
		return
	}

	if jsonOut {
		w.Encode(map[string]interface{}{
			"url":   urlStr,
			"icons": entries,
		})
	} else {
		fmt.Printf("# %s\n", urlStr)
		for _, icon := range entries {
			sizeStr := ""
			if icon.Info.Size != nil {
				sizeStr = fmt.Sprintf(" %dx%d", icon.Info.Size.Width, icon.Info.Size.Height)
			} else if icon.Info.Sizes != nil {
				sizeStr = " " + icon.Info.Sizes.String()
			}
			fmt.Printf("%s %s %s%s\n", icon.URL, icon.Kind, icon.Info.Type, sizeStr)
		}
		fmt.Println()
	}
}

func readURLsFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var urls []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}
	return urls, scanner.Err()
}

func processBatch(path string, fast, jsonOut, debug bool, maxWorkers int) {
	urls, err := readURLsFromFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", path, err)
		os.Exit(1)
	}

	if len(urls) == 0 {
		fmt.Fprintf(os.Stderr, "No URLs found in %s\n", path)
		os.Exit(1)
	}

	if debug {
		fmt.Fprintf(os.Stderr, "Processing %d URLs with %d workers\n", len(urls), maxWorkers)
	}

	urlCh := make(chan string, len(urls))
	type result struct {
		url   string
		icons []*site_icons.Icon
	}
	resultCh := make(chan result, len(urls))

	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range urlCh {
				entries := processURL(u, fast, false, debug)
				resultCh <- result{url: u, icons: entries}
			}
		}()
	}

	for _, u := range urls {
		urlCh <- u
	}
	close(urlCh)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		for r := range resultCh {
			outputIcon(enc, r.url, r.icons, true)
		}
	} else {
		for r := range resultCh {
			outputIcon(nil, r.url, r.icons, false)
		}
	}
}

func printHelp() {
	prog := os.Args[0]
	fmt.Fprintf(os.Stderr, `FindSiteIcons - Website icon scraper (Go)

USAGE:
  %s [options] <url>
  %s -l <file> [options]

OPTIONS:
  --fast       Stop early when best matches are found (faster, fewer results)
  --json       Output in JSON format
  --debug      Print debug information to stderr
  -l <file>    Read URLs from file (one per line, # for comments)
  -j <N>       Max concurrent workers when using -l (default: 10)
  -q           Quiet mode, suppress banner output
  --help       Show this help message

EXAMPLES:
  %s https://github.com
  %s --json https://github.com
  %s --fast --json https://www.example.com
  %s -l urls.txt --json
  %s -l urls.txt --json -j 5

ICON SOURCES:
  HTML <link> tags (icon, apple-touch-icon, manifest)
  Web App Manifest (manifest.json)
  Default locations (/favicon.svg, /favicon.ico)
  Site logo detection (<img> tags with weighted scoring)

OUTPUT:
  Each icon contains: url, kind, type, size/sizes, data (base64 image)
  Icons sorted: SVG > PNG > GIF > JPEG > ICO, by resolution descending
`, prog, prog, prog, prog, prog, prog, prog)
}
