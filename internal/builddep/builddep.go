package builddep

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v52/github"
	"github.com/willabides/bindown/v3"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"golang.org/x/oauth2"
	"gopkg.in/yaml.v3"
)

//go:generate sh -c "go tool dist list > go_dist_list.txt"

//go:embed go_dist_list.txt
var _goDists string

func QueryGitHubRelease(ctx context.Context, repo, tag, tkn string) (urls []string, version, homepage, description string, _ error) {
	client := github.NewClient(oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: tkn},
	)))
	splitRepo := strings.Split(repo, "/")
	orgName, repoName := splitRepo[0], splitRepo[1]
	repoResp, _, err := client.Repositories.Get(ctx, orgName, repoName)
	if err != nil {
		return nil, "", "", "", err
	}
	description = repoResp.GetDescription()
	homepage = repoResp.GetHomepage()
	if homepage == "" {
		homepage = repoResp.GetHTMLURL()
	}
	var release *github.RepositoryRelease
	if tag == "" {
		release, _, err = client.Repositories.GetLatestRelease(ctx, orgName, repoName)
		if err != nil {
			return nil, "", "", "", err
		}
		tag = release.GetTagName()
	} else {
		release, _, err = client.Repositories.GetReleaseByTag(ctx, orgName, repoName, tag)
		if err != nil {
			return nil, "", "", "", err
		}
	}

	if version == "" {
		version = tag
		if strings.HasPrefix(version, "v") {
			_, err = semver.NewVersion(version[1:])
			if err == nil {
				version = version[1:]
			}
		}
	}
	for _, asset := range release.Assets {
		urls = append(urls, asset.GetBrowserDownloadURL())
	}
	return urls, version, homepage, description, nil
}

func AddDependency(
	ctx context.Context,
	cfg *bindown.Config,
	name, version string,
	homepage, description string,
	urls []string,
) error {
	return addDependency(ctx, cfg, name, version, homepage, description, urls, nil)
}

func addDependency(
	ctx context.Context,
	cfg *bindown.Config,
	name, version string,
	homepage, description string,
	urls []string,
	selector selectCandidateFunc,
) error {
	var systems []string
	if cfg.Systems != nil {
		for _, systemInfo := range cfg.Systems {
			systems = append(systems, systemInfo.String())
		}
	} else {
		systems = distSystems()
	}
	groups := parseDownloads(urls, name, version, systems)
	var regrouped []*depGroup
	for _, g := range groups {
		gg, err := g.regroupByArchivePath(ctx, name, version, selector)
		if err != nil {
			return err
		}
		regrouped = append(regrouped, gg...)
	}
	built := buildConfig(name, version, regrouped)
	err := built.AddChecksums([]string{name}, built.Dependencies[name].Systems)
	if err != nil {
		return err
	}
	err = built.Validate(nil, built.Systems)
	if err != nil {
		b, e := yaml.Marshal(&bindown.Config{
			Dependencies: built.Dependencies,
			Templates:    built.Templates,
			URLChecksums: built.URLChecksums,
		})
		if e != nil {
			b = []byte(fmt.Sprintf("could not marshal invalid config: %v", e))
		}
		return fmt.Errorf("generated config is invalid: %v\n\n%s", err, string(b))
	}
	for k, v := range built.Dependencies {
		if cfg.Dependencies == nil {
			cfg.Dependencies = make(map[string]*bindown.Dependency)
		}
		cfg.Dependencies[k] = v
	}
	for k, v := range built.Templates {
		if homepage != "" {
			v.Homepage = &homepage
		}
		if description != "" {
			v.Description = &description
		}
		if cfg.Templates == nil {
			cfg.Templates = make(map[string]*bindown.Dependency)
		}
		cfg.Templates[k] = v
	}
	for k, v := range built.URLChecksums {
		if cfg.URLChecksums == nil {
			cfg.URLChecksums = make(map[string]string)
		}
		cfg.URLChecksums[k] = v
	}
	return nil
}

var forbiddenOS = map[string]bool{
	"js": true,
}

var forbiddenArch = map[string]bool{
	"arm":  true,
	"wasm": true,
}

func distSystems() []string {
	return strings.Split(strings.TrimSpace(_goDists), "\n")
}

func parseDist(dist string) (os, arch string) {
	parts := strings.Split(dist, "/")
	if len(parts) != 2 {
		panic(fmt.Sprintf("invalid dist: %q", dist))
	}
	return parts[0], parts[1]
}

func systemOs(system string) string {
	os, _ := parseDist(system)
	return os
}

func systemArch(system string) string {
	_, arch := parseDist(system)
	return arch
}

type systemSub struct {
	val        string
	normalized string
	priority   int
	idx        int
}

func osSubs(systems []string) []systemSub {
	subs := []systemSub{
		{val: "apple-darwin", normalized: "darwin"},
		{val: "unknown-linux-gnu", normalized: "linux", priority: -1},
		{val: "unknown-linux-musl", normalized: "linux"},
		{val: "pc-windows-msvc", normalized: "windows"},
		{val: "pc-windows-gnu", normalized: "windows", priority: -1},
		{val: "apple", normalized: "darwin"},
		{val: "osx", normalized: "darwin"},
		{val: "macos", normalized: "darwin"},
		{val: "mac", normalized: "darwin"},
		{val: "windows", normalized: "windows"},
		{val: "darwin", normalized: "darwin"},
		{val: "win64", normalized: "windows"},
		{val: "win", normalized: "windows"},
	}
	if systems == nil {
		systems = distSystems()
	}
	for _, dist := range systems {
		distOS := systemOs(dist)
		if !slices.ContainsFunc(subs, func(sub systemSub) bool {
			return sub.val == distOS
		}) {
			subs = append(subs, systemSub{val: distOS, normalized: distOS})
		}
	}
	slices.SortFunc(subs, func(a, b systemSub) bool {
		return len(a.val) > len(b.val)
	})
	return subs
}

func archSubs(systems []string) []systemSub {
	subs := []systemSub{
		{val: "amd64", normalized: "amd64"},
		{val: "arm64", normalized: "arm64"},
		{val: "x86_64", normalized: "amd64"},
		{val: "x86_32", normalized: "386"},
		{val: "x86", normalized: "386"},
		{val: "x64", normalized: "amd64"},
		{val: "64bit", normalized: "amd64"},
		{val: "64-bit", normalized: "amd64"},
		{val: "aarch64", normalized: "arm64"},
		{val: "aarch_64", normalized: "arm64"},
		{val: "ppcle_64", normalized: "ppc64le"},
		{val: "s390x_64", normalized: "s390x"},
		{val: "i386", normalized: "386"},
	}
	if systems == nil {
		systems = distSystems()
	}
	for _, dist := range systems {
		distArch := systemArch(dist)
		if !slices.ContainsFunc(subs, func(sub systemSub) bool {
			return sub.val == distArch
		}) {
			subs = append(subs, systemSub{val: distArch, normalized: distArch})
		}
	}
	slices.SortFunc(subs, func(a, b systemSub) bool {
		return len(a.val) > len(b.val)
	})
	return subs
}

func matchSub(filename string, subs []systemSub) *systemSub {
	downcased := strings.ToLower(filename)
	for _, sub := range subs {
		idx := strings.Index(downcased, sub.val)
		if idx == -1 {
			continue
		}
		casedVal := filename[idx : idx+len(sub.val)]
		return &systemSub{
			val:        casedVal,
			normalized: sub.normalized,
			priority:   sub.priority,
			idx:        idx,
		}
	}
	return nil
}

func parseOs(filename string, systems []string) *systemSub {
	sub := matchSub(filename, osSubs(systems))
	if sub != nil {
		return sub
	}
	if strings.HasSuffix(strings.ToLower(filename), ".exe") {
		return &systemSub{
			val:        "",
			normalized: "windows",
			idx:        -1,
		}
	}
	return nil
}

func parseArch(filename string, systems []string) *systemSub {
	sub := matchSub(filename, archSubs(systems))
	if sub != nil {
		return sub
	}
	return &systemSub{
		val:        "",
		normalized: "amd64",
		idx:        -1,
		priority:   -1,
	}
}

var archiveSuffixes = []string{
	".tar.br",
	".tbr",
	".tar.bz2",
	".tbz2",
	".tar.gz",
	".tgz",
	".tar.lz4",
	".tlz4",
	".tar.sz",
	".tsz",
	".tar.xz",
	".txz",
	".tar.zst",
	".tzst",
	".rar",
	".zip",
	".br",
	".gz",
	".bz2",
	".lz4",
	".sz",
	".xz",
	".zst",
}

func parseDownload(dlURL, version string, systems []string) (*dlFile, bool) {
	tmpl := dlURL
	osSub := parseOs(dlURL, systems)
	if osSub == nil {
		return nil, false
	}
	if osSub.idx != -1 {
		tmpl = tmpl[:osSub.idx] + "{{.os}}" + tmpl[osSub.idx+len(osSub.val):]
	}
	archSub := parseArch(tmpl, systems)
	if archSub == nil {
		return nil, false
	}
	if archSub.idx != -1 {
		tmpl = tmpl[:archSub.idx] + "{{.arch}}" + tmpl[archSub.idx+len(archSub.val):]
	}
	if forbiddenArch[archSub.normalized] || forbiddenOS[osSub.normalized] {
		return nil, false
	}
	if !slices.ContainsFunc(systems, func(sys string) bool {
		o, a := parseDist(sys)
		return o == osSub.normalized && a == archSub.normalized
	}) {
		return nil, false
	}
	isArchive := false
	suffix := ""
	for _, s := range archiveSuffixes {
		if strings.HasSuffix(dlURL, s) {
			suffix = s
			isArchive = true
			break
		}
	}
	if strings.HasSuffix(dlURL, ".exe") {
		suffix = ".exe"
	}
	tmpl = tmpl[:len(tmpl)-len(suffix)] + "{{.urlSuffix}}"
	if version != "" {
		tmpl = strings.ReplaceAll(tmpl, version, "{{.version}}")
	}
	priority := 0
	if osSub != nil {
		priority += osSub.priority
	}
	if archSub != nil {
		priority += archSub.priority
	}
	return &dlFile{
		origUrl:   dlURL,
		url:       tmpl,
		osSub:     osSub,
		archSub:   archSub,
		suffix:    suffix,
		isArchive: isArchive,
		priority:  priority,
	}, true
}

func parseDownloads(dlUrls []string, binName, version string, allowedSystems []string) []*depGroup {
	systemFiles := map[string][]*dlFile{}
	for _, dlUrl := range dlUrls {
		f, ok := parseDownload(dlUrl, version, allowedSystems)
		if !ok {
			continue
		}
		dlSystem := f.system()
		systemFiles[dlSystem] = append(systemFiles[dlSystem], f)
	}
	for system := range systemFiles {
		if len(systemFiles[system]) < 2 {
			continue
		}
		// remove all but the highest priority
		slices.SortFunc(systemFiles[system], func(a, b *dlFile) bool {
			return a.priority > b.priority
		})
		cutOff := slices.IndexFunc(systemFiles[system], func(f *dlFile) bool {
			return f.priority < systemFiles[system][0].priority
		})
		if cutOff != -1 {
			systemFiles[system] = systemFiles[system][:cutOff]
		}
	}

	urlFrequency := map[string]int{}
	for _, files := range systemFiles {
		for _, f := range files {
			urlFrequency[f.url]++
		}
	}

	for system := range systemFiles {
		if len(systemFiles[system]) < 2 {
			continue
		}
		// prefer templates that are used more often
		slices.SortFunc(systemFiles[system], func(a, b *dlFile) bool {
			return urlFrequency[a.url] > urlFrequency[b.url]
		})
		cutOff := slices.IndexFunc(systemFiles[system], func(f *dlFile) bool {
			return urlFrequency[f.url] < urlFrequency[systemFiles[system][0].url]
		})
		if cutOff != -1 {
			systemFiles[system] = systemFiles[system][:cutOff]
		}
		if len(systemFiles[system]) == 1 {
			continue
		}
		// prefer archives
		slices.SortFunc(systemFiles[system], func(a, b *dlFile) bool {
			return a.isArchive && !b.isArchive
		})
		cutOff = slices.IndexFunc(systemFiles[system], func(f *dlFile) bool {
			return !f.isArchive
		})
		if cutOff != -1 {
			systemFiles[system] = systemFiles[system][:cutOff]
		}
		if len(systemFiles[system]) == 1 {
			continue
		}
		// now arbitrarily pick the first one alphabetically by origUrl
		slices.SortFunc(systemFiles[system], func(a, b *dlFile) bool {
			return a.origUrl < b.origUrl
		})
		systemFiles[system] = systemFiles[system][:1]
	}

	templates := maps.Keys(urlFrequency)
	slices.SortFunc(templates, func(a, b string) bool {
		return urlFrequency[a] > urlFrequency[b]
	})

	// special handling to remap darwin/arm64 to darwin/amd64
	if len(systemFiles["darwin/amd64"]) > 0 && len(systemFiles["darwin/arm64"]) == 0 && slices.Contains(allowedSystems, "darwin/arm64") {
		f := systemFiles["darwin/amd64"][0].clone()
		f.archSub.normalized = "arm64"
		f.priority -= 2
		systemFiles["darwin/arm64"] = append(systemFiles["darwin/arm64"], f)
	}

	var groups []*depGroup
	systems := maps.Keys(systemFiles)
	slices.SortFunc(systems, func(a, b string) bool {
		if len(systemFiles[a]) == 0 || len(systemFiles[b]) == 0 {
			return len(systemFiles[a]) > 0
		}
		aFile := systemFiles[a][0]
		bFile := systemFiles[b][0]
		if aFile.priority != bFile.priority {
			return aFile.priority > bFile.priority
		}
		return a < b
	})
	for _, system := range systems {
		file := systemFiles[system][0]
		idx := slices.IndexFunc(groups, func(g *depGroup) bool {
			return g.fileAllowed(file, binName)
		})
		if idx != -1 {
			groups[idx].addFile(file, binName)
			continue
		}
		group := &depGroup{
			substitutions: map[string]map[string]string{
				"os":   {},
				"arch": {},
			},
			overrideMatcher: map[string][]string{},
		}
		group.addFile(file, binName)
		groups = append(groups, group)
	}
	slices.SortFunc(groups, func(a, b *depGroup) bool {
		return len(a.files) > len(b.files)
	})
	return groups
}

func buildConfig(name, version string, groups []*depGroup) *bindown.Config {
	dep := groups[0].dependency()
	checksums := map[string]string{}
	for i := 0; i < len(groups); i++ {
		group := groups[i]
		for _, file := range group.files {
			checksums[file.origUrl] = file.checksum
		}
		if i == 0 {
			continue
		}
		otherGroups := slices.Clone(groups[:i])
		otherGroups = append(otherGroups, groups[i+1:]...)
		for _, system := range group.systems {
			o, a := parseDist(system)
			dep.Systems = append(dep.Systems, bindown.SystemInfo{
				OS:   o,
				Arch: a,
			})
		}
		dep.Overrides = append(dep.Overrides, group.overrides(otherGroups)...)
	}
	slices.SortFunc(dep.Systems, func(a, b bindown.SystemInfo) bool {
		if a.OS != b.OS {
			return a.OS < b.OS
		}
		return a.Arch < b.Arch
	})
	for tp := range dep.Substitutions {
		for k, v := range dep.Substitutions[tp] {
			if k == v {
				delete(dep.Substitutions[tp], k)
			}
		}
		if len(dep.Substitutions[tp]) == 0 {
			delete(dep.Substitutions, tp)
		}
	}
	for i := range dep.Overrides {
		for tp := range dep.Overrides[i].Dependency.Substitutions {
			for k, v := range dep.Overrides[i].Dependency.Substitutions[tp] {
				if k != v {
					continue
				}
				var depVal string
				var ok bool
				if dep.Substitutions != nil && dep.Substitutions[tp] != nil {
					depVal, ok = dep.Substitutions[tp][k]
				}
				if !ok {
					if k == v {
						delete(dep.Overrides[i].Dependency.Substitutions[tp], k)
					}
					continue
				}
				if depVal == v {
					delete(dep.Overrides[i].Dependency.Substitutions[tp], k)
				}
			}
			if len(dep.Overrides[i].Dependency.Substitutions[tp]) == 0 {
				delete(dep.Overrides[i].Dependency.Substitutions, tp)
			}
		}
	}
	return &bindown.Config{
		Systems: dep.Systems,
		Dependencies: map[string]*bindown.Dependency{
			name: {
				Template: &name,
				Vars: map[string]string{
					"version": version,
				},
			},
		},
		Templates: map[string]*bindown.Dependency{
			name: dep,
		},
		URLChecksums: checksums,
	}
}

func splitSystems(systems []string, fn func(s string) bool) (matching, nonMatching []string) {
	for _, system := range systems {
		if fn(system) {
			matching = append(matching, system)
		} else {
			nonMatching = append(nonMatching, system)
		}
	}
	return
}

func systemsMatcher(systems, otherSystems []string) (_ map[string][]string, matcherSystems, remainingSystems []string) {
	var oses, arches, otherOses, otherArches, exclusiveOses, exclusiveArches []string
	for _, system := range systems {
		o, a := parseDist(system)
		if !slices.Contains(oses, o) {
			oses = append(oses, o)
		}
		if !slices.Contains(arches, a) {
			arches = append(arches, a)
		}
	}
	for _, system := range otherSystems {
		o, a := parseDist(system)
		if !slices.Contains(otherOses, o) {
			otherOses = append(otherOses, o)
		}
		if !slices.Contains(otherArches, a) {
			otherArches = append(otherArches, a)
		}
	}
	for _, s := range oses {
		if !slices.Contains(otherOses, s) {
			exclusiveOses = append(exclusiveOses, s)
		}
	}
	if len(exclusiveOses) > 0 {
		s, r := splitSystems(systems, func(system string) bool {
			return slices.Contains(exclusiveOses, systemOs(system))
		})
		return map[string][]string{"os": exclusiveOses}, s, r
	}
	for _, s := range arches {
		if !slices.Contains(otherArches, s) {
			exclusiveArches = append(exclusiveArches, s)
		}
	}
	if len(exclusiveArches) > 0 {
		s, r := splitSystems(systems, func(system string) bool {
			return slices.Contains(exclusiveArches, systemArch(system))
		})
		return map[string][]string{"arch": exclusiveArches}, s, r
	}
	if (len(oses) == 0) != (len(arches) == 0) {
		panic("inconsistent systems")
	}
	if len(oses) == 0 {
		return nil, nil, systems
	}
	if len(arches) < len(oses) {
		a := arches[0]
		var archOses []string
		for _, system := range systems {
			if systemArch(system) == a {
				matcherSystems = append(matcherSystems, system)
				archOses = append(archOses, systemOs(system))
				continue
			}
			remainingSystems = append(remainingSystems, system)
		}
		return map[string][]string{
			"arch": {a},
			"os":   archOses,
		}, matcherSystems, remainingSystems
	}
	o := oses[0]
	var osArches []string
	for _, system := range systems {
		if systemOs(system) == o {
			osArches = append(osArches, systemArch(system))
		}
	}
	s, r := splitSystems(systems, func(system string) bool {
		return systemOs(system) == o
	})
	return map[string][]string{
		"os":   {o},
		"arch": osArches,
	}, s, r
}
