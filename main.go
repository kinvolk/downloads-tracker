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

	// fetch default org and repos from environment variables
	defaultOrg := os.Getenv("GITHUB_ORG")
	if defaultOrg == "" {
		log.Fatal("GITHUB_ORG not set")
	}

	defaultReposEnv := os.Getenv("GITHUB_REPOS")
	if defaultReposEnv == "" {
		log.Fatal("GITHUB_REPOS not set")
	}
	defaultRepos := strings.Split(defaultReposEnv, ",")

	// fetch org and repos for assets (release downloads)
	assetsOrg := getEnvWithDefault("ASSETS_GITHUB_ORG", defaultOrg)
	assetsReposEnv := getEnvWithDefault("ASSETS_GITHUB_REPOS", defaultReposEnv)
	assetsRepos := strings.Split(assetsReposEnv, ",")

	// fetch org and repos for container images
	imageOrg := getEnvWithDefault("IMAGE_GITHUB_ORG", defaultOrg)
	imageReposEnv := getEnvWithDefault("IMAGE_GITHUB_REPOS", defaultReposEnv)
	imageRepos := strings.Split(imageReposEnv, ",")

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

	// register prometheus metrics
	prometheus.MustRegister(releaseCounter)
	prometheus.MustRegister(containerCounter)
	prometheus.MustRegister(starCounter)

	client := gitClient(token)

	// Process GitHub repositories for star count metrics
	for _, repo := range defaultRepos {
		log.Printf("Processing star count for repo: %s in org: %s\n", repo, defaultOrg)
		// fetch star count
		repoInfo, _, err := client.Repositories.Get(context.TODO(), defaultOrg, repo)
		if err != nil {
			log.Printf("Error fetching star count info for repo:%s,err:%v\n", repo, err)
			continue
		}
		starCounter.With(prometheus.Labels{"repository": repo}).Add(float64(*repoInfo.StargazersCount))
	}

	// Process release asset metrics
	for _, repo := range assetsRepos {
		log.Printf("Processing release assets for repo: %s in org: %s\n", repo, assetsOrg)
		// fetch release info
		releaseInfos, err := fetchReleaseInfo(client, assetsOrg, repo)
		if err != nil {
			log.Printf("Error fetching release info for repo:%s, err:%v\n", repo, err)
			continue
		}
		// iterate over releases and set metrics
		for _, releaseInfo := range releaseInfos {
			for _, releaseAsset := range releaseInfo.Assets {
				releaseCounter.With(prometheus.Labels{
					"repository":   repo,
					"tag":          releaseInfo.TagName,
					"name":         *releaseAsset.Name,
					"content_type": *releaseAsset.ContentType,
				}).Add(float64(*releaseAsset.DownloadCount))
			}
		}
	}

	// Process container image metrics
	for _, repo := range imageRepos {
		log.Printf("Processing container images for repo: %s in org: %s\n", repo, imageOrg)

		url := fmt.Sprintf("https://github.com/orgs/%s/packages/container/%s/versions", imageOrg, repo)
		containerDownloads := make(map[string]int)
		page := 1

		for {
			fmt.Println(url)
			containerDownloadsPage, hasMore, err := processContainerPackagesURL(url)
			if err != nil {
				log.Printf("Error fetching container info for repo:%s, err:%v\n", repo, err)
				break
			}
			// Stop pagination if there are no more items on the page
			if !hasMore {
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
	}

	// push metrics to pushgateway
	log.Println("Pushing metrics to pushgateway 🏹")
	metricJobName := "download_metrics"
	if len(defaultRepos) > 0 {
		metricJobName = fmt.Sprintf("download_metrics_%s", defaultRepos[0])
	}
	err := push.New(pushgateway, metricJobName).
		BasicAuth(pushgatewayUsername, pushgatewayPassword).
		Collector(releaseCounter).
		Collector(containerCounter).
		Collector(starCounter).
		Push()

	if err != nil {
		log.Fatal("Error pushing metrics", err)
	}
	log.Println("👏 Successfully pushed metrics to pushgateway 👋")
}

// getEnvWithDefault returns the value of the environment variable or a default if not set
func getEnvWithDefault(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
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
// and a boolean indicating if the page has any versions (to continue pagination)
func processContainerPackagesURL(url string) (map[string]int, bool, error) {

	var containerDownloads = make(map[string]int)

	doc, err := htmlquery.LoadURL(url)
	if err != nil {
		return nil, false, fmt.Errorf("error fetching: %q ,err:%v", url, err)
	}

	// Find all list items with class "Box-row" inside the versions-list
	list := htmlquery.Find(doc, `//div[@id="versions-list"]//li[@class="Box-row"]`)

	// If there are no items at all, we've reached the end of pagination
	if len(list) == 0 {
		return containerDownloads, false, nil
	}

	for _, item := range list {
		// Try to find version tag(s) - these are Label elements for tagged versions
		versionTags := htmlquery.Find(item, `.//a[contains(@class, "Label")]`)

		var version string
		if len(versionTags) > 0 {
			// Tagged version - extract tag names
			var tags []string
			for _, tag := range versionTags {
				tagText := strings.TrimSpace(htmlquery.InnerText(tag))
				if tagText != "" {
					tags = append(tags, tagText)
				}
			}
			// Use the first non-"latest" tag, or "latest" if that's all we have
			for _, tag := range tags {
				if tag != "latest" {
					version = tag
					break
				}
			}
			if version == "" && len(tags) > 0 {
				version = tags[0]
			}
		} else {
			// Untagged version (SHA256 checksum) - skip these
			continue
		}

		if version == "" {
			log.Printf("Could not extract version from item\n")
			continue
		}

		// Find download count - it's the text node after the download icon
		// The download icon is an SVG with class "octicon-download"
		downloadNodes := htmlquery.Find(item, `.//svg[contains(@class, "octicon-download")]/following-sibling::text()[1]`)

		var count int
		if len(downloadNodes) > 0 {
			countStr := strings.TrimSpace(htmlquery.InnerText(downloadNodes[0]))
			countStr = strings.ReplaceAll(countStr, ",", "")
			var err error
			count, err = strconv.Atoi(countStr)
			if err != nil {
				log.Printf("Error parsing download count '%s' for version %s: %v\n", countStr, version, err)
				continue
			}
		} else {
			// No download count found, default to 0
			count = 0
		}

		containerDownloads[version] = count
	}
	// Return true to indicate there were items on this page (continue pagination)
	return containerDownloads, true, nil
}
