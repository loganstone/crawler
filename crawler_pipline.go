package main

import (
	"fmt"
	"golang.org/x/net/html"
	"log"
	"net/http"
	"runtime"
	"sync"
)

var fetched = struct {
	m map[string]error
	sync.Mutex
}{m: make(map[string]error)}

type result struct {
	url  string
	name string
}

func fetch(url string) (*html.Node, error) {
	res, err := http.Get(url)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	doc, err := html.Parse(res.Body)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return doc, nil
}

func parseFollowing(doc *html.Node, urls chan string) <-chan string {
	name := make(chan string)

	go func() {
		var f func(*html.Node)
		f = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "img" { // img tag
				for _, a := range n.Attr {

					if a.Key == "class" && a.Val == "avatar left" {
						for _, a := range n.Attr {
							if a.Key == "alt" {
								name <- a.Val
								break
							}
						}
					}

					if a.Key == "class" && a.Val == "gravatar" {
						user := n.Parent.Attr[0].Val
						urls <- "https://github.com" + user + "/following"
						break
					}

				}
			}

			for c := n.FirstChild; c != nil; c = c.NextSibling {
				f(c)
			}
		}
		f(doc)
	}()

	return name
}

func crawl(url string, urls chan string, c chan <- result) {
	fetched.Lock()
	if _, ok := fetched.m[url]; ok {
		fetched.Unlock()
		return
	}

	fetched.Unlock()

	doc, err := fetch(url)

	if err != nil {
		go func(u string) {
			urls <- u
		}(url)
		return
	}

	fetched.Lock()
	fetched.m[url] = err
	fetched.Unlock()

	name := <-parseFollowing(doc, urls)
	c <- result{url, name}
}

func worker(done <-chan struct{}, urls chan string, c chan <- result) {
	for url := range urls {
		select {
		case <-done:
			return
		default:
			crawl(url, urls, c)
		}
	}
}

func main() {
	fmt.Println(runtime.GOMAXPROCS(0))

	urls := make(chan string)
	done := make(chan struct{})
	c := make(chan result)

	var wg sync.WaitGroup
	const numWorkers = 50
	wg.Add(numWorkers)

	for i := 0; i < numWorkers; i++ {
		go func() {
			worker(done, urls, c)
			wg.Done()
		}()
	}

	go func() {
		wg.Wait()
		close(c)
	}()

	urls <- "https://github.com/hyunsuk/following"

	count := 0

	for r := range c {
		fmt.Println(r.name)

		count++

		if count > 10000 {
			close(done)
			break
		}
	}
}
