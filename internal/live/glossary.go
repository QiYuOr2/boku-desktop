package live

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"unicode"

	"boku/internal/domain"
	"boku/internal/platform/atomicfile"
)

const (
	// GlossaryFileName 是用户数据目录下的术语文件名。
	GlossaryFileName = "live_glossary.json"

	glossaryVersion = 1
	glossaryLimit   = 2000
	promptTermLimit = 32
)

type Entry struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// Glossary 是术语文件。解析失败的文件在内存中当作空表，
// 并且不会被覆盖写回。
type Glossary struct {
	path    string
	entries []Entry
	broken  bool
	clock   domain.Clock
}

func LoadGlossary(path string, clock domain.Clock) (*Glossary, error) {
	if clock == nil {
		clock = domain.SystemClock{}
	}
	glossary := &Glossary{path: path, clock: clock}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return glossary, nil
	}
	if err != nil {
		return nil, err
	}
	var document struct {
		Version int     `json:"version"`
		Entries []Entry `json:"entries"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.Version != glossaryVersion {
		glossary.broken = true
		return glossary, nil
	}
	seen := map[string]bool{}
	for _, entry := range document.Entries {
		source := strings.TrimSpace(entry.Source)
		target := strings.TrimSpace(entry.Target)
		if entry.ID == "" || source == "" || target == "" || seen[source] {
			glossary.broken = true
			glossary.entries = nil
			return glossary, nil
		}
		seen[source] = true
		entry.Source = source
		entry.Target = target
		glossary.entries = append(glossary.entries, entry)
	}
	return glossary, nil
}

func (g *Glossary) Broken() bool { return g != nil && g.broken }

func (g *Glossary) Entries() []Entry {
	if g == nil {
		return nil
	}
	out := make([]Entry, len(g.entries))
	copy(out, g.entries)
	return out
}

func (g *Glossary) Add(source, target string) error {
	source, target, err := cleanPair(source, target)
	if err != nil {
		return err
	}
	if g.broken {
		return errors.New("术语文件读不了，没有改它")
	}
	if len(g.entries) >= glossaryLimit {
		return errors.New("术语最多 2000 条")
	}
	if g.has(source, "") {
		return errors.New("这条日语已经有了")
	}
	id, err := domain.NewIDAt(g.clock.Now())
	if err != nil {
		return err
	}
	g.entries = append(g.entries, Entry{ID: id, Source: source, Target: target})
	if err := g.write(); err != nil {
		g.entries = g.entries[:len(g.entries)-1]
		return err
	}
	return nil
}

func (g *Glossary) Update(id, source, target string) error {
	source, target, err := cleanPair(source, target)
	if err != nil {
		return err
	}
	if g.broken {
		return errors.New("术语文件读不了，没有改它")
	}
	index := g.index(id)
	if index < 0 {
		return errors.New("没有这条术语")
	}
	if g.has(source, id) {
		return errors.New("这条日语已经有了")
	}
	previous := g.entries[index]
	g.entries[index] = Entry{ID: id, Source: source, Target: target}
	if err := g.write(); err != nil {
		g.entries[index] = previous
		return err
	}
	return nil
}

func (g *Glossary) Delete(id string) error {
	if g.broken {
		return errors.New("术语文件读不了，没有改它")
	}
	index := g.index(id)
	if index < 0 {
		return errors.New("没有这条术语")
	}
	removed := g.entries[index]
	g.entries = append(g.entries[:index], g.entries[index+1:]...)
	if err := g.write(); err != nil {
		g.entries = append(g.entries[:index], append([]Entry{removed}, g.entries[index:]...)...)
		return err
	}
	return nil
}

// Import 读取形如 `日语 => 中文` 的行，空行与以 # 开头的注释忽略。
// 格式错误的行或已存在的源文会被跳过并计数，已有条目保持不变。
func (g *Glossary) Import(text string) (int, error) {
	if g.broken {
		return 0, errors.New("术语文件读不了，没有改它")
	}
	skipped := 0
	previous := append([]Entry(nil), g.entries...)
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		source, target, found := strings.Cut(line, "=>")
		if !found {
			skipped++
			continue
		}
		source, target, err := cleanPair(source, target)
		if err != nil || g.has(source, "") || len(g.entries) >= glossaryLimit {
			skipped++
			continue
		}
		id, err := domain.NewIDAt(g.clock.Now())
		if err != nil {
			return skipped, err
		}
		g.entries = append(g.entries, Entry{ID: id, Source: source, Target: target})
	}
	if err := g.write(); err != nil {
		g.entries = previous
		return skipped, err
	}
	return skipped, nil
}

func (g *Glossary) write() error {
	if g.broken {
		return errors.New("术语文件读不了，没有改它")
	}
	body, err := json.Marshal(struct {
		Version int     `json:"version"`
		Entries []Entry `json:"entries"`
	}{Version: glossaryVersion, Entries: g.entries})
	if err != nil {
		return err
	}
	return atomicfile.Write(g.path, body)
}

func (g *Glossary) has(source, exceptID string) bool {
	for _, entry := range g.entries {
		if entry.Source == source && entry.ID != exceptID {
			return true
		}
	}
	return false
}

func (g *Glossary) index(id string) int {
	for i, entry := range g.entries {
		if entry.ID == id {
			return i
		}
	}
	return -1
}

func cleanPair(source, target string) (string, string, error) {
	source = strings.TrimSpace(source)
	target = strings.TrimSpace(target)
	if source == "" || target == "" || strings.TrimFunc(source, unicode.IsSpace) == "" {
		return "", "", errors.New("源文和译文都不能空")
	}
	return source, target, nil
}

type matchSpan struct {
	index  int
	start  int
	end    int
	length int
}

// Match 返回源文是 sentence 子串的条目。重叠时保留更长的源文；
// 长度相同时保留文件中靠前的条目。结果按文件顺序排列，最多 32 条。
func Match(sentence string, entries []Entry) []Entry {
	sentence = strings.TrimSpace(sentence)
	var spans []matchSpan
	for index, entry := range entries {
		source := entry.Source
		if source == "" {
			continue
		}
		from := 0
		for from <= len(sentence) {
			at := strings.Index(sentence[from:], source)
			if at < 0 {
				break
			}
			start := from + at
			spans = append(spans, matchSpan{index: index, start: start, end: start + len(source), length: len(source)})
			from = start + 1
		}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].length != spans[j].length {
			return spans[i].length > spans[j].length
		}
		if spans[i].index != spans[j].index {
			return spans[i].index < spans[j].index
		}
		return spans[i].start < spans[j].start
	})
	var kept []matchSpan
	chosen := map[int]bool{}
	for _, span := range spans {
		if chosen[span.index] {
			continue
		}
		overlaps := false
		for _, earlier := range kept {
			if span.start < earlier.end && earlier.start < span.end {
				overlaps = true
				break
			}
		}
		if overlaps {
			continue
		}
		kept = append(kept, span)
		chosen[span.index] = true
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].index < kept[j].index })
	if len(kept) > promptTermLimit {
		kept = kept[:promptTermLimit]
	}
	out := make([]Entry, len(kept))
	for i, span := range kept {
		out[i] = entries[span.index]
	}
	return out
}
