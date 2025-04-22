package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/antchfx/htmlquery"
	"github.com/google/go-github/v45/github"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"golang.org/x/net/html"
	"golang.org/x/oauth2"
)

// ReleaseCounter is a prometheus counter metric for release downloads
var releaseCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "release_downloads",
	Help: "Number of downloads for a given release",
}, []string{"repository", "tag", "name", "content_type"})

// ContainerCounter is a prometheus counter metric for container downloads
var containerCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "container_downloads",
	Help: "Number of downloads of containers",
}, []string{"repository", "version"})

// StarCounter is a prometheus counter metric for star counts
var starCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "star_count",
	Help: "Number of stars",
}, []string{"repository"})

func main() {

	// fetch token from environment variable
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN not set")
	}

	// fetch org and repo from environment variable
	org := os.Getenv("GITHUB_ORG")
	if org == "" {
		log.Fatal("GITHUB_ORG not set")
	}

	// fetch command separated repos from environment variable
	reposEnv := os.Getenv("GITHUB_REPOS")
	if reposEnv == "" {
		log.Fatal("GITHUB_REPOS not set")
	}

	// fetch pushgateway url from environment variable
	pushgateway := os.Getenv("PUSHGATEWAY_URL")
	if pushgateway == "" {
		log.Fatal("PUSHGATEWAY_URL not set")
	}

	// fetch push gateway username and password from environment variable
	pushgatewayUsername := os.Getenv("PUSHGATEWAY_USERNAME")
	if pushgatewayUsername == "" {
		log.Fatal("PUSHGATEWAY_USERNAME not set")
	}
	pushgatewayPassword := os.Getenv("PUSHGATEWAY_PASSWORD")
	if pushgatewayPassword == "" {
		log.Fatal("PUSHGATEWAY_PASSWORD not set")
	}

	// split repo string to array
	repos := strings.Split(reposEnv, ",")

	// register prometheus metrics
	prometheus.MustRegister(releaseCounter)
	prometheus.MustRegister(containerCounter)
	prometheus.MustRegister(starCounter)

	client := gitClient(token)
	for _, repo := range repos {
		log.Printf("Processing repo: %s\n", repo)
		// fetch release info
		releaseInfos, err := fetchReleaseInfo(client, org, repo)
		if err != nil {
			log.Printf("Error fetching release info for repo:%s, err:%v\n", repo, err)
			continue
		}
		// iterate over releases and set metrics
		for _, releaseInfo := range releaseInfos {
			for _, releaseAsset := range releaseInfo.Assets {
				releaseCounter.With(prometheus.Labels{"repository": repo, "tag": releaseInfo.TagName, "name": *releaseAsset.Name, "content_type": *releaseAsset.ContentType}).Add(float64(*releaseAsset.DownloadCount))
			}
		}
		// fetch container info
		url := fmt.Sprintf("https://github.com/%s/%s/pkgs/container/%s/versions", org, repo, repo)
		containerDownloads := make(map[string]int)
		page := 1
		for {
			fmt.Println(url)
			containerDownloadsPage, err := processContainerPackagesURL(url)
			if err != nil {
				log.Printf("Error fetching container info for repo:%s, err:%v\n", repo, err)
				break
			}
			if len(containerDownloadsPage) == 0 {
				break
			}
			for version, count := range containerDownloadsPage {
				containerDownloads[version] = count
			}

			url, _ = strings.CutSuffix(url, fmt.Sprintf("?page=%d", page))
			page++
			url = fmt.Sprintf("%s?page=%d", url, page)
		}
		// iterate over container downloads and set metrics
		for version, count := range containerDownloads {
			containerCounter.With(prometheus.Labels{"repository": repo, "version": version}).Add(float64(count))
		}
		// fetch star count
		repoInfo, _, err := client.Repositories.Get(context.TODO(), org, repo)
		if err != nil {
			log.Fatalf("Error fetching star count info for repo:%s,err:%v\n", repo, err)
		}
		starCounter.With(prometheus.Labels{"repository": repo}).Add(float64(*repoInfo.StargazersCount))

		// push metrics to pushgateway
		log.Println("Pushing metrics to pushgateway 🏹")
		err = push.New(pushgateway, fmt.Sprintf("download_metrics_%s", repo)).BasicAuth(pushgatewayUsername, pushgatewayPassword).Collector(releaseCounter).Collector(containerCounter).Collector(starCounter).Push()
		if err != nil {
			log.Fatal("Error pushing metrics", err)
		}
		log.Println("👏 Successfully pushed metrics to pushgateway 👋")
	}
}

type ReleaseInfo struct {
	TagName string
	Assets  []*github.ReleaseAsset
}

// fetchReleaseInfo fetches release info for a given repo
func fetchReleaseInfo(client *github.Client, org string, repo string) ([]ReleaseInfo, error) {
	releases, _, err := client.Repositories.ListReleases(context.TODO(), org, repo, nil)
	if err != nil {
		return nil, err
	}
	// iterate over releases and fetch assets
	var releaseInfos []ReleaseInfo
	for _, release := range releases {
		releaseAssets, _, err := client.Repositories.ListReleaseAssets(context.TODO(), org, repo, *release.ID, nil)
		if err != nil {
			return nil, err
		}
		releaseInfos = append(releaseInfos, ReleaseInfo{*release.TagName, releaseAssets})
	}
	return releaseInfos, nil
}

// gitClient is a github client
func gitClient(token string) *github.Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.TODO(), ts)
	return github.NewClient(tc)
}

// processContainerPackagesURL returns a map of container versions to download counts
func processContainerPackagesURL(url string) (map[string]int, error) {

	var containerDownloads = make(map[string]int)
	doc, err := htmlquery.LoadURL(url)
	if err != nil {
		return nil, fmt.Errorf("error fetching: %q ,err:%v", url, err)
	}

	list := htmlquery.Find(doc, `//*[@id="versions-list"]/ul/li`)
	for _, item := range list {
		versionNodes := htmlquery.Find(item, `/div/div[1]/div[1]/a`)
		downloadCountNodes := htmlquery.Find(item, `/div/div[2]/span/text()`)
		version := innerTextOfNodes(versionNodes)
		downloads := innerTextOfNodes(downloadCountNodes)
		if len(version) == 2 && version[0] == "latest" {
			version = []string{version[1]}
		}
		if len(downloads) == 2 {
			downloads[1] = strings.TrimPrefix(downloads[1], "\n")
			downloads[1] = strings.TrimLeft(downloads[1], " ")
			downloads[1] = strings.TrimRight(downloads[1], " ")
			downloads[1] = strings.TrimSuffix(downloads[1], "\n")
			downloads = []string{downloads[1]}
		}

		// Trim any whitespace around the download count
		countStr := strings.TrimSpace(downloads[0])
		// Remove decimal comma
		countStr = strings.ReplaceAll(countStr, ",", "")

		count, err := strconv.Atoi(countStr)
		if err != nil {
			return nil, err
		}
		containerDownloads[version[0]] = count
	}
	return containerDownloads, nil
}

// innerTextOfNodes returns the inner text of a list of html nodes
func innerTextOfNodes(nodes []*html.Node) []string {
	innerTexts := []string{}
	for _, node := range nodes {
		nodeText := strings.TrimSpace(htmlquery.InnerText(node))
		if nodeText != "" {
			innerTexts = append(innerTexts, htmlquery.InnerText(node))
		}
	}
	return innerTexts
}
