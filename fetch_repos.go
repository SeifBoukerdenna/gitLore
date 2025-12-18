package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

type ghRepo struct {
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
	Language    string `json:"language"`
	SizeKB      int    `json:"size"` // GitHub: KB (approx)
	UpdatedAt   string `json:"updated_at"`
	HTMLURL     string `json:"html_url"`
	Owner       struct {
		Login string `json:"login"`
		Type  string `json:"type"` // User / Organization
	} `json:"owner"`
}

type commitListItem struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Date string `json:"date"` // RFC3339
		} `json:"author"`
	} `json:"commit"`
}

type weeklyStat struct {
	Total int `json:"total"`
	// days array exists but we don't need it for a weekly view
}

type outRepo struct {
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
	Language    string `json:"language"`

	SizeKB       int    `json:"size_kb"`
	SizeReadable string `json:"size_readable"`

	UpdatedAt string `json:"updated_at"`
	HTMLURL   string `json:"html_url"`

	OwnerLogin string `json:"owner_login"`
	OwnerType  string `json:"owner_type"`

	// Enrichment
	LastCommitAt      string `json:"last_commit_at"`
	WeeklyCommits52W  []int  `json:"weekly_commits_52w"`
	StatsCachePending bool   `json:"stats_cache_pending"` // true if /stats returned 202 after retries
}

type summary struct {
	GeneratedAt string `json:"generated_at"`

	RepoCounts struct {
		Total   int `json:"total"`
		Public  int `json:"public"`
		Private int `json:"private"`
		Org     int `json:"org_owned_or_member"`
		User    int `json:"user_owned"`
	} `json:"repo_counts"`

	Size struct {
		TotalKB int    `json:"total_kb"`
		Human   string `json:"human"`
	} `json:"size"`

	Languages map[string]int `json:"languages"`

	Activity struct {
		MostRecentUpdate string `json:"most_recent_update"`
		OldestUpdate     string `json:"oldest_update"`
	} `json:"activity"`

	Enrichment struct {
		ReposWithLastCommit int `json:"repos_with_last_commit"`
		ReposWithStats52W   int `json:"repos_with_stats_52w"`
		ReposStatsPending   int `json:"repos_stats_pending"`
	} `json:"enrichment"`
}

func mustToken() string {
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		panic("GITHUB_TOKEN is missing. Put it in .env as: GITHUB_TOKEN=ghp_... (no quotes) or export it in your shell.")
	}
	return token
}

func humanSizeFromKB(kb int) string {
	bytes := float64(kb) * 1024
	if bytes <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := int(math.Floor(math.Log(bytes) / math.Log(1024)))
	if i < 0 {
		i = 0
	}
	if i >= len(units) {
		i = len(units) - 1
	}
	val := bytes / math.Pow(1024, float64(i))
	if units[i] == "B" || units[i] == "KB" {
		return fmt.Sprintf("%.0f %s", val, units[i])
	}
	return fmt.Sprintf("%.1f %s", val, units[i])
}

func doGET(client *http.Client, url string, token string) (int, []byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gitlore-enricher")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

func fetchAllAccessibleRepos(client *http.Client, token string) ([]ghRepo, error) {
	perPage := 100
	page := 1
	aff := "owner,collaborator,organization_member"

	var all []ghRepo
	for {
		url := fmt.Sprintf("https://api.github.com/user/repos?per_page=%d&page=%d&sort=updated&affiliation=%s",
			perPage, page, aff)

		status, body, err := doGET(client, url, token)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("github api error %d: %s", status, string(body))
		}

		var pageRepos []ghRepo
		if err := json.Unmarshal(body, &pageRepos); err != nil {
			return nil, err
		}
		if len(pageRepos) == 0 {
			break
		}
		all = append(all, pageRepos...)
		page++
	}
	return all, nil
}

func fetchLastCommitAt(client *http.Client, token, fullName string) (string, error) {
	// fullName: "owner/repo"
	url := fmt.Sprintf("https://api.github.com/repos/%s/commits?per_page=1", fullName)
	status, body, err := doGET(client, url, token)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("commits list error %d: %s", status, string(body))
	}

	var commits []commitListItem
	if err := json.Unmarshal(body, &commits); err != nil {
		return "", err
	}
	if len(commits) == 0 {
		return "", nil
	}
	return commits[0].Commit.Author.Date, nil
}

func fetchCommitActivity52W(client *http.Client, token, fullName string) (weekly []int, pending bool, err error) {
	// This endpoint can return 202 while stats are being generated.
	// We'll retry a few times with short backoff.
	url := fmt.Sprintf("https://api.github.com/repos/%s/stats/commit_activity", fullName)

	backoffs := []time.Duration{700 * time.Millisecond, 1200 * time.Millisecond, 2000 * time.Millisecond, 3000 * time.Millisecond}
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		status, body, e := doGET(client, url, token)
		if e != nil {
			return nil, false, e
		}

		if status == 202 {
			// stats being generated
			if attempt == len(backoffs) {
				return nil, true, nil
			}
			time.Sleep(backoffs[attempt])
			continue
		}

		if status < 200 || status >= 300 {
			return nil, false, fmt.Errorf("commit_activity error %d: %s", status, string(body))
		}

		var weeks []weeklyStat
		if err := json.Unmarshal(body, &weeks); err != nil {
			return nil, false, err
		}
		out := make([]int, 0, len(weeks))
		for _, w := range weeks {
			out = append(out, w.Total)
		}
		return out, false, nil
	}

	return nil, true, nil
}

func main() {
	_ = godotenv.Load() // loads .env if present
	token := mustToken()

	client := &http.Client{Timeout: 25 * time.Second}

	repos, err := fetchAllAccessibleRepos(client, token)
	if err != nil {
		panic(err)
	}

	// Base output objects
	out := make([]outRepo, 0, len(repos))
	for _, r := range repos {
		out = append(out, outRepo{
			Name:         r.Name,
			FullName:     r.FullName,
			Description:  r.Description,
			Private:      r.Private,
			Language:     r.Language,
			SizeKB:       r.SizeKB,
			SizeReadable: humanSizeFromKB(r.SizeKB),
			UpdatedAt:    r.UpdatedAt,
			HTMLURL:      r.HTMLURL,
			OwnerLogin:   r.Owner.Login,
			OwnerType:    r.Owner.Type,
		})
	}

	// Enrich concurrently but politely
	workers := 8
	jobs := make(chan int, len(out))
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				full := out[i].FullName

				// 1) last commit
				last, e := fetchLastCommitAt(client, token, full)
				if e == nil {
					out[i].LastCommitAt = last
				}

				// 2) 52w activity
				weeks, pending, e2 := fetchCommitActivity52W(client, token, full)
				if e2 == nil {
					out[i].WeeklyCommits52W = weeks
					out[i].StatsCachePending = pending
				}
			}
		}()
	}

	for i := range out {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	// Build summary
	var sum summary
	sum.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	sum.Languages = map[string]int{}

	var newest, oldest time.Time
	var newestSet, oldestSet bool

	for _, r := range out {
		sum.RepoCounts.Total++
		if r.Private {
			sum.RepoCounts.Private++
		} else {
			sum.RepoCounts.Public++
		}

		if r.OwnerType == "Organization" {
			sum.RepoCounts.Org++
		} else {
			sum.RepoCounts.User++
		}

		sum.Size.TotalKB += r.SizeKB
		if r.Language != "" {
			sum.Languages[r.Language]++
		}

		// updated_at summary
		t, err := time.Parse(time.RFC3339, r.UpdatedAt)
		if err == nil {
			if !newestSet || t.After(newest) {
				newest = t
				newestSet = true
			}
			if !oldestSet || t.Before(oldest) {
				oldest = t
				oldestSet = true
			}
		}

		// enrichment counters
		if r.LastCommitAt != "" {
			sum.Enrichment.ReposWithLastCommit++
		}
		if len(r.WeeklyCommits52W) > 0 {
			sum.Enrichment.ReposWithStats52W++
		}
		if r.StatsCachePending {
			sum.Enrichment.ReposStatsPending++
		}
	}

	sum.Size.Human = humanSizeFromKB(sum.Size.TotalKB)
	if newestSet {
		sum.Activity.MostRecentUpdate = newest.UTC().Format(time.RFC3339)
	}
	if oldestSet {
		sum.Activity.OldestUpdate = oldest.UTC().Format(time.RFC3339)
	}

	// Write JSON files
	indexJSON, _ := json.MarshalIndent(out, "", "  ")
	summaryJSON, _ := json.MarshalIndent(sum, "", "  ")

	_ = os.WriteFile("repos_index_enriched.json", indexJSON, 0644)
	_ = os.WriteFile("repos_summary.json", summaryJSON, 0644)

	fmt.Println("Generated:")
	fmt.Println(" - repos_index_enriched.json")
	fmt.Println(" - repos_summary.json")
	fmt.Printf("Repos: %d | Stats pending (202 after retries): %d\n", len(out), sum.Enrichment.ReposStatsPending)
}

