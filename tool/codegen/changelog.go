package codegen

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// ChangelogEntry 一条 CHANGELOG 记录
type ChangelogEntry struct {
	Version   string
	Date      string
	Hash      string
	Kind      string // feat | fix | refactor | docs | perf | test | chore | BREAKING
	Scope     string
	Subject   string
}

// GroupedChangelog 按 Kind 分组的变更日志（用于版本 section 内部排版）
type GroupedChangelog struct {
	Version string
	Date    string
	Groups  map[string][]ChangelogEntry
}

// GenerateGitChangelog 调用 `git log` 收集提交，按 kind(scope): subject 格式解析
// 输出 Markdown 风格 CHANGELOG 片段
//
// fromRef: 起点（如上一个 tag），为空则取最近 200 条
// toRef:   终点（默认 HEAD）
// version: 当前版本字符串，用于顶部标题
func GenerateGitChangelog(fromRef, toRef, version string, maxCount int) (string, error) {
	if toRef == "" {
		toRef = "HEAD"
	}
	if maxCount <= 0 {
		maxCount = 200
	}
	args := []string{"log", "--pretty=format:%h|%ad|%s", "--date=short"}
	if fromRef != "" {
		args = append(args, fmt.Sprintf("%s..%s", fromRef, toRef))
	} else {
		args = append(args, toRef, fmt.Sprintf("-n%d", maxCount))
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("git log failed: %w", err)
	}
	return formatChangelog(string(out), version), nil
}

// formatChangelog 解析 git log 输出并按类型分组生成 Markdown
func formatChangelog(log, version string) string {
	entries := parseGitLog(log)
	grouped := groupByKind(entries)

	var b strings.Builder
	if version != "" {
		b.WriteString(fmt.Sprintf("# %s\n\n", version))
	}

	// 固定输出顺序
	order := []string{"BREAKING", "feat", "fix", "perf", "refactor", "docs", "test", "chore", "other"}
	for _, kind := range order {
		items := grouped[kind]
		if len(items) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("## %s\n\n", kindHeading(kind)))
		for _, e := range items {
			line := e.Subject
			if e.Scope != "" {
				line = fmt.Sprintf("**%s**: %s", e.Scope, line)
			}
			b.WriteString(fmt.Sprintf("- %s (`%s`)\n", line, e.Hash))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func kindHeading(k string) string {
	switch k {
	case "BREAKING":
		return "⚠ Breaking Changes"
	case "feat":
		return "Features"
	case "fix":
		return "Bug Fixes"
	case "perf":
		return "Performance"
	case "refactor":
		return "Refactor"
	case "docs":
		return "Documentation"
	case "test":
		return "Tests"
	case "chore":
		return "Chore"
	}
	return "Other"
}

var commitRe = regexp.MustCompile(`^(feat|fix|refactor|docs|perf|test|chore|style|build|ci)(?:\(([^)]+)\))?(!)?: (.+)$`)

// parseGitLog 解析 git log 三字段输出（hash|date|subject）
func parseGitLog(log string) []ChangelogEntry {
	lines := strings.Split(strings.TrimSpace(log), "\n")
	out := make([]ChangelogEntry, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		fields := strings.SplitN(l, "|", 3)
		if len(fields) < 3 {
			continue
		}
		entry := ChangelogEntry{
			Hash:    fields[0],
			Date:    fields[1],
			Subject: fields[2],
		}
		classifyCommit(&entry)
		out = append(out, entry)
	}
	return out
}

// classifyCommit 解析 Conventional Commits 风格 subject，识别 kind/scope/breaking
func classifyCommit(entry *ChangelogEntry) {
	subj := strings.TrimSpace(entry.Subject)
	if m := commitRe.FindStringSubmatch(subj); m != nil {
		entry.Kind = m[1]
		entry.Scope = m[2]
		if m[3] == "!" {
			entry.Kind = "BREAKING"
		}
		entry.Subject = strings.TrimSpace(m[4])
		return
	}
	if strings.HasPrefix(strings.ToUpper(subj), "BREAKING CHANGE") {
		entry.Kind = "BREAKING"
		entry.Subject = strings.TrimSpace(strings.TrimPrefix(subj, "BREAKING CHANGE"))
		entry.Subject = strings.TrimPrefix(entry.Subject, ":")
		entry.Subject = strings.TrimSpace(entry.Subject)
		return
	}
	entry.Kind = "other"
}

func groupByKind(entries []ChangelogEntry) map[string][]ChangelogEntry {
	m := make(map[string][]ChangelogEntry)
	for _, e := range entries {
		m[e.Kind] = append(m[e.Kind], e)
	}
	// 每组内按日期倒序，使得最新的排前面
	for k := range m {
		items := m[k]
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Date > items[j].Date
		})
		m[k] = items
	}
	return m
}
