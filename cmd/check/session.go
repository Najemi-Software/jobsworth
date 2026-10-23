package check

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/najemi-software/jobsworth/internal/cache"
	"github.com/najemi-software/jobsworth/internal/check"
	"github.com/najemi-software/jobsworth/internal/cli"
	"github.com/najemi-software/jobsworth/internal/config"
	"github.com/najemi-software/jobsworth/internal/fetch"
	"github.com/najemi-software/jobsworth/internal/repolink"
	"github.com/najemi-software/jobsworth/internal/rules"
)

// Session is everything a run shares: the governing config, per-repo
// configs, resolved entries, repo links, the cache, and the fetch client.
// check builds one per invocation; `cache warm` reuses it so links, config
// and cache settings behave identically (spec-cache §2.4).
type Session struct {
	Base    config.Config
	Configs map[string]config.Config
	Roots   []string
	Entries []string
	Links   *repolink.Links
	Store   *cache.Cache
	Fetcher *fetch.Client
}

// NewSession reads flags (config, repo, cache-dir, offline, no-cache) and
// the environment per spec-init §4.4. With expandEntries the positional
// args are resolved to entry files; otherwise Entries is nil.
func NewSession(args []string, flags map[string][]string, env Env, expandEntries bool) (*Session, error) {
	first := func(name string) string {
		if v := flags[name]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	_, offline := flags["offline"]
	_, noCache := flags["no-cache"]

	// Config (spec-init §4.4): validated before any file is read. The config
	// of the current directory's repo governs run-wide settings (entries,
	// fail_on, fetch, cache); each entry repo's own config governs its rules
	// and skips. --config replaces both.
	var explicitCfg *config.Config
	if p := first("config"); p != "" {
		c, err := config.Load(check.ExpandTilde(p, env.Home), rules.IsKnown)
		if err != nil {
			return nil, cli.Exit(cli.ExitUsage, err)
		}
		explicitCfg = &c
	}
	base := config.Defaults()
	if explicitCfg != nil {
		base = *explicitCfg
	} else {
		c, err := config.LoadDefault(check.RepoRoot(env.Cwd), rules.IsKnown)
		if err != nil {
			return nil, cli.Exit(cli.ExitUsage, err)
		}
		base = c
	}
	if base.Path != "" {
		for _, n := range base.Notes {
			fmt.Fprintf(env.Stderr, "note: %s: %s\n", base.Path, n)
		}
	}
	var entries []string
	if expandEntries {
		var err error
		entries, err = check.ExpandEntriesWith(args, base.Entries, env.Cwd, env.Home)
		if err != nil {
			return nil, cli.Exit(cli.ExitUsage, err)
		}
	}

	roots := make([]string, len(entries))
	configs := map[string]config.Config{}
	for i, e := range entries {
		roots[i] = check.RepoRoot(filepath.Dir(e))
		if explicitCfg != nil {
			configs[roots[i]] = *explicitCfg
			continue
		}
		if _, done := configs[roots[i]]; done {
			continue
		}
		c, err := config.LoadDefault(roots[i], rules.IsKnown)
		if err != nil {
			return nil, cli.Exit(cli.ExitUsage, err)
		}
		configs[roots[i]] = c
	}

	// Links (§4): explicit flags, then config repos, then implicit.
	links := repolink.New()
	for _, arg := range flags["repo"] {
		lk, err := repolink.ParseExplicit(arg, env.Cwd, env.Home)
		if err != nil {
			return nil, cli.Exit(cli.ExitUsage, err)
		}
		if err := repolink.AddExplicit(links, lk); err != nil {
			return nil, cli.Exit(cli.ExitUsage, err)
		}
	}
	for _, c := range configs {
		for slug, p := range c.Repos {
			if _, taken := repolink.LookupSlug(links, slug); taken {
				continue // flag wins over config
			}
			base := c.Dir
			if base == "" {
				base = env.Cwd
			}
			lk, err := repolink.ParseExplicit(slug+"="+p, base, env.Home)
			if err != nil {
				return nil, cli.Exit(cli.ExitUsage, fmt.Errorf("config repos: %w", err))
			}
			if err := repolink.AddExplicit(links, lk); err != nil {
				return nil, cli.Exit(cli.ExitUsage, err)
			}
			if st, err := os.Stat(filepath.Join(lk.Path, ".github")); err != nil || !st.IsDir() {
				fmt.Fprintf(env.Stderr, "warning: repos: %s: %s has no .github directory; link is active anyway\n", lk.Display, lk.Path)
			}
		}
	}
	if lk, ok := repolink.Detect(env.Cwd, repolink.CWD); ok {
		repolink.AddImplicit(links, lk)
	}
	for _, e := range entries {
		if lk, ok := repolink.Detect(filepath.Dir(e), repolink.Entry); ok {
			repolink.AddImplicit(links, lk)
		}
	}
	for _, lk := range repolink.All(links) {
		fmt.Fprintf(env.Stderr, "link: %s → %s (%s)\n", lk.Display, lk.Path, lk.Source)
	}

	// Cache and fetcher (§5, §6). Precedence: flag, env, config, default.
	cacheFlag := check.ExpandTilde(first("cache-dir"), env.Home)
	if cacheFlag == "" && env.Getenv("JOBSWORTH_CACHE_DIR") == "" && base.CacheDir != "" {
		cacheFlag = check.ExpandTilde(base.CacheDir, env.Home)
		if !filepath.IsAbs(cacheFlag) && base.Dir != "" {
			cacheFlag = filepath.Join(base.Dir, cacheFlag)
		}
	}
	cacheRoot, err := cache.DefaultRoot(cacheFlag, env.Getenv)
	if err != nil {
		return nil, cli.Exit(cli.ExitUsage, err)
	}
	store := cache.New(cacheRoot)
	store.Disabled = noCache
	store.TagTTL = base.CacheTagTTL
	store.BranchTTL = base.CacheBranchTTL
	apiURL := env.Getenv("JOBSWORTH_API_URL")
	if apiURL == "" {
		apiURL = base.FetchAPIURL
	}
	fetcher := fetch.New(apiURL, "", store)
	fetcher.Offline = offline
	// The token is resolved on the first real request (review 2): a run
	// whose entries never fetch does not shell out to gh.
	fetcher.LazyToken = func() (string, bool) {
		token, source := fetch.ResolveToken(env.Getenv, env.GHToken)
		return token, source != fetch.SourceNone
	}
	fetcher.OnNoToken = func() {
		fmt.Fprintln(env.Stderr, "warning: no GitHub token found (JOBSWORTH_TOKEN, GITHUB_TOKEN, GH_TOKEN, or gh auth); fetching unauthenticated")
	}
	fetcher.OnCacheError = func(err error) {
		fmt.Fprintf(env.Stderr, "warning: cache root %s is not writable (%v); continuing without caching\n", cacheRoot, err)
	}
	fetcher.HTTP.Timeout = base.FetchTimeout
	fetch.SetConcurrency(fetcher, base.FetchParallel)
	if env.StderrIsTerminal {
		fetcher.Progress = func(msg string) { fmt.Fprintln(env.Stderr, msg+"...") }
	}

	return &Session{Base: base, Configs: configs, Roots: roots, Entries: entries, Links: links, Store: store, Fetcher: fetcher}, nil
}

// WriteRunStats records the run's cache and API counters (spec-cache §3.2).
func WriteRunStats(store *cache.Cache, fetcher *fetch.Client, command string) {
	cache.WriteRunStats(store, cache.RunStats{
		Command:      command,
		At:           time.Now().UTC(),
		Hits:         store.Stats.Hits(),
		Misses:       store.Stats.Misses(),
		Stale:        store.Stats.Stale.Load(),
		APICalls:     fetcher.Stats.APICalls.Load(),
		FetchedBytes: fetcher.Stats.FetchedBytes.Load(),
	})
}
