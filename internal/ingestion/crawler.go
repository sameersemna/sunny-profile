package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
)

// Source is a URL + label to ingest
type Source struct {
	URL      string `yaml:"url"       json:"url"`
	Label    string `yaml:"label"     json:"label"`
	MaxDepth int    `yaml:"max_depth" json:"max_depth"`
}

// Chunk is one extracted piece of content
type Chunk struct {
	ID        string            `json:"id"`
	SourceURL string            `json:"source_url"`
	Label     string            `json:"label"`
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Metadata  map[string]string `json:"metadata"`
	CreatedAt time.Time         `json:"created_at"`
}

// Crawler fetches and extracts text from URLs
type Crawler struct {
	http    *http.Client
	visited map[string]bool
}

func NewCrawler() *Crawler {
	return &Crawler{
		http:    &http.Client{Timeout: 20 * time.Second},
		visited: make(map[string]bool),
	}
}

func (c *Crawler) Crawl(ctx context.Context, src Source) ([]Chunk, error) {
	c.visited = make(map[string]bool)
	return c.crawlURL(ctx, src, src.URL, 0)
}

func (c *Crawler) crawlURL(ctx context.Context, src Source, rawURL string, depth int) ([]Chunk, error) {
	if c.visited[rawURL] {
		return nil, nil
	}
	c.visited[rawURL] = true

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (SunnyBot/1.0 personal-ai)")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "text/plain") {
		return nil, nil
	}

	chunks, links, err := c.extractChunks(rawURL, src.Label, body)
	if err != nil {
		return nil, err
	}

	if depth < src.MaxDepth {
		baseDomain := extractDomain(rawURL)
		for _, link := range links {
			if extractDomain(link) == baseDomain && !c.visited[link] {
				sub, _ := c.crawlURL(ctx, src, link, depth+1)
				chunks = append(chunks, sub...)
				if len(chunks) > 200 {
					break
				}
			}
		}
	}
	return chunks, nil
}

func (c *Crawler) extractChunks(pageURL, label string, body []byte) ([]Chunk, []string, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	title := strings.TrimSpace(doc.Find("title").First().Text())
	doc.Find("script, style, nav, footer, .ad").Remove()

	var mainText string
	main := doc.Find("main, article, .content, #content, [role=main]")
	if main.Length() > 0 {
		mainText = cleanText(main.Text())
	} else {
		mainText = cleanText(doc.Find("body").Text())
	}

	var links []string
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok {
			return
		}
		abs := resolveURL(pageURL, href)
		if abs != "" && strings.HasPrefix(abs, "http") {
			links = append(links, abs)
		}
	})

	textChunks := splitIntoChunks(mainText, 500)
	var chunks []Chunk
	for i, tc := range textChunks {
		if len(strings.TrimSpace(tc)) < 50 {
			continue
		}
		chunks = append(chunks, Chunk{
			ID:        uuid.New().String(),
			SourceURL: pageURL,
			Label:     label,
			Title:     title,
			Content:   tc,
			Metadata:  map[string]string{"chunk_index": fmt.Sprintf("%d", i), "url": pageURL},
			CreatedAt: time.Now(),
		})
	}
	return chunks, links, nil
}

func splitIntoChunks(text string, wordsPerChunk int) []string {
	words := strings.Fields(text)
	var out []string
	for i := 0; i < len(words); i += wordsPerChunk {
		end := i + wordsPerChunk
		if end > len(words) {
			end = len(words)
		}
		out = append(out, strings.Join(words[i:end], " "))
	}
	return out
}

func cleanText(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, " ")
}

func extractDomain(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}

func resolveURL(base, href string) string {
	if href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
		return ""
	}
	if strings.HasPrefix(href, "http") {
		return href
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	return baseURL.ResolveReference(ref).String()
}

// GitHubCrawler extracts structured profile data via the GitHub API
type GitHubCrawler struct {
	http *http.Client
}

func NewGitHubCrawler() *GitHubCrawler {
	return &GitHubCrawler{http: &http.Client{Timeout: 15 * time.Second}}
}

type ghUser struct {
	Login       string `json:"login"`
	Name        string `json:"name"`
	Bio         string `json:"bio"`
	Company     string `json:"company"`
	Location    string `json:"location"`
	Blog        string `json:"blog"`
	Followers   int    `json:"followers"`
	PublicRepos int    `json:"public_repos"`
}

type ghRepo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Language    string   `json:"language"`
	Stars       int      `json:"stargazers_count"`
	Topics      []string `json:"topics"`
}

func (g *GitHubCrawler) ExtractProfile(ctx context.Context, username string) ([]Chunk, error) {
	var user ghUser
	if err := g.fetchJSON(ctx, "https://api.github.com/users/"+username, &user); err != nil {
		return nil, err
	}
	var repos []ghRepo
	_ = g.fetchJSON(ctx, fmt.Sprintf("https://api.github.com/users/%s/repos?per_page=100&sort=updated", username), &repos)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("GitHub Profile: %s\n", user.Name))
	sb.WriteString(fmt.Sprintf("Username: %s | Repos: %d | Followers: %d\n", user.Login, user.PublicRepos, user.Followers))
	if user.Bio != "" {
		sb.WriteString("Bio: " + user.Bio + "\n")
	}
	if user.Company != "" {
		sb.WriteString("Company: " + user.Company + "\n")
	}
	sb.WriteString("\nNotable repositories:\n")
	langs := map[string]int{}
	for _, r := range repos {
		if r.Description == "" && r.Stars == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s: %s", r.Name, r.Description))
		if r.Language != "" {
			sb.WriteString(" [" + r.Language + "]")
			langs[r.Language]++
		}
		if r.Stars > 0 {
			sb.WriteString(fmt.Sprintf(" stars:%d", r.Stars))
		}
		sb.WriteString("\n")
	}
	if len(langs) > 0 {
		var ll []string
		for l := range langs {
			ll = append(ll, l)
		}
		sb.WriteString("Languages: " + strings.Join(ll, ", ") + "\n")
	}

	return []Chunk{{
		ID:        uuid.New().String(),
		SourceURL: "https://github.com/" + username,
		Label:     "github",
		Title:     "GitHub: " + user.Name,
		Content:   sb.String(),
		Metadata:  map[string]string{"type": "github_profile", "username": username},
		CreatedAt: time.Now(),
	}}, nil
}

func (g *GitHubCrawler) fetchJSON(ctx context.Context, rawURL string, v interface{}) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}
