package main

import (
	"fmt"
	"golang.org/x/net/html"
	"log"
	"net/http"
	"runtime"
	"sync"
)

type FollowingResult struct {
	url  string
	name string
}

type StarsResult struct {
	repo string
}


type FetchedUrl struct {
	m map[string]error
	sync.Mutex
}

type FetchedRepo struct {
	m map[string]struct{}
	sync.Mutex
}

type Crawler interface {
	Crawl()
}

type GitHubFollowing struct {
	fetchedUrl *FetchedUrl
	p          *Pipeline
	stars      *GitHubStars
	result     chan FollowingResult
	url        string
}


type GitHubStars struct {
	fetchedUrl  *FetchedUrl
	fetchedRepo *FetchedRepo
	p           *Pipeline
	result      chan StarsResult
	url         string
}

func fetch_by_interface(url string) (*html.Node, error) {
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

func (g *GitHubFollowing) Request(url string) {
	g.p.request <- &GitHubFollowing{
		fetchedUrl: g.fetchedUrl,
		p: g.p,
		result: g.result,
		stars: g.stars,
		url: url,
	}
}

func (g *GitHubFollowing) Parse(doc *html.Node) <-chan string {
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
						g.Request("https://github.com" + user + "/following")
						g.stars.Request("https://github.com/stars" + user)
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

func (g *GitHubFollowing) Crawl() {
	g.fetchedUrl.Lock()
	if _, ok := g.fetchedUrl.m[g.url]; ok {
		g.fetchedUrl.Unlock()
		return
	}
	g.fetchedUrl.Unlock()

	doc, err := fetch_by_interface(g.url)
	if err != nil {
		go func(u string) {
			g.Request(u)
		}(g.url)
		return
	}

	g.fetchedUrl.Lock()
	g.fetchedUrl.m[g.url] = err
	g.fetchedUrl.Unlock()

	name := <-g.Parse(doc)
	g.result <- FollowingResult{g.url, name}
}

func (g *GitHubStars) Request(url string) {
	g.p.request <- &GitHubStars{
		fetchedUrl: g.fetchedUrl,
		fetchedRepo: g.fetchedRepo,
		p: g.p,
		result: g.result,
		url: url,
	}
}

func (g *GitHubStars) Parse(doc *html.Node) <-chan string {
	repo := make(chan string)

	go func() {
		defer close(repo)
		var f func(*html.Node)
		f = func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "span" {
				for _, a := range n.Attr {
					if a.Key == "class" && a.Val == "prefix" {
						repo <- n.Parent.Attr[0].Val
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

	return repo
}

func (g *GitHubStars) Crawl() {
	g.fetchedUrl.Lock()
	if _, ok := g.fetchedUrl.m[g.url]; ok {
		g.fetchedUrl.Unlock()
		return
	}
	g.fetchedUrl.Unlock()

	doc, err := fetch_by_interface(g.url)
	if err != nil {
		go func(u string) {
			g.Request(u)
		}(g.url)
		return
	}

	g.fetchedUrl.Lock()
	g.fetchedUrl.m[g.url] = err
	g.fetchedUrl.Unlock()

	repositories := g.Parse(doc)

	for r := range repositories {
		g.fetchedRepo.Lock()
		if _, ok := g.fetchedRepo.m[r]; !ok {
			g.result <- StarsResult{r}
			g.fetchedRepo.m[r] = struct{}{}
		}
		g.fetchedRepo.Unlock()
	}
}

type Pipeline struct {
	request chan Crawler
	done    chan struct{}
	wg      *sync.WaitGroup
}

func NewPipeline() *Pipeline {
	return &Pipeline{
		request: make(chan Crawler),
		done: make(chan struct{}),
		wg: new(sync.WaitGroup),
	}
}

func (p *Pipeline) Worker() {
	for r := range p.request {
		select {
		case <-p.done:
			return
		default:
			r.Crawl()
		}
	}
}

func (p *Pipeline) Run() {
	const numWorkers = 50
	p.wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			p.Worker()
			p.wg.Done()
		}()
	}

	go func() {
		p.wg.Wait()
	}()
}

func main() {
	fmt.Println(runtime.GOMAXPROCS(0))

	p := NewPipeline()
	p.Run()

	stars := &GitHubStars{
		fetchedUrl: &FetchedUrl{m: make(map[string]error)},
		fetchedRepo: &FetchedRepo{m: make(map[string]struct{})},
		p: p,
		result: make(chan StarsResult),
	}

	following := &GitHubFollowing{
		fetchedUrl: &FetchedUrl{m: make(map[string]error)},
		p: p,
		result: make(chan FollowingResult),
		stars: stars,
		url: "https://github.com/hyunsuk/following",
	}

	p.request <- following

	count := 0
	LOOP:
	for {
		select {
		case f := <-following.result:
			fmt.Println(f.name)
		case s := <-stars.result:
			fmt.Println(s.repo)
			if count == 10000 {
				close(p.done)
				break LOOP
			}
			count++
		}
	}
}