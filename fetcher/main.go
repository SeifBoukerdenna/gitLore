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
	Name            string   `json:"name"`
	FullName        string   `json:"full_name"`
	Description     string   `json:"description"`
	Private         bool     `json:"private"`
	Fork            bool     `json:"fork"`
	Archived        bool     `json:"archived"`
	Disabled        bool     `json:"disabled"`
	Language        string   `json:"language"`
	SizeKB          int      `json:"size"`
	StargazersCount int      `json:"stargazers_count"`
	WatchersCount   int      `json:"watchers_count"`
	ForksCount      int      `json:"forks_count"`
	OpenIssuesCount int      `json:"open_issues_count"`
	DefaultBranch   string   `json:"default_branch"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
	PushedAt        string   `json:"pushed_at"`
	HTMLURL         string   `json:"html_url"`
	Homepage        string   `json:"homepage"`
	Topics          []string `json:"topics"`
	HasIssues       bool     `json:"has_issues"`
	HasProjects     bool     `json:"has_projects"`
	HasWiki         bool     `json:"has_wiki"`
	HasPages        bool     `json:"has_pages"`
	HasDownloads    bool     `json:"has_downloads"`
	Owner           struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"owner"`
	License struct {
		Key  string `json:"key"`
		Name string `json:"name"`
		SPDX string `json:"spdx_id"`
	} `json:"license"`
}

type commitListItem struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Date string `json:"date"`
		} `json:"author"`
		Message string `json:"message"`
	} `json:"commit"`
}

type weeklyStat struct {
	Total int   `json:"total"`
	Week  int64 `json:"w"`
	Days  []int `json:"days"`
}

type languageStats map[string]int

type contributor struct {
	Login         string `json:"login"`
	Contributions int    `json:"contributions"`
}

type outRepo struct {
	Name          string   `json:"name"`
	FullName      string   `json:"full_name"`
	Description   string   `json:"description"`
	Private       bool     `json:"private"`
	Fork          bool     `json:"fork"`
	Archived      bool     `json:"archived"`
	Disabled      bool     `json:"disabled"`
	Language      string   `json:"language"`
	Topics        []string `json:"topics"`
	Homepage      string   `json:"homepage"`
	DefaultBranch string   `json:"default_branch"`

	// Size
	SizeKB       int    `json:"size_kb"`
	SizeReadable string `json:"size_readable"`

	// Engagement
	Stars      int `json:"stars"`
	Forks      int `json:"forks"`
	Watchers   int `json:"watchers"`
	OpenIssues int `json:"open_issues"`

	// Timestamps
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	PushedAt  string `json:"pushed_at"`

	// URLs
	HTMLURL string `json:"html_url"`

	// Owner
	OwnerLogin string `json:"owner_login"`
	OwnerType  string `json:"owner_type"`

	// License
	License string `json:"license"`

	// Features
	HasIssues    bool `json:"has_issues"`
	HasProjects  bool `json:"has_projects"`
	HasWiki      bool `json:"has_wiki"`
	HasPages     bool `json:"has_pages"`
	HasDownloads bool `json:"has_downloads"`

	// Enrichment data
	LastCommitAt      string         `json:"last_commit_at"`
	LastCommitMessage string         `json:"last_commit_message"`
	WeeklyCommits52W  []int          `json:"weekly_commits_52w"`
	WeeklyStats52W    []weeklyStat   `json:"weekly_stats_52w"`
	LanguageBreakdown map[string]int `json:"language_breakdown"`
	TopContributors   []contributor  `json:"top_contributors"`
	ContributorCount  int            `json:"contributor_count"`
	TotalCommits      int            `json:"total_commits"`
	StatsCachePending bool           `json:"stats_cache_pending"`
}

type summary struct {
	GeneratedAt string `json:"generated_at"`

	RepoCounts struct {
		Total    int `json:"total"`
		Public   int `json:"public"`
		Private  int `json:"private"`
		Archived int `json:"archived"`
		Forks    int `json:"forks"`
		Org      int `json:"org_owned_or_member"`
		User     int `json:"user_owned"`
	} `json:"repo_counts"`

	Size struct {
		TotalKB int    `json:"total_kb"`
		Human   string `json:"human"`
	} `json:"size"`

	Engagement struct {
		TotalStars    int `json:"total_stars"`
		TotalForks    int `json:"total_forks"`
		TotalWatchers int `json:"total_watchers"`
		TotalCommits  int `json:"total_commits"`
	} `json:"engagement"`

	Languages map[string]int `json:"languages"`
	Topics    map[string]int `json:"topics"`
	Licenses  map[string]int `json:"licenses"`

	Activity struct {
		MostRecentUpdate string `json:"most_recent_update"`
		MostRecentPush   string `json:"most_recent_push"`
		OldestCreated    string `json:"oldest_created"`
		OldestUpdate     string `json:"oldest_update"`
	} `json:"activity"`

	Enrichment struct {
		ReposWithLastCommit   int `json:"repos_with_last_commit"`
		ReposWithStats52W     int `json:"repos_with_stats_52w"`
		ReposWithLanguages    int `json:"repos_with_languages"`
		ReposWithContributors int `json:"repos_with_contributors"`
		ReposStatsPending     int `json:"repos_stats_pending"`
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

func fetchLastCommit(client *http.Client, token, fullName string) (string, string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/commits?per_page=1", fullName)
	status, body, err := doGET(client, url, token)
	if err != nil {
		return "", "", err
	}
	if status < 200 || status >= 300 {
		return "", "", fmt.Errorf("commits list error %d", status)
	}

	var commits []commitListItem
	if err := json.Unmarshal(body, &commits); err != nil {
		return "", "", err
	}
	if len(commits) == 0 {
		return "", "", nil
	}

	msg := commits[0].Commit.Message
	if len(msg) > 100 {
		msg = msg[:100] + "..."
	}

	return commits[0].Commit.Author.Date, msg, nil
}

func fetchCommitActivity52W(client *http.Client, token, fullName string) ([]weeklyStat, bool, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/stats/commit_activity", fullName)

	backoffs := []time.Duration{700 * time.Millisecond, 1200 * time.Millisecond, 2000 * time.Millisecond, 3000 * time.Millisecond}
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		status, body, e := doGET(client, url, token)
		if e != nil {
			return nil, false, e
		}

		if status == 202 {
			if attempt == len(backoffs) {
				return nil, true, nil
			}
			time.Sleep(backoffs[attempt])
			continue
		}

		if status < 200 || status >= 300 {
			return nil, false, fmt.Errorf("commit_activity error %d", status)
		}

		var weeks []weeklyStat
		if err := json.Unmarshal(body, &weeks); err != nil {
			return nil, false, err
		}
		return weeks, false, nil
	}

	return nil, true, nil
}

func fetchLanguages(client *http.Client, token, fullName string) (map[string]int, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/languages", fullName)
	status, body, err := doGET(client, url, token)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("languages error %d", status)
	}

	var langs map[string]int
	if err := json.Unmarshal(body, &langs); err != nil {
		return nil, err
	}
	return langs, nil
}

func fetchContributors(client *http.Client, token, fullName string) ([]contributor, int, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/contributors?per_page=10", fullName)
	status, body, err := doGET(client, url, token)
	if err != nil {
		return nil, 0, err
	}
	if status < 200 || status >= 300 {
		return nil, 0, fmt.Errorf("contributors error %d", status)
	}

	var contribs []contributor
	if err := json.Unmarshal(body, &contribs); err != nil {
		return nil, 0, err
	}

	// Total count can be derived from pagination, but for simplicity we'll use what we got
	total := len(contribs)
	if len(contribs) == 10 {
		// There might be more, but we cap at top 10 for display
		total = 10
	}

	return contribs, total, nil
}

func main() {
	_ = godotenv.Load()
	token := mustToken()

	client := &http.Client{Timeout: 30 * time.Second}

	fmt.Println("🔍 Fetching accessible repositories...")
	repos, err := fetchAllAccessibleRepos(client, token)
	if err != nil {
		panic(err)
	}
	fmt.Printf("✓ Found %d repositories\n\n", len(repos))

	// Base output objects
	out := make([]outRepo, 0, len(repos))
	for _, r := range repos {
		license := ""
		if r.License.Key != "" {
			license = r.License.Name
		}

		out = append(out, outRepo{
			Name:          r.Name,
			FullName:      r.FullName,
			Description:   r.Description,
			Private:       r.Private,
			Fork:          r.Fork,
			Archived:      r.Archived,
			Disabled:      r.Disabled,
			Language:      r.Language,
			Topics:        r.Topics,
			Homepage:      r.Homepage,
			DefaultBranch: r.DefaultBranch,
			SizeKB:        r.SizeKB,
			SizeReadable:  humanSizeFromKB(r.SizeKB),
			Stars:         r.StargazersCount,
			Forks:         r.ForksCount,
			Watchers:      r.WatchersCount,
			OpenIssues:    r.OpenIssuesCount,
			CreatedAt:     r.CreatedAt,
			UpdatedAt:     r.UpdatedAt,
			PushedAt:      r.PushedAt,
			HTMLURL:       r.HTMLURL,
			OwnerLogin:    r.Owner.Login,
			OwnerType:     r.Owner.Type,
			License:       license,
			HasIssues:     r.HasIssues,
			HasProjects:   r.HasProjects,
			HasWiki:       r.HasWiki,
			HasPages:      r.HasPages,
			HasDownloads:  r.HasDownloads,
		})
	}

	// Enrich concurrently
	fmt.Println("🔧 Enriching repositories with detailed data...")
	workers := 6 // Reduced to be gentler on rate limits
	jobs := make(chan int, len(out))
	var wg sync.WaitGroup
	var mu sync.Mutex

	completed := 0
	total := len(out)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				full := out[i].FullName

				// 1) Last commit + message
				lastDate, lastMsg, e := fetchLastCommit(client, token, full)
				if e == nil {
					out[i].LastCommitAt = lastDate
					out[i].LastCommitMessage = lastMsg
				}

				// 2) 52w activity stats
				weeks, pending, e2 := fetchCommitActivity52W(client, token, full)
				if e2 == nil {
					out[i].WeeklyStats52W = weeks
					out[i].StatsCachePending = pending

					// Extract simple totals
					totals := make([]int, len(weeks))
					totalCommits := 0
					for idx, w := range weeks {
						totals[idx] = w.Total
						totalCommits += w.Total
					}
					out[i].WeeklyCommits52W = totals
					out[i].TotalCommits = totalCommits
				}

				// 3) Language breakdown
				langs, e3 := fetchLanguages(client, token, full)
				if e3 == nil && len(langs) > 0 {
					out[i].LanguageBreakdown = langs
				}

				// 4) Contributors (top 10)
				contribs, count, e4 := fetchContributors(client, token, full)
				if e4 == nil {
					out[i].TopContributors = contribs
					out[i].ContributorCount = count
				}

				mu.Lock()
				completed++
				if completed%5 == 0 || completed == total {
					fmt.Printf("  Progress: %d/%d repositories enriched\n", completed, total)
				}
				mu.Unlock()

				// Small delay to respect rate limits
				time.Sleep(100 * time.Millisecond)
			}
		}()
	}

	for i := range out {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	fmt.Println("\n📊 Building summary...")

	// Build comprehensive summary
	var sum summary
	sum.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	sum.Languages = map[string]int{}
	sum.Topics = map[string]int{}
	sum.Licenses = map[string]int{}

	var newestUpdate, newestPush, oldestCreated, oldestUpdate time.Time
	var hasUpdate, hasPush, hasCreated, hasOldUpdate bool

	for _, r := range out {
		sum.RepoCounts.Total++

		if r.Private {
			sum.RepoCounts.Private++
		} else {
			sum.RepoCounts.Public++
		}

		if r.Archived {
			sum.RepoCounts.Archived++
		}

		if r.Fork {
			sum.RepoCounts.Forks++
		}

		if r.OwnerType == "Organization" {
			sum.RepoCounts.Org++
		} else {
			sum.RepoCounts.User++
		}

		sum.Size.TotalKB += r.SizeKB
		sum.Engagement.TotalStars += r.Stars
		sum.Engagement.TotalForks += r.Forks
		sum.Engagement.TotalWatchers += r.Watchers
		sum.Engagement.TotalCommits += r.TotalCommits

		if r.Language != "" {
			sum.Languages[r.Language]++
		}

		for _, topic := range r.Topics {
			sum.Topics[topic]++
		}

		if r.License != "" {
			sum.Licenses[r.License]++
		}

		// Timestamps
		if t, err := time.Parse(time.RFC3339, r.UpdatedAt); err == nil {
			if !hasUpdate || t.After(newestUpdate) {
				newestUpdate = t
				hasUpdate = true
			}
			if !hasOldUpdate || t.Before(oldestUpdate) {
				oldestUpdate = t
				hasOldUpdate = true
			}
		}

		if t, err := time.Parse(time.RFC3339, r.PushedAt); err == nil {
			if !hasPush || t.After(newestPush) {
				newestPush = t
				hasPush = true
			}
		}

		if t, err := time.Parse(time.RFC3339, r.CreatedAt); err == nil {
			if !hasCreated || t.Before(oldestCreated) {
				oldestCreated = t
				hasCreated = true
			}
		}

		// Enrichment counters
		if r.LastCommitAt != "" {
			sum.Enrichment.ReposWithLastCommit++
		}
		if len(r.WeeklyCommits52W) > 0 {
			sum.Enrichment.ReposWithStats52W++
		}
		if len(r.LanguageBreakdown) > 0 {
			sum.Enrichment.ReposWithLanguages++
		}
		if len(r.TopContributors) > 0 {
			sum.Enrichment.ReposWithContributors++
		}
		if r.StatsCachePending {
			sum.Enrichment.ReposStatsPending++
		}
	}

	sum.Size.Human = humanSizeFromKB(sum.Size.TotalKB)
	if hasUpdate {
		sum.Activity.MostRecentUpdate = newestUpdate.UTC().Format(time.RFC3339)
	}
	if hasPush {
		sum.Activity.MostRecentPush = newestPush.UTC().Format(time.RFC3339)
	}
	if hasCreated {
		sum.Activity.OldestCreated = oldestCreated.UTC().Format(time.RFC3339)
	}
	if hasOldUpdate {
		sum.Activity.OldestUpdate = oldestUpdate.UTC().Format(time.RFC3339)
	}

	// Write JSON files
	fmt.Println("\n💾 Writing output files...")

	indexJSON, _ := json.MarshalIndent(out, "", "  ")
	summaryJSON, _ := json.MarshalIndent(sum, "", "  ")

	_ = os.WriteFile("../repos_index_enriched.json", indexJSON, 0644)
	_ = os.WriteFile("../repos_summary.json", summaryJSON, 0644)

	fmt.Println("\n✨ Generated:")
	fmt.Println("   📄 repos_index_enriched.json")
	fmt.Println("   📊 repos_summary.json")
	fmt.Printf("\n📈 Stats:\n")
	fmt.Printf("   Repositories: %d\n", len(out))
	fmt.Printf("   Total Stars: %d\n", sum.Engagement.TotalStars)
	fmt.Printf("   Total Commits: %d\n", sum.Engagement.TotalCommits)
	fmt.Printf("   Stats pending (202): %d\n", sum.Enrichment.ReposStatsPending)
	fmt.Println()
}
